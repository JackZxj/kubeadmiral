package metrics

import (
	"path"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	genericapi "k8s.io/apiserver/pkg/endpoints"
	genericapiserver "k8s.io/apiserver/pkg/server"
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
) error {
	metricsProvider := custom.NewCustomMetricsProvider(federatedInformerManager)
	root := path.Join(parentPath, "apis")

	groupInfo := genericapiserver.NewDefaultAPIGroupInfo(custom_metrics.GroupName, scheme, parameterCodec, codecs)

	// Register custom metrics REST handler for all supported API versions.
	for _, mainGroupVer := range groupInfo.PrioritizedVersions {
		cmAPI := cmAPI(root, &groupInfo, mainGroupVer, metricsProvider)
		if err := cmAPI.InstallREST(s.Handler.GoRestfulContainer); err != nil {
			return err
		}
	}
	return nil
}

func cmAPI(
	rootPath string,
	groupInfo *genericapiserver.APIGroupInfo,
	groupVersion schema.GroupVersion,
	metricsProvider provider.CustomMetricProvider,
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
