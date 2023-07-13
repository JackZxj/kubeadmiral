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

package informermanager

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"sync"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	fedcorev1a1 "github.com/kubewharf/kubeadmiral/pkg/apis/core/v1alpha1"
	fedcorev1a1informers "github.com/kubewharf/kubeadmiral/pkg/client/informers/externalversions/core/v1alpha1"
	fedcorev1a1listers "github.com/kubewharf/kubeadmiral/pkg/client/listers/core/v1alpha1"
	clusterutil "github.com/kubewharf/kubeadmiral/pkg/util/cluster"
	"github.com/kubewharf/kubeadmiral/pkg/util/logging"
	"github.com/kubewharf/kubeadmiral/pkg/util/managedlabel"
)

type federatedInformerManager struct {
	lock sync.RWMutex

	started  bool
	shutdown bool

	clientGetter    ClusterClientGetter
	ftcInformer     fedcorev1a1informers.FederatedTypeConfigInformer
	clusterInformer fedcorev1a1informers.FederatedClusterInformer

	eventHandlerGenerators []*EventHandlerGenerator
	clusterEventHandlers   []*ClusterEventHandler

	clients                     map[string]dynamic.Interface
	connectionMap               map[string][]byte
	informerManagers            map[string]InformerManager
	informerManagersCancelFuncs map[string]context.CancelFunc

	queue workqueue.RateLimitingInterface
}

func NewFederatedInformerManager(
	clientGetter ClusterClientGetter,
	ftcInformer fedcorev1a1informers.FederatedTypeConfigInformer,
	clusterInformer fedcorev1a1informers.FederatedClusterInformer,
) FederatedInformerManager {
	manager := &federatedInformerManager{
		lock:                        sync.RWMutex{},
		started:                     false,
		shutdown:                    false,
		clientGetter:                clientGetter,
		ftcInformer:                 ftcInformer,
		clusterInformer:             clusterInformer,
		eventHandlerGenerators:      []*EventHandlerGenerator{},
		clusterEventHandlers:        []*ClusterEventHandler{},
		clients:                     map[string]dynamic.Interface{},
		connectionMap:               map[string][]byte{},
		informerManagers:            map[string]InformerManager{},
		informerManagersCancelFuncs: map[string]context.CancelFunc{},
		queue:                       workqueue.NewRateLimitingQueue(workqueue.DefaultItemBasedRateLimiter()),
	}

	clusterInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			cluster := obj.(*fedcorev1a1.FederatedCluster)
			return clusterutil.IsClusterJoined(&cluster.Status)
		},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    func(obj interface{}) { manager.enqueue(obj) },
			UpdateFunc: func(_ interface{}, obj interface{}) { manager.enqueue(obj) },
			DeleteFunc: func(obj interface{}) { manager.enqueue(obj) },
		},
	})

	ftcInformer.Informer()

	return manager
}

func (m *federatedInformerManager) enqueue(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		klog.Error(err, "federated-informer-manager: Failed to enqueue FederatedCluster")
		return
	}
	m.queue.Add(key)
}

func (m *federatedInformerManager) worker(ctx context.Context) {
	key, shutdown := m.queue.Get()
	if shutdown {
		return
	}
	defer m.queue.Done(key)

	logger := klog.FromContext(ctx)

	_, name, err := cache.SplitMetaNamespaceKey(key.(string))
	if err != nil {
		logger.Error(err, "Failed to process FederatedCluster")
		return
	}

	ctx, logger = logging.InjectLoggerValues(ctx, "cluster", name)

	cluster, err := m.clusterInformer.Lister().Get(name)
	if err != nil && !apierrors.IsNotFound(err) {
		logger.Error(err, "Failed to get FederatedCluster from lister, will retry")
		m.queue.AddRateLimited(key)
		return
	}
	if apierrors.IsNotFound(err) || !clusterutil.IsClusterJoined(&cluster.Status) {
		if err := m.processClusterDeletion(ctx, name); err != nil {
			logger.Error(err, "Failed to process FederatedCluster, will retry")
			m.queue.AddRateLimited(key)
		} else {
			m.queue.Forget(key)
		}
		return
	}

	err, needReenqueue := m.processCluster(ctx, cluster)
	if err != nil {
		if needReenqueue {
			logger.Error(err, "Failed to process FederatedCluster, will retry")
			m.queue.AddRateLimited(key)
		} else {
			logger.Error(err, "Failed to process FederatedCluster")
			m.queue.Forget(key)
		}
		return
	}

	m.queue.Forget(key)
	if needReenqueue {
		m.queue.Add(key)
	}
}

func (m *federatedInformerManager) processCluster(
	ctx context.Context,
	cluster *fedcorev1a1.FederatedCluster,
) (err error, needReenqueue bool) {
	m.lock.Lock()
	defer m.lock.Unlock()

	clusterName := cluster.Name

	connectionHash, err := m.clientGetter.ConnectionHash(cluster)
	if err != nil {
		return fmt.Errorf("failed to get connection hash for cluster %s: %w", clusterName, err), true
	}
	if oldConnectionHash, exists := m.connectionMap[clusterName]; exists {
		if !bytes.Equal(oldConnectionHash, connectionHash) {
			// This might occur if a cluster was deleted and recreated with different connection details within a short
			// period of time and we missed processing the deletion. We simply process the cluster deletion and
			// reenqueue.
			// Note: updating of cluster connection details, however, is still not a supported use case.
			err := m.processClusterDeletionUnlocked(ctx, clusterName)
			return err, true
		}
	} else {
		clusterClient, err := m.clientGetter.ClientGetter(cluster)
		if err != nil {
			return fmt.Errorf("failed to get client for cluster %s: %w", clusterName, err), true
		}

		manager := NewInformerManager(
			clusterClient,
			m.ftcInformer,
			func(opts *metav1.ListOptions) {
				selector := &metav1.LabelSelector{}
				metav1.AddLabelToSelector(
					selector,
					managedlabel.ManagedByKubeAdmiralLabelKey,
					managedlabel.ManagedByKubeAdmiralLabelValue,
				)
				opts.LabelSelector = metav1.FormatLabelSelector(selector)
			},
		)

		ctx, cancel := context.WithCancel(ctx)
		for _, generator := range m.eventHandlerGenerators {
			if err := manager.AddEventHandlerGenerator(generator); err != nil {
				cancel()
				return fmt.Errorf("failed to initialized InformerManager for cluster %s: %w", clusterName, err), true
			}
		}

		klog.FromContext(ctx).V(2).Info("Starting new InformerManager for FederatedCluster")

		manager.Start(ctx)

		m.connectionMap[clusterName] = connectionHash
		m.clients[clusterName] = clusterClient
		m.informerManagers[clusterName] = manager
		m.informerManagersCancelFuncs[clusterName] = cancel
	}

	return nil, false
}

func (m *federatedInformerManager) processClusterDeletion(ctx context.Context, clusterName string) error {
	m.lock.Lock()
	defer m.lock.Unlock()
	return m.processClusterDeletionUnlocked(ctx, clusterName)
}

func (m *federatedInformerManager) processClusterDeletionUnlocked(ctx context.Context, clusterName string) error {
	delete(m.connectionMap, clusterName)
	delete(m.clients, clusterName)

	if cancel, ok := m.informerManagersCancelFuncs[clusterName]; ok {
		klog.FromContext(ctx).V(2).Info("Stopping InformerManager for FederatedCluster")
		cancel()
	}
	delete(m.informerManagers, clusterName)
	delete(m.informerManagersCancelFuncs, clusterName)

	return nil
}

func (m *federatedInformerManager) AddClusterEventHandler(handler *ClusterEventHandler) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.started {
		return fmt.Errorf("failed to add ClusterEventHandler: FederatedInformerManager is already started")
	}

	m.clusterEventHandlers = append(m.clusterEventHandlers, handler)
	return nil
}

func (m *federatedInformerManager) AddEventHandlerGenerator(generator *EventHandlerGenerator) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.started {
		return fmt.Errorf("failed to add EventHandlerGenerator: FederatedInformerManager is already started")
	}

	m.eventHandlerGenerators = append(m.eventHandlerGenerators, generator)
	return nil
}

func (m *federatedInformerManager) GetClusterClient(cluster string) (client dynamic.Interface, exists bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()
	client, ok := m.clients[cluster]
	return client, ok
}

func (m *federatedInformerManager) GetFederatedClusterLister() fedcorev1a1listers.FederatedClusterLister {
	return m.clusterInformer.Lister()
}

func (m *federatedInformerManager) GetFederatedTypeConfigLister() fedcorev1a1listers.FederatedTypeConfigLister {
	return m.ftcInformer.Lister()
}

func (m *federatedInformerManager) GetResourceLister(
	gvk schema.GroupVersionKind,
	cluster string,
) (lister cache.GenericLister, informerSynced cache.InformerSynced, exists bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	manager, ok := m.informerManagers[cluster]
	if !ok {
		return nil, nil, false
	}

	return manager.GetResourceLister(gvk)
}

func (m *federatedInformerManager) HasSynced() bool {
	return m.ftcInformer.Informer().HasSynced() && m.clusterInformer.Informer().HasSynced()
}

func (m *federatedInformerManager) Start(ctx context.Context) {
	m.lock.Lock()
	defer m.lock.Unlock()

	ctx, logger := logging.InjectLoggerName(ctx, "federated-informer-manager")

	if m.started {
		logger.Error(nil, "FederatedInformerManager cannot be started more than once")
		return
	}

	m.started = true

	if !cache.WaitForCacheSync(ctx.Done(), m.HasSynced) {
		logger.Error(nil, "Failed to wait for FederatedInformerManager cache sync")
		return
	}

	for _, handler := range m.clusterEventHandlers {
		predicate := handler.Predicate
		callback := handler.Callback

		m.clusterInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				cluster := obj.(*fedcorev1a1.FederatedCluster)
				if predicate(nil, cluster) {
					callback(cluster)
				}
			},
			UpdateFunc: func(oldObj interface{}, newObj interface{}) {
				oldCluster := oldObj.(*fedcorev1a1.FederatedCluster)
				newCluster := newObj.(*fedcorev1a1.FederatedCluster)
				if predicate(oldCluster, newCluster) {
					callback(newCluster)
				}
			},
			DeleteFunc: func(obj interface{}) {
				cluster := obj.(*fedcorev1a1.FederatedCluster)
				if predicate(cluster, nil) {
					callback(cluster)
				}
			},
		})
	}

	go wait.UntilWithContext(ctx, m.worker, 0)
	go func() {
		<-ctx.Done()

		m.lock.Lock()
		defer m.lock.Unlock()

		logger.V(2).Info("Stopping FederatedInformerManager")
		m.queue.ShutDown()
		m.shutdown = true
	}()
}

func (m *federatedInformerManager) IsShutdown() bool {
	m.lock.RLock()
	defer m.lock.RUnlock()
	return m.shutdown
}

var _ FederatedInformerManager = &federatedInformerManager{}

func DefaultClusterConnectionHash(cluster *fedcorev1a1.FederatedCluster) ([]byte, error) {
	hashObj := struct {
		APIEndpoint            string
		SecretName             string
		UseServiceAccountToken bool
	}{
		APIEndpoint:            cluster.Spec.APIEndpoint,
		SecretName:             cluster.Spec.SecretRef.Name,
		UseServiceAccountToken: cluster.Spec.UseServiceAccountToken,
	}

	var b bytes.Buffer
	if err := gob.NewEncoder(&b).Encode(hashObj); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
