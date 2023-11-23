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

package metrics

import (
	"fmt"
	"path"
	"time"

	apidiscoveryv2beta1 "k8s.io/api/apidiscovery/v2beta1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	genericapi "k8s.io/apiserver/pkg/endpoints"
	"k8s.io/apiserver/pkg/endpoints/discovery"
	discoveryendpoint "k8s.io/apiserver/pkg/endpoints/discovery/aggregated"
	"k8s.io/apiserver/pkg/features"
	genericfeatures "k8s.io/apiserver/pkg/features"
	"k8s.io/apiserver/pkg/registry/rest"
	genericapiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/apiserver/pkg/storageversion"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	corev1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/metrics/pkg/apis/metrics"
	"k8s.io/metrics/pkg/apis/metrics/v1beta1"

	"github.com/kubewharf/kubeadmiral/pkg/registry/hpaaggregator/aggregation/metrics/resource"
)

// Install builds the metrics for the metrics.k8s.io API, and then installs it into the given API metrics-server.
func Install(
	prefix string,
	m resource.MetricsGetter,
	podMetadataLister cache.GenericLister,
	nodeLister corev1.NodeLister,
	storages map[string]rest.Storage,
	nodeSelector []labels.Requirement,
) error {
	if storages == nil {
		return fmt.Errorf("storages should not be nil")
	}
	node := resource.NewNodeMetrics(metrics.Resource("nodemetrics"), m, nodeLister, nodeSelector)
	pod := resource.NewPodMetrics(metrics.Resource("podmetrics"), m, podMetadataLister)

	prefix = path.Join(prefix, "apis", metrics.GroupName, v1beta1.SchemeGroupVersion.Version)
	storages[prefix+"/nodes"] = node
	storages[prefix+"/pods"] = pod
	return nil
}

func InstallMetrics(
	parentPath string,
	c genericapiserver.CompletedConfig,
	scheme *runtime.Scheme,
	parameterCodec runtime.ParameterCodec,
	codecs serializer.CodecFactory,
	m resource.MetricsGetter,
	podMetadataLister cache.GenericLister,
	nodeLister corev1.NodeLister,
	nodeSelector []labels.Requirement,
	s *genericapiserver.GenericAPIServer,
) error {
	storage := make(map[string]rest.Storage)
	node := resource.NewNodeMetrics(metrics.Resource("nodemetrics"), m, nodeLister, nodeSelector)
	pod := resource.NewPodMetrics(metrics.Resource("podmetrics"), m, podMetadataLister)
	storage["nodes"] = node
	storage["pods"] = pod

	version := NewAPIGroupVersion(c, scheme, parameterCodec, codecs, v1beta1.SchemeGroupVersion)
	version.Storage = storage
	version.Root = path.Join(parentPath, "apis")

	discoveryAPIResources, r, err := version.InstallREST(s.Handler.GoRestfulContainer)
	if err != nil {
		return fmt.Errorf("unable to setup API %v: %v", version, err)
	}

	if c.EnableDiscovery {
		root := path.Join(parentPath, "api")
		// TODO: make it global
		discoveryGroupManager := discovery.NewLegacyRootAPIHandler(c.DiscoveryAddresses, codecs, root)
		if utilfeature.DefaultFeatureGate.Enabled(genericfeatures.AggregatedDiscoveryEndpoint) {
			manager := discoveryendpoint.NewResourceManager()
			manager.AddGroupVersion(
				v1beta1.SchemeGroupVersion.Group,
				apidiscoveryv2beta1.APIVersionDiscovery{
					Version:   v1beta1.SchemeGroupVersion.Version,
					Resources: discoveryAPIResources,
				},
			)
			wrapped := discoveryendpoint.WrapAggregatedDiscoveryToHandler(discoveryGroupManager, manager)
			s.Handler.GoRestfulContainer.Add(wrapped.GenerateWebService(root, metav1.APIGroupList{}))
		} else {
			s.Handler.GoRestfulContainer.Add(discoveryGroupManager.WebService())
		}
	}

	var resourceInfos []*storageversion.ResourceInfo
	resourceInfos = append(resourceInfos, r...)

	s.RegisterDestroyFunc(func() {
		node.Destroy()
		pod.Destroy()
	})

	if utilfeature.DefaultFeatureGate.Enabled(features.StorageVersionAPI) &&
		utilfeature.DefaultFeatureGate.Enabled(features.APIServerIdentity) {
		// API installation happens before we start listening on the handlers,
		// therefore it is safe to register ResourceInfos here. The handler will block
		// write requests until the storage versions of the targeting resources are updated.
		s.StorageVersionManager.AddResourceInfo(resourceInfos...)
	}

	return nil
}

func NewAPIGroupVersion(
	c genericapiserver.CompletedConfig,
	scheme *runtime.Scheme,
	parameterCodec runtime.ParameterCodec,
	codecs serializer.CodecFactory,
	groupVersion schema.GroupVersion,
) *genericapi.APIGroupVersion {
	return &genericapi.APIGroupVersion{
		GroupVersion: groupVersion,

		ParameterCodec:        parameterCodec,
		Serializer:            codecs,
		Creater:               scheme,
		Convertor:             scheme,
		ConvertabilityChecker: scheme,
		UnsafeConvertor:       runtime.UnsafeObjectConvertor(scheme),
		Defaulter:             scheme,
		Typer:                 scheme,
		Namer:                 runtime.Namer(meta.NewAccessor()),

		EquivalentResourceRegistry: runtime.NewEquivalentResourceRegistry(),

		Admit:             c.AdmissionControl,
		MinRequestTimeout: time.Duration(c.MinRequestTimeout) * time.Second,
		Authorizer:        c.Authorization.Authorizer,
	}
}
