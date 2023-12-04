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

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	genericapi "k8s.io/apiserver/pkg/endpoints"
	"k8s.io/apiserver/pkg/endpoints/discovery"
	"k8s.io/apiserver/pkg/features"
	"k8s.io/apiserver/pkg/registry/rest"
	genericapiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/apiserver/pkg/storageversion"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	corev1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/metrics/pkg/apis/metrics"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"

	"github.com/kubewharf/kubeadmiral/pkg/registry/hpaaggregator/aggregation/metrics/resource"
)

func InstallResourceMetrics(
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
	groupInfo := genericapiserver.NewDefaultAPIGroupInfo(metrics.GroupName, scheme, parameterCodec, codecs)
	container := s.Handler.GoRestfulContainer
	var resourceInfos []*storageversion.ResourceInfo

	// Register custom metrics REST handler for all supported API versions.
	for versionIndex, mainGroupVer := range groupInfo.PrioritizedVersions {
		preferredVersionForDiscovery := metav1.GroupVersionForDiscovery{
			GroupVersion: mainGroupVer.String(),
			Version:      mainGroupVer.Version,
		}
		groupVersion := metav1.GroupVersionForDiscovery{
			GroupVersion: mainGroupVer.String(),
			Version:      mainGroupVer.Version,
		}
		apiGroup := metav1.APIGroup{
			Name:             mainGroupVer.Group,
			Versions:         []metav1.GroupVersionForDiscovery{groupVersion},
			PreferredVersion: preferredVersionForDiscovery,
		}

		api, destroy := resourceAPI(
			path.Join(parentPath, "apis"),
			c,
			&groupInfo,
			mainGroupVer,
			m,
			podMetadataLister,
			nodeLister,
			nodeSelector,
		)
		s.RegisterDestroyFunc(destroy)

		_, r, err := api.InstallREST(s.Handler.GoRestfulContainer)
		if err != nil {
			return fmt.Errorf("unable to setup API %v: %v", api, err)
		}

		resourceInfos = append(resourceInfos, r...)
		if versionIndex == 0 {
			s.DiscoveryGroupManager.AddGroup(apiGroup)
			container.Add(discovery.NewAPIGroupHandler(s.Serializer, apiGroup).WebService())

			// add it for discovery
			root := path.Join("/apis", mainGroupVer.String())
			addNewRedirect(root, path.Join(parentPath, root), container)
		}
	}

	if utilfeature.DefaultFeatureGate.Enabled(features.StorageVersionAPI) &&
		utilfeature.DefaultFeatureGate.Enabled(features.APIServerIdentity) {
		// API installation happens before we start listening on the handlers,
		// therefore it is safe to register ResourceInfos here. The handler will block
		// write requests until the storage versions of the targeting resources are updated.
		s.StorageVersionManager.AddResourceInfo(resourceInfos...)
	}

	return nil
}

func resourceAPI(
	rootPath string,
	c genericapiserver.CompletedConfig,
	groupInfo *genericapiserver.APIGroupInfo,
	groupVersion schema.GroupVersion,
	m resource.MetricsGetter,
	podMetadataLister cache.GenericLister,
	nodeLister corev1.NodeLister,
	nodeSelector []labels.Requirement,
) (api *genericapi.APIGroupVersion, destroyFn func()) {
	storage := make(map[string]rest.Storage)
	node := resource.NewNodeMetrics(metrics.Resource("nodemetrics"), m, nodeLister, nodeSelector)
	pod := resource.NewPodMetrics(metrics.Resource("podmetrics"), m, podMetadataLister)
	storage["nodes"] = node
	storage["pods"] = pod

	destroyFn = func() {
		node.Destroy()
		pod.Destroy()
	}

	return &genericapi.APIGroupVersion{
		Storage:      storage,
		Root:         genericapiserver.APIGroupPrefix,
		GroupVersion: groupVersion,

		ParameterCodec:        groupInfo.ParameterCodec,
		Serializer:            groupInfo.NegotiatedSerializer,
		Creater:               groupInfo.Scheme,
		Convertor:             groupInfo.Scheme,
		ConvertabilityChecker: groupInfo.Scheme,
		UnsafeConvertor:       runtime.UnsafeObjectConvertor(groupInfo.Scheme),
		Defaulter:             groupInfo.Scheme,
		Typer:                 groupInfo.Scheme,
		Namer:                 runtime.Namer(meta.NewAccessor()),

		EquivalentResourceRegistry: runtime.NewEquivalentResourceRegistry(),

		Admit:             c.AdmissionControl,
		MinRequestTimeout: time.Duration(c.MinRequestTimeout) * time.Second,
		Authorizer:        c.Authorization.Authorizer,
	}, destroyFn
}

// BuildResourceMetrics constructs APIGroupInfo the metrics.k8s.io API group using the given getters.
func BuildResourceMetrics(
	scheme *runtime.Scheme,
	parameterCodec runtime.ParameterCodec,
	codecs serializer.CodecFactory,
	m resource.MetricsGetter,
	podMetadataLister cache.GenericLister,
	nodeLister corev1.NodeLister,
	nodeSelector []labels.Requirement,
) genericapiserver.APIGroupInfo {
	node := resource.NewNodeMetrics(metrics.Resource("nodemetrics"), m, nodeLister, nodeSelector)
	pod := resource.NewPodMetrics(metrics.Resource("podmetrics"), m, podMetadataLister)

	apiGroupInfo := genericapiserver.NewDefaultAPIGroupInfo(metrics.GroupName, scheme, parameterCodec, codecs)
	metricsServerResources := map[string]rest.Storage{
		"nodes": node,
		"pods":  pod,
	}
	apiGroupInfo.VersionedResourcesStorageMap[metricsv1beta1.SchemeGroupVersion.Version] = metricsServerResources

	return apiGroupInfo
}
