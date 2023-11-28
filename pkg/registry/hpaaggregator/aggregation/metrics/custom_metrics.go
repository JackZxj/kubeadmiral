package metrics

import (
	"path"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	genericapi "k8s.io/apiserver/pkg/endpoints"
	"k8s.io/apiserver/pkg/endpoints/discovery"
	genericapiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/klog/v2"
	"k8s.io/metrics/pkg/apis/custom_metrics"
	specificapi "sigs.k8s.io/custom-metrics-apiserver/pkg/apiserver/installer"
	"sigs.k8s.io/custom-metrics-apiserver/pkg/provider"
	metricstorage "sigs.k8s.io/custom-metrics-apiserver/pkg/registry/custom_metrics"

	"github.com/kubewharf/kubeadmiral/pkg/registry/hpaaggregator/aggregation/metrics/custom"
	"github.com/kubewharf/kubeadmiral/pkg/util/informermanager"
)

func InstallCustomMetricsAPI(
	parentPath string,
	scheme *runtime.Scheme,
	parameterCodec runtime.ParameterCodec,
	codecs serializer.CodecFactory,
	s *genericapiserver.GenericAPIServer,
	federatedInformerManager informermanager.FederatedInformerManager,
	logger klog.Logger,
) error {
	metricsProvider := custom.NewCustomMetricsProvider(
		federatedInformerManager,
		0,
		logger,
	)
	root := path.Join(parentPath, "apis")

	groupInfo := genericapiserver.NewDefaultAPIGroupInfo(custom_metrics.GroupName, scheme, parameterCodec, codecs)
	container := s.Handler.GoRestfulContainer

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

		cmAPI := cmAPI(root, &groupInfo, mainGroupVer, metricsProvider)
		if err := cmAPI.InstallREST(container); err != nil {
			return err
		}
		if versionIndex == 0 {
			s.DiscoveryGroupManager.AddGroup(apiGroup)
			container.Add(discovery.NewAPIGroupHandler(s.Serializer, apiGroup).WebService())

			// add it for discovery
			root := path.Join("/apis", mainGroupVer.String())
			addNewRedirect(root, path.Join(parentPath, root), container)
		}
	}
	return nil
}

func cmAPI(
	rootPath string,
	groupInfo *genericapiserver.APIGroupInfo,
	groupVersion schema.GroupVersion,
	metricsProvider provider.CustomMetricsProvider,
) *specificapi.MetricsAPIGroupVersion {
	resourceStorage := metricstorage.NewREST(metricsProvider)

	return &specificapi.MetricsAPIGroupVersion{
		DynamicStorage: resourceStorage,
		APIGroupVersion: &genericapi.APIGroupVersion{
			Root:             rootPath,
			GroupVersion:     groupVersion,
			MetaGroupVersion: groupInfo.MetaGroupVersion,

			ParameterCodec:  groupInfo.ParameterCodec,
			Serializer:      groupInfo.NegotiatedSerializer,
			Creater:         groupInfo.Scheme,
			Convertor:       groupInfo.Scheme,
			UnsafeConvertor: runtime.UnsafeObjectConvertor(groupInfo.Scheme),
			Typer:           groupInfo.Scheme,
			Namer:           runtime.Namer(meta.NewAccessor()),
		},

		ResourceLister: provider.NewCustomMetricResourceLister(metricsProvider),
		Handlers:       &specificapi.CMHandlers{},
	}
}
