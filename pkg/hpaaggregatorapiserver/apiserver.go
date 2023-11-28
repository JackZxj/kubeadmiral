/*
Copyright 2023 The KubeAdmiral Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package hpaaggregatorapiserver

import (
	"context"
	"path"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/apiserver/pkg/registry/rest"
	genericapiserver "k8s.io/apiserver/pkg/server"
	dynamicclient "k8s.io/client-go/dynamic"
	kubeclient "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	autoscalinginstall "k8s.io/kubernetes/pkg/apis/autoscaling/install"
	apiinstall "k8s.io/kubernetes/pkg/apis/core/install"
	custommetricsv1beta1 "k8s.io/metrics/pkg/apis/custom_metrics/v1beta1"
	custommetricsv1beta2 "k8s.io/metrics/pkg/apis/custom_metrics/v1beta2"
	metricsinstall "k8s.io/metrics/pkg/apis/metrics/install"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	custommetricsscheme "k8s.io/metrics/pkg/client/custom_metrics/scheme"
	"sigs.k8s.io/custom-metrics-apiserver/pkg/apiserver/installer"

	hpaaggregatorapi "github.com/kubewharf/kubeadmiral/pkg/apis/hpaaggregator"
	"github.com/kubewharf/kubeadmiral/pkg/apis/hpaaggregator/install"
	"github.com/kubewharf/kubeadmiral/pkg/apis/hpaaggregator/v1alpha1"
	fedclient "github.com/kubewharf/kubeadmiral/pkg/client/clientset/versioned"
	fedinformers "github.com/kubewharf/kubeadmiral/pkg/client/informers/externalversions"
	"github.com/kubewharf/kubeadmiral/pkg/hpaaggregatorapiserver/serverconfig"
	"github.com/kubewharf/kubeadmiral/pkg/registry"
	"github.com/kubewharf/kubeadmiral/pkg/registry/hpaaggregator/aggregation"
	metricsaggregator "github.com/kubewharf/kubeadmiral/pkg/registry/hpaaggregator/aggregation/metrics"
	"github.com/kubewharf/kubeadmiral/pkg/registry/hpaaggregator/externalmetricadaptor"
	"github.com/kubewharf/kubeadmiral/pkg/util/informermanager"
)

var (
	// Scheme defines methods for serializing and deserializing API objects.
	Scheme = runtime.NewScheme()
	// Codecs provides methods for retrieving codecs and serializers for specific
	// versions and content types.
	Codecs = serializer.NewCodecFactory(Scheme)
	// ParameterCodec handles versioning of objects that are converted to query parameters.
	ParameterCodec = runtime.NewParameterCodec(Scheme)
)

func init() {
	install.Install(Scheme)
	apiinstall.Install(Scheme)
	autoscalinginstall.Install(Scheme)
	metricsinstall.Install(Scheme)
	custommetricsscheme.AddToScheme(Scheme)

	// we need custom conversion functions to list resources with options
	utilruntime.Must(installer.RegisterConversions(Scheme))

	// we need to add the options to empty v1
	// TODO fix the server code to avoid this
	metav1.AddToGroupVersion(Scheme, schema.GroupVersion{Version: "v1"})

	// TODO: keep the generic API server from wanting this
	unversioned := schema.GroupVersion{Group: "", Version: "v1"}
	Scheme.AddUnversionedTypes(unversioned,
		&metav1.Status{},
		&metav1.APIVersions{},
		&metav1.APIGroupList{},
		&metav1.APIGroup{},
		&metav1.APIResourceList{},
	)
	utilruntime.Must(internalversion.AddToScheme(Scheme))
}

// ExtraConfig holds custom apiserver config
type ExtraConfig struct {
	// Place you custom config here.
	APIServerEndpoint string

	KubeClientset    kubeclient.Interface
	DynamicClientset dynamicclient.Interface
	FedClientset     fedclient.Interface
	RestConfig       *restclient.Config

	FederatedInformerManager informermanager.FederatedInformerManager
	FedInformerFactory       fedinformers.SharedInformerFactory
	RequestInfoResolver      *serverconfig.RequestInfoResolver
}

// Config defines the config for the apiserver
type Config struct {
	GenericConfig *genericapiserver.RecommendedConfig
	ExtraConfig   *ExtraConfig
}

// Server contains state for a Kubernetes cluster master/api server.
type Server struct {
	GenericAPIServer *genericapiserver.GenericAPIServer
}

type completedConfig struct {
	GenericConfig genericapiserver.CompletedConfig
	ExtraConfig   *ExtraConfig
}

// CompletedConfig embeds a private pointer that cannot be instantiated outside of this package.
type CompletedConfig struct {
	*completedConfig
}

// Complete fills in any fields not set that are required to have valid data. It's mutating the receiver.
func (cfg *Config) Complete() CompletedConfig {
	c := completedConfig{
		cfg.GenericConfig.Complete(),
		cfg.ExtraConfig,
	}

	c.GenericConfig.Version = &version.Info{
		Major: "1",
		Minor: "0",
	}

	return CompletedConfig{&c}
}

// New returns a new instance of Server from the given config.
func (c completedConfig) New() (*Server, error) {
	genericServer, err := c.GenericConfig.New("hpa-aggregator-apiserver", genericapiserver.NewEmptyDelegate())
	if err != nil {
		return nil, err
	}

	s := &Server{
		GenericAPIServer: genericServer,
	}

	apiGroupInfo := genericapiserver.NewDefaultAPIGroupInfo(hpaaggregatorapi.GroupName, Scheme, ParameterCodec, Codecs)

	v1alpha1storage := map[string]rest.Storage{}
	v1alpha1storage["externalmetricadaptors"] = registry.RESTInPeace(externalmetricadaptor.NewREST(Scheme, c.GenericConfig.RESTOptionsGetter))
	aggregationAPI, err := aggregation.NewREST(
		c.ExtraConfig.DynamicClientset,
		c.ExtraConfig.FedClientset,
		c.ExtraConfig.FederatedInformerManager,
		Scheme,
		c.ExtraConfig.APIServerEndpoint,
		"",
		c.ExtraConfig.RestConfig,
		time.Duration(c.GenericConfig.MinRequestTimeout)*time.Second,
		klog.Background().WithValues("api", "aggregation"),
	)
	v1alpha1storage["aggregation"] = registry.RESTInPeace(aggregationAPI, err)
	apiGroupInfo.VersionedResourcesStorageMap[v1alpha1.SchemeGroupVersion.Version] = v1alpha1storage

	if err := s.GenericAPIServer.InstallAPIGroup(&apiGroupInfo); err != nil {
		return nil, err
	}

	root := path.Join("/apis", v1alpha1.SchemeGroupVersion.Group, v1alpha1.SchemeGroupVersion.Version, "aggregation")

	if err := metricsaggregator.InstallMetrics(
		root,
		c.GenericConfig,
		Scheme,
		ParameterCodec,
		Codecs,
		aggregationAPI.MetricsGetter,
		aggregationAPI.PodLister,
		aggregationAPI.NodeLister,
		nil,
		genericServer,
	); err != nil {
		return nil, err
	}

	if err := metricsaggregator.InstallCustomMetricsAPI(
		root,
		Scheme,
		ParameterCodec,
		Codecs,
		genericServer,
		c.ExtraConfig.FederatedInformerManager,
		klog.Background().WithValues("api", "custom-metrics"),
	); err != nil {
		return nil, err
	}

	c.ExtraConfig.RequestInfoResolver.InsertCustomPrefixes(serverconfig.NewDefaultResolver(root))
	c.ExtraConfig.RequestInfoResolver.InsertConnecterBlacklist(
		path.Join(root, "apis", metricsv1beta1.SchemeGroupVersion.String()),
		path.Join(root, "apis", custommetricsv1beta1.SchemeGroupVersion.String()),
		path.Join(root, "apis", custommetricsv1beta2.SchemeGroupVersion.String()),
	)

	return s, nil
}

func (e *ExtraConfig) Run(ctx context.Context) {
	e.FedInformerFactory.Start(ctx.Done())
	e.FederatedInformerManager.Start(ctx)

	if !cache.WaitForNamedCacheSync("hpa-aggregator", ctx.Done(), e.FederatedInformerManager.HasSynced) {
		klog.Error("Timed out waiting for cache sync")
		return
	}
	klog.Info("FederatedInformerManager started")
	return
}
