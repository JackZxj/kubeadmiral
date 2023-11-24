package aggregatedlister

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"

	"github.com/kubewharf/kubeadmiral/pkg/util/informermanager"
)

type PodLister struct {
	federatedInformerManager informermanager.FederatedInformerManager
}

type PodNamespaceLister struct {
	namespace string

	federatedInformerManager informermanager.FederatedInformerManager
}

var _ cache.GenericLister = &PodLister{}
var _ cache.GenericNamespaceLister = &PodNamespaceLister{}

func NewPodLister(informer informermanager.FederatedInformerManager) *PodLister {
	return &PodLister{federatedInformerManager: informer}
}

func (p *PodLister) List(selector labels.Selector) (ret []runtime.Object, err error) {
	clusters, err := p.federatedInformerManager.GetReadyClusters()
	if err != nil {
		return nil, err
	}
	for _, cluster := range clusters {
		podLister, podsSynced, exists := p.federatedInformerManager.GetPodLister(cluster.Name)
		if !exists || !podsSynced() {
			continue
		}
		pods, err := podLister.List(selector)
		if err != nil {
			continue
		}
		for i := range pods {
			pod := pods[i].DeepCopy()
			MakePodUnique(pod, cluster.Name)
			ret = append(ret, pod)
		}
	}
	fmt.Println("list pod", selector, ret)
	return ret, nil
}

func (p *PodLister) Get(name string) (runtime.Object, error) {
	items := strings.Split(name, "/")
	if len(items) != 2 {
		return nil, errors.NewBadRequest(fmt.Sprintf("invalid name %q", name))
	}
	return p.ByNamespace(items[0]).Get(items[1])
}

func (p *PodLister) ByNamespace(namespace string) cache.GenericNamespaceLister {
	return &PodNamespaceLister{federatedInformerManager: p.federatedInformerManager, namespace: namespace}
}

func (p *PodNamespaceLister) List(selector labels.Selector) (ret []runtime.Object, err error) {
	clusters, err := p.federatedInformerManager.GetReadyClusters()
	if err != nil {
		return nil, err
	}
	for _, cluster := range clusters {
		podLister, podsSynced, exists := p.federatedInformerManager.GetPodLister(cluster.Name)
		if !exists || !podsSynced() {
			fmt.Println("pod lister failed", exists, podsSynced(), cluster.Name)
			continue
		}
		pods, err := podLister.Pods(p.namespace).List(selector)
		if err != nil {
			fmt.Println("list namespace pod failed", selector, p.namespace, err)
			continue
		}
		for i := range pods {
			pod := pods[i].DeepCopy()
			MakePodUnique(pod, cluster.Name)
			ret = append(ret, pod)
		}
		fmt.Println("list namespace pod cluster", cluster, p.namespace, ret)
	}
	fmt.Println("list namespace pod", selector, p.namespace, ret)
	return ret, nil
}

func (p *PodNamespaceLister) Get(name string) (runtime.Object, error) {
	clusters, err := p.federatedInformerManager.GetReadyClusters()
	if err != nil {
		return nil, err
	}

	for _, cluster := range GetPossibleClusters(clusters, name) {
		podLister, podsSynced, exists := p.federatedInformerManager.GetPodLister(cluster)
		if !exists || !podsSynced() {
			continue
		}
		pods, err := podLister.Pods(p.namespace).List(labels.Everything())
		if err != nil {
			continue
		}
		fmt.Println("get pod #### pod list", cluster, pods)
		for i := range pods {
			if name == GenUniqueName(cluster, pods[i].Name) {
				pod := pods[i].DeepCopy()
				MakePodUnique(pod, cluster)
				fmt.Println("get pod", name, pod)
				return pod, nil
			}
		}
	}
	fmt.Println("get pod", name, clusters)
	fmt.Println(p.federatedInformerManager.GetJoinedClusters())
	return nil, errors.NewNotFound(corev1.Resource("pod"), name)
}
