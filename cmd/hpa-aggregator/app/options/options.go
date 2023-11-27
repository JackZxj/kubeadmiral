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

package options

import (
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/endpoints/openapi"
	"k8s.io/apiserver/pkg/features"
	genericapiserver "k8s.io/apiserver/pkg/server"
	genericoptions "k8s.io/apiserver/pkg/server/options"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	dynamicclient "k8s.io/client-go/dynamic"
	kubeclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	restfulcommon "k8s.io/kube-openapi/pkg/common"
	netutils "k8s.io/utils/net"

	fedcorev1a1 "github.com/kubewharf/kubeadmiral/pkg/apis/core/v1alpha1"
	"github.com/kubewharf/kubeadmiral/pkg/apis/hpaaggregator"
	hpaaggregatorv1alpha1 "github.com/kubewharf/kubeadmiral/pkg/apis/hpaaggregator/v1alpha1"
	fedclient "github.com/kubewharf/kubeadmiral/pkg/client/clientset/versioned"
	fedinformers "github.com/kubewharf/kubeadmiral/pkg/client/informers/externalversions"
	fedopenapi "github.com/kubewharf/kubeadmiral/pkg/client/openapi"
	"github.com/kubewharf/kubeadmiral/pkg/controllers/common"
	apiserver "github.com/kubewharf/kubeadmiral/pkg/hpaaggregatorapiserver"
	"github.com/kubewharf/kubeadmiral/pkg/hpaaggregatorapiserver/serverconfig"
	clusterutil "github.com/kubewharf/kubeadmiral/pkg/util/cluster"
	"github.com/kubewharf/kubeadmiral/pkg/util/informermanager"
)

const (
	defaultEtcdPathPrefix = "/hpa-aggregator"
	openAPITitle          = "KubeAdmiral-HPA-Aggregator"
)

type Options struct {
	RecommendedOptions *genericoptions.RecommendedOptions

	Master       string
	KubeAPIQPS   float32
	KubeAPIBurst int

	EnableProfiling bool
	LogFile         string
	LogVerbosity    int
	KlogVerbosity   int

	MaxPodListers    int64
	EnablePodPruning bool

	PrometheusMetrics bool
	PrometheusAddr    string
	PrometheusPort    uint16
}

func NewOptions() *Options {
	o := &Options{
		RecommendedOptions: genericoptions.NewRecommendedOptions(
			defaultEtcdPathPrefix,
			apiserver.Codecs.LegacyCodec(hpaaggregatorv1alpha1.SchemeGroupVersion),
		),
	}
	o.RecommendedOptions.Etcd.StorageConfig.EncodeVersioner = runtime.NewMultiGroupVersioner(
		hpaaggregatorv1alpha1.SchemeGroupVersion,
		schema.GroupKind{Group: hpaaggregator.GroupName},
	)
	return o
}

//nolint:lll
func (o *Options) AddFlags(flags *pflag.FlagSet) {
	flags.StringVar(&o.Master, "master", "", "The address of the host Kubernetes cluster.")
	flags.Float32Var(&o.KubeAPIQPS, "kube-api-qps", 500, "The maximum QPS from each Kubernetes client.")
	flags.IntVar(&o.KubeAPIBurst, "kube-api-burst", 1000, "The maximum burst for throttling requests from each Kubernetes client.")

	flags.BoolVar(&o.EnableProfiling, "enable-profiling", false, "Enable profiling for the controller manager.")

	flags.Int64Var(&o.MaxPodListers, "max-pod-listers", 0, "The maximum number of concurrent pod listing requests to member clusters. "+
		"A non-positive number means unlimited, but may increase the instantaneous memory usage.")
	flags.BoolVar(&o.EnablePodPruning, "enable-pod-pruning", false, "Enable pod pruning for pod informer. "+
		"Enabling this can reduce memory usage of the pod informer, but will disable pod propagation.")

	flags.BoolVar(&o.PrometheusMetrics, "export-prometheus", true, "Whether to expose metrics through a prometheus endpoint")
	flags.StringVar(&o.PrometheusAddr, "prometheus-addr", "", "Prometheus collector address")
	flags.Uint16Var(&o.PrometheusPort, "prometheus-port", 9090, "Prometheus collector port")

	o.RecommendedOptions.AddFlags(flags)
	o.addKlogFlags(flags)
}

// Validate validates Options
func (o *Options) Validate() error {
	var errors []error
	errors = append(errors, o.RecommendedOptions.Validate()...)
	return utilerrors.NewAggregate(errors)
}

// Complete fills in fields required to have valid data
func (o *Options) Complete() error {
	// TODO: register admission plugins and add it to o.RecommendedOptions.Admission.RecommendedPluginOrder
	return nil
}

// Config returns config for the api server given Options
func (o *Options) Config() (*apiserver.Config, error) {
	// TODO have a "real" external address
	if err := o.RecommendedOptions.SecureServing.MaybeDefaultWithSelfSignedCerts(
		"localhost",
		nil,
		[]net.IP{netutils.ParseIPSloppy("127.0.0.1")},
	); err != nil {
		return nil, fmt.Errorf("error creating self-signed certificates: %v", err)
	}

	o.RecommendedOptions.Etcd.StorageConfig.Paging = true

	o.RecommendedOptions.ExtraAdmissionInitializers = func(c *genericapiserver.RecommendedConfig) ([]admission.PluginInitializer, error) {
		return []admission.PluginInitializer{}, nil
	}

	serverConfig := genericapiserver.NewRecommendedConfig(apiserver.Codecs)
	serverConfig.Config.BuildHandlerChainFunc = serverconfig.CopyAuthHandlerChain

	serverConfig.OpenAPIConfig = genericapiserver.DefaultOpenAPIConfig(
		fedopenapi.GetOpenAPIDefinitions,
		openapi.NewDefinitionNamer(apiserver.Scheme),
	)
	serverConfig.OpenAPIConfig.Info.Title = openAPITitle
	serverConfig.OpenAPIConfig.GetOperationIDAndTagsFromRoute = func(r restfulcommon.Route) (string, []string, error) {
		return r.OperationName() + r.Path(), nil, nil
	}

	if utilfeature.DefaultFeatureGate.Enabled(features.OpenAPIV3) {
		serverConfig.OpenAPIV3Config = genericapiserver.DefaultOpenAPIV3Config(
			fedopenapi.GetOpenAPIDefinitions,
			openapi.NewDefinitionNamer(apiserver.Scheme),
		)
		serverConfig.OpenAPIV3Config.Info.Title = openAPITitle
		serverConfig.OpenAPIV3Config.GetOperationIDAndTagsFromRoute = func(r restfulcommon.Route) (string, []string, error) {
			return r.OperationName() + r.Path(), nil, nil
		}
	}

	if err := o.RecommendedOptions.ApplyTo(serverConfig); err != nil {
		return nil, err
	}

	RequestInfoResolver := serverconfig.NewRequestInfoResolver(&serverConfig.Config)
	serverConfig.RequestInfoResolver = RequestInfoResolver

	restConfig, err := clientcmd.BuildConfigFromFlags(o.Master, o.RecommendedOptions.CoreAPI.CoreAPIKubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create rest config: %w", err)
	}
	restConfig.QPS = o.KubeAPIQPS
	restConfig.Burst = o.KubeAPIBurst

	kubeClientset, err := kubeclient.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kube clientset: %w", err)
	}
	dynamicClientset, err := dynamicclient.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic clientset: %w", err)
	}
	fedClientset, err := fedclient.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create fed clientset: %w", err)
	}

	informerResyncPeriod := time.Duration(0)
	fedInformerFactory := fedinformers.NewSharedInformerFactory(fedClientset, informerResyncPeriod)

	federatedInformerManager := informermanager.NewFederatedInformerManager(
		informermanager.ClusterClientHelper{
			ConnectionHash: informermanager.DefaultClusterConnectionHash,
			RestConfigGetter: func(cluster *fedcorev1a1.FederatedCluster) (*rest.Config, error) {
				return clusterutil.BuildClusterConfig(
					cluster,
					kubeClientset,
					restConfig,
					common.DefaultFedSystemNamespace,
				)
			},
		},
		fedInformerFactory.Core().V1alpha1().FederatedTypeConfigs(),
		fedInformerFactory.Core().V1alpha1().FederatedClusters(),
	)

	config := &apiserver.Config{
		GenericConfig: serverConfig,
		ExtraConfig: &apiserver.ExtraConfig{
			APIServerEndpoint:        restConfig.Host,
			KubeClientset:            kubeClientset,
			DynamicClientset:         dynamicClientset,
			FedClientset:             fedClientset,
			FedInformerFactory:       fedInformerFactory,
			FederatedInformerManager: federatedInformerManager,
			RequestInfoResolver:      RequestInfoResolver,
		},
	}
	return config, nil
}

func (o *Options) addKlogFlags(flags *pflag.FlagSet) {
	klogFlags := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	klog.InitFlags(klogFlags)

	klogFlags.VisitAll(func(f *flag.Flag) {
		f.Name = fmt.Sprintf("klog-%s", strings.ReplaceAll(f.Name, "_", "-"))
	})
	flags.AddGoFlagSet(klogFlags)
}