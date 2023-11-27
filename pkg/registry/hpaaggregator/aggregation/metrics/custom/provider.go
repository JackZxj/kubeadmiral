package custom

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/discovery"
	"k8s.io/metrics/pkg/apis/custom_metrics"
	"k8s.io/metrics/pkg/apis/custom_metrics/v1beta2"
	custommetricsclient "k8s.io/metrics/pkg/client/custom_metrics"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/custom-metrics-apiserver/pkg/provider"

	"github.com/kubewharf/kubeadmiral/pkg/util/informermanager"
)

var (
	converter = custommetricsclient.NewMetricConverter()
)

type CustomMetricsProvider struct {
	federatedInformerManager informermanager.FederatedInformerManager
}

var _ provider.CustomMetricsProvider = &CustomMetricsProvider{}

func NewCustomMetricsProvider(
	federatedInformerManager informermanager.FederatedInformerManager,
) *CustomMetricsProvider {
	p := &CustomMetricsProvider{
		federatedInformerManager: federatedInformerManager,
	}
	return p
}

func (c *CustomMetricsProvider) newCustomMetricsClient(
	cluster string,
) (meta.RESTMapper, custommetricsclient.CustomMetricsClient, error) {
	config, ok := c.federatedInformerManager.GetClusterRestConfig(cluster)
	if !ok {
		return nil, nil, fmt.Errorf("failed to get rest config for %s", cluster)
	}
	client, ok := c.federatedInformerManager.GetClusterDiscoveryClient(cluster)
	if !ok {
		return nil, nil, fmt.Errorf("failed to get discovery client for %s", cluster)
	}
	mapper, err := apiutil.NewDynamicRESTMapper(config, apiutil.WithLazyDiscovery)
	if err != nil {
		return nil, nil, err
	}
	apiVersionsGetter := custommetricsclient.NewAvailableAPIsGetter(client)

	return mapper, custommetricsclient.NewForConfig(config, mapper, apiVersionsGetter), err
}

func (c *CustomMetricsProvider) GetMetricByName(
	ctx context.Context,
	name types.NamespacedName,
	info provider.CustomMetricInfo,
	metricSelector labels.Selector,
) (*custom_metrics.MetricValue, error) {
	clusters, err := c.federatedInformerManager.GetReadyClusters()
	if err != nil {
		return nil, err
	}

	//TODO: context timeout
	var wg sync.WaitGroup
	var lock sync.Mutex
	collections := map[string]*custom_metrics.MetricValue{}
	for _, cluster := range clusters {
		wg.Add(1)
		go func(cluster string) {
			defer wg.Done()

			mapper, client, err := c.newCustomMetricsClient(cluster)
			if err != nil {
				return
			}
			// find the target gvk
			gvk, err := mapper.KindFor(info.GroupResource.WithVersion(""))
			if err != nil {
				return
			}

			var metric *v1beta2.MetricValue
			if info.Namespaced {
				metric, err = client.NamespacedMetrics(name.Namespace).GetForObject(gvk.GroupKind(), name.Name, info.Metric, metricSelector)
				if err != nil {
					return
				}
			} else {
				metric, err = client.RootScopedMetrics().GetForObject(gvk.GroupKind(), name.Name, info.Metric, metricSelector)
				if err != nil {
					return
				}
			}

			var m *custom_metrics.MetricValue
			if err = converter.Scheme().Convert(metric, m, nil); err != nil {
				return
			}

			lock.Lock()
			defer lock.Unlock()
			collections[cluster] = m
		}(cluster.Name)
	}

	wg.Wait()
	var result *custom_metrics.MetricValue
	for _, m := range collections {
		if result == nil {
			result = m
			continue
		}
		result.Value.Add(m.Value)
	}
	if result == nil {
		return nil, provider.NewMetricNotFoundForError(info.GroupResource, info.Metric, name.Name)
	}
	result.Value.Set(result.Value.Value() / int64(len(collections)))
	return result, nil
}

func (c *CustomMetricsProvider) GetMetricBySelector(
	ctx context.Context,
	namespace string,
	selector labels.Selector,
	info provider.CustomMetricInfo,
	metricSelector labels.Selector,
) (*custom_metrics.MetricValueList, error) {
	clusters, err := c.federatedInformerManager.GetReadyClusters()
	if err != nil {
		return nil, err
	}

	//TODO: context timeout
	var wg sync.WaitGroup
	var lock sync.Mutex
	collections := map[string]*custom_metrics.MetricValueList{}
	for _, cluster := range clusters {
		wg.Add(1)
		go func(cluster string) {
			defer wg.Done()

			mapper, client, err := c.newCustomMetricsClient(cluster)
			if err != nil {
				return
			}
			// find the target gvk
			gvk, err := mapper.KindFor(info.GroupResource.WithVersion(""))
			if err != nil {
				return
			}

			var metrics *v1beta2.MetricValueList
			if info.Namespaced {
				metrics, err = client.NamespacedMetrics(namespace).GetForObjects(gvk.GroupKind(), selector, info.Metric, metricSelector)
				if err != nil {
					return
				}
			} else {
				metrics, err = client.RootScopedMetrics().GetForObjects(gvk.GroupKind(), selector, info.Metric, metricSelector)
				if err != nil {
					return
				}
			}

			var m *custom_metrics.MetricValueList
			if err = converter.Scheme().Convert(metrics, m, nil); err != nil {
				return
			}

			lock.Lock()
			defer lock.Unlock()
			collections[cluster] = m
		}(cluster.Name)
	}

	wg.Wait()
	var result *custom_metrics.MetricValueList
	for _, m := range collections {
		if result == nil {
			result = m
			continue
		}
		result.Items = append(result.Items, m.Items...)
	}
	if result == nil || len(result.Items) == 0 {
		return nil, provider.NewMetricNotFoundError(info.GroupResource, info.Metric)
	}
	return result, nil
}

func (c *CustomMetricsProvider) ListAllMetrics() []provider.CustomMetricInfo {
	clusters, err := c.federatedInformerManager.GetReadyClusters()
	if err != nil {
		return []provider.CustomMetricInfo{}
	}

	var wg sync.WaitGroup
	var lock sync.Mutex
	result := sets.Set[provider.CustomMetricInfo]{}
	for _, cluster := range clusters {
		wg.Add(1)
		go func(cluster string) {
			defer wg.Done()

			_, metricInfos := c.getMetricsInfos(cluster)

			lock.Lock()
			defer lock.Unlock()
			result.Insert(metricInfos...)
		}(cluster.Name)
	}

	wg.Wait()
	infos := result.UnsortedList()
	sort.Slice(infos, func(i, j int) bool {
		if infos[i].GroupResource.String() == infos[j].GroupResource.String() {
			return infos[i].Metric < infos[j].Metric
		}
		return infos[i].GroupResource.String() < infos[j].GroupResource.String()
	})
	return infos
}

func (c *CustomMetricsProvider) getMetricsInfos(cluster string) (gv string, resources []provider.CustomMetricInfo) {
	discoveryClient, ok := c.federatedInformerManager.GetClusterDiscoveryClient(cluster)
	if !ok {
		return "", nil
	}

	resourceList, err := getSupportedCustomMetricsAPIVersion(discoveryClient)
	if err != nil {
		return "", nil
	}

	metricInfos := make([]provider.CustomMetricInfo, 0, len(resourceList.APIResources))
	for _, resource := range resourceList.APIResources {
		// The resource name consists of GroupResource and Metric
		info := strings.SplitN(resource.Name, "/", 2)
		if len(info) != 2 {
			continue
		}
		metricInfos = append(metricInfos, provider.CustomMetricInfo{
			GroupResource: schema.ParseGroupResource(info[0]),
			Namespaced:    resource.Namespaced,
			Metric:        info[1],
		})
	}
	return resourceList.GroupVersion, metricInfos
}

func getSupportedCustomMetricsAPIVersion(client discovery.DiscoveryInterface) (resources *metav1.APIResourceList, err error) {
	groups, err := client.ServerGroups()
	if err != nil {
		return nil, err
	}

	var apiVersion string
	for _, group := range groups.Groups {
		if group.Name == custom_metrics.GroupName {
			versions := sets.Set[string]{}
			for _, version := range group.Versions {
				versions.Insert(version.Version)
			}
			for _, version := range custommetricsclient.MetricVersions {
				if versions.Has(version.Version) {
					apiVersion = version.String()
					break
				}
			}
			break
		}
	}

	if apiVersion == "" {
		return &metav1.APIResourceList{}, nil
	}
	return client.ServerResourcesForGroupVersion(apiVersion)
}
