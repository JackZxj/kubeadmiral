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

package automigration

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	pkgruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicclient "k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers"
	kubeclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"

	fedcorev1a1 "github.com/kubewharf/kubeadmiral/pkg/apis/core/v1alpha1"
	"github.com/kubewharf/kubeadmiral/pkg/client/generic"
	"github.com/kubewharf/kubeadmiral/pkg/controllers/automigration/plugins"
	"github.com/kubewharf/kubeadmiral/pkg/controllers/common"
	"github.com/kubewharf/kubeadmiral/pkg/controllers/scheduler/framework"
	"github.com/kubewharf/kubeadmiral/pkg/controllers/util"
	"github.com/kubewharf/kubeadmiral/pkg/controllers/util/delayingdeliver"
	"github.com/kubewharf/kubeadmiral/pkg/controllers/util/eventsink"
	"github.com/kubewharf/kubeadmiral/pkg/controllers/util/managedlabel"
	utilunstructured "github.com/kubewharf/kubeadmiral/pkg/controllers/util/unstructured"
	"github.com/kubewharf/kubeadmiral/pkg/controllers/util/worker"
	"github.com/kubewharf/kubeadmiral/pkg/stats"
)

const (
	EventReasonAutoMigrationInfoUpdated = "AutoMigrationInfoUpdated"
)

/*
The current implementation would query the member apiserver for pods for every reconcile, which is not ideal.
An easy alternative is to cache all pods from all clusters, but this would significantly
reduce scalability based on past experience.

One way to prevent both is:
- Watch pods directly (would the cpu cost be ok?), but cache only unschedulable pods in a map from UID to pod labels
- When a pod is deleted, remove it from the map
- When the status of a target resource is updated, find its unschedulable pods and compute latest estimatedCapacity
*/

type Controller struct {
	typeConfig *fedcorev1a1.FederatedTypeConfig
	name       string

	federatedObjectClient   dynamicclient.NamespaceableResourceInterface
	federatedObjectInformer informers.GenericInformer

	federatedInformer       util.FederatedInformer
	federatedInformerForPod util.FederatedInformer

	worker worker.ReconcileWorker

	eventRecorder record.EventRecorder

	metrics stats.Metrics
	logger  klog.Logger
}

// IsControllerReady implements controllermanager.Controller
func (c *Controller) IsControllerReady() bool {
	return c.HasSynced()
}

func NewAutoMigrationController(
	controllerConfig *util.ControllerConfig,
	typeConfig *fedcorev1a1.FederatedTypeConfig,
	genericFedClient generic.Client,
	kubeClient kubeclient.Interface,
	federatedObjectClient dynamicclient.NamespaceableResourceInterface,
	federatedObjectInformer informers.GenericInformer,
) (*Controller, error) {
	controllerName := fmt.Sprintf("%s-auto-migration", typeConfig.Name)

	c := &Controller{
		typeConfig: typeConfig,
		name:       controllerName,

		federatedObjectClient:   federatedObjectClient,
		federatedObjectInformer: federatedObjectInformer,

		metrics:       controllerConfig.Metrics,
		logger:        klog.NewKlogr().WithValues("controller", "auto-migration", "ftc", typeConfig.Name),
		eventRecorder: eventsink.NewDefederatingRecorderMux(kubeClient, controllerName, 6),
	}

	c.worker = worker.NewReconcileWorker(
		c.reconcile,
		worker.WorkerTiming{},
		controllerConfig.WorkerCount,
		controllerConfig.Metrics,
		delayingdeliver.NewMetricTags("auto-migration-worker", c.typeConfig.GetFederatedType().Kind),
	)

	federatedObjectInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		// Only need to handle UnschedulableThreshold updates
		// Addition and deletion will be triggered by the target resources.
		UpdateFunc: func(oldUntyped, newUntyped interface{}) {
			oldObj, newObj := oldUntyped.(*unstructured.Unstructured), newUntyped.(*unstructured.Unstructured)
			oldThreshold := oldObj.GetAnnotations()[common.PodUnschedulableThresholdAnnotation]
			newThreshold := newObj.GetAnnotations()[common.PodUnschedulableThresholdAnnotation]
			if oldThreshold != newThreshold {
				c.worker.Enqueue(common.NewQualifiedName(newObj))
			}
		},
	})

	var err error
	targetType := typeConfig.GetTargetType()
	c.federatedInformer, err = util.NewFederatedInformer(
		controllerConfig,
		genericFedClient,
		controllerConfig.KubeConfig,
		&targetType,
		func(o pkgruntime.Object) {
			// enqueue with a delay to simulate a rudimentary rate limiter
			c.worker.EnqueueWithDelay(common.NewQualifiedName(o), 10*time.Second)
		},
		&util.ClusterLifecycleHandlerFuncs{},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create federated informer: %w", err)
	}

	c.federatedInformerForPod, err = util.NewFederatedInformerWithEventHandler(
		controllerConfig,
		genericFedClient,
		controllerConfig.KubeConfig,
		&metav1.APIResource{
			Name:       "pods",
			Namespaced: true,
			Group:      corev1.GroupName,
			Version:    "v1",
			Kind:       common.PodKind,
		},
		&util.ResourceEventHandlerFuncsForCluster{
			UpdateFunc: func(oldObj, newObj interface{}, cluster *fedcorev1a1.FederatedCluster) {
				keyedLogger := c.logger.WithValues("cluster", cluster.Name)
				ctx := klog.NewContext(context.TODO(), keyedLogger)

				unsNewObj, ok := newObj.(*unstructured.Unstructured)
				if !ok {
					keyedLogger.Error(nil, fmt.Sprintf("Internal error: newObj not of Unstructured type: %v", newObj))
					return
				}
				if unsNewObj.GetDeletionTimestamp() != nil {
					return
				}
				unsOldObj, ok := oldObj.(*unstructured.Unstructured)
				if !ok {
					keyedLogger.Error(nil, fmt.Sprintf("Internal error: oldObj not of Unstructured type: %v", oldObj))
					return
				}

				newPod := &corev1.Pod{}
				oldPod := &corev1.Pod{}
				if err := pkgruntime.DefaultUnstructuredConverter.FromUnstructured(unsNewObj.Object, newPod); err != nil {
					keyedLogger.Error(nil, fmt.Sprintf("Internal error: newObj not of Pod type: %v", unsNewObj))
					return
				}
				if err := pkgruntime.DefaultUnstructuredConverter.FromUnstructured(unsOldObj.Object, oldPod); err != nil {
					keyedLogger.Error(nil, fmt.Sprintf("Internal error: oldObj not of Pod type: %v", unsOldObj))
					return
				}

				if !podScheduledConditionChanged(oldPod, newPod) {
					return
				}

				qualifiedName, found, err := c.getSourceObjFromCluster(ctx, newPod, cluster.Name)
				if err != nil || !found {
					keyedLogger.V(3).Info(
						"Failed to get source object form pod",
						"pod", common.NewQualifiedName(newPod),
						"err", err,
					)
					return
				}
				// enqueue with a delay to simulate a rudimentary rate limiter
				c.worker.EnqueueWithDelay(*qualifiedName, 10*time.Second)
			},
		},
		nil,
		&util.ClusterLifecycleHandlerFuncs{},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create federated informer for pod: %w", err)
	}

	return c, nil
}

func (c *Controller) Run(ctx context.Context) {
	c.logger.Info("Starting controller")
	defer c.logger.Info("Stopping controller")

	c.federatedInformer.Start()
	defer c.federatedInformer.Stop()
	c.federatedInformerForPod.Start()
	defer c.federatedInformerForPod.Stop()

	if !cache.WaitForNamedCacheSync(c.name, ctx.Done(), c.HasSynced) {
		return
	}
	c.worker.Run(ctx.Done())

	<-ctx.Done()
}

func (c *Controller) HasSynced() bool {
	if !c.federatedObjectInformer.Informer().HasSynced() ||
		!c.federatedInformer.ClustersSynced() ||
		!c.federatedInformerForPod.ClustersSynced() {
		return false
	}

	// We do not wait for the individual clusters' informers to sync to prevent a single faulty informer from blocking the
	// whole automigration controller. If a single cluster's informer is not synced and the cluster objects cannot be
	// retrieved, it will simply be ineligible for automigraiton.

	return true
}

func (c *Controller) reconcile(qualifiedName common.QualifiedName) (status worker.Result) {
	key := qualifiedName.String()
	keyedLogger := c.logger.WithValues("control-loop", "reconcile", "object", key)
	ctx := klog.NewContext(context.TODO(), keyedLogger)

	startTime := time.Now()
	c.metrics.Rate("auto-migration.throughput", 1)
	keyedLogger.V(3).Info("Start reconcile")
	defer func() {
		c.metrics.Duration(fmt.Sprintf("%s.latency", c.name), startTime)
		keyedLogger.V(3).Info("Finished reconcile", "duration", time.Since(startTime), "status", status)
	}()

	fedObject, err := util.UnstructuredFromStore(c.federatedObjectInformer.Informer().GetStore(), key)
	if err != nil {
		keyedLogger.Error(err, "Failed to get object from store")
		return worker.StatusError
	}
	if fedObject == nil || fedObject.GetDeletionTimestamp() != nil {
		return worker.StatusAllOK
	}

	// PodUnschedulableThresholdAnnotation is set by the scheduler. Its presence determines whether auto migration is enabled.
	annotations := fedObject.GetAnnotations()
	var unschedulableThreshold *time.Duration
	if value, exists := annotations[common.PodUnschedulableThresholdAnnotation]; exists {
		if duration, err := time.ParseDuration(value); err != nil {
			keyedLogger.Error(err, "Failed to parse PodUnschedulableThresholdAnnotation")
		} else {
			unschedulableThreshold = &duration
		}
	}

	// auto-migration controller sets AutoMigrationAnnotation to
	// feedback auto-migration information back to the scheduler

	var estimatedCapacity map[string]int64
	var result *worker.Result
	needsUpdate := false
	if unschedulableThreshold == nil {
		// Clean up the annotation if auto migration is disabled.
		keyedLogger.V(3).Info("Auto migration is disabled")
		_, exists := annotations[common.AutoMigrationInfoAnnotation]
		if exists {
			delete(annotations, common.AutoMigrationInfoAnnotation)
			needsUpdate = true
		}
	} else {
		// Keep the annotation up-to-date if auto migration is enabled.
		keyedLogger.V(3).Info("Auto migration is enabled")
		clusterObjs, err := c.federatedInformer.GetTargetStore().GetFromAllClusters(key)
		if err != nil {
			keyedLogger.Error(err, "Failed to get objects from federated informer stores")
			return worker.StatusError
		}

		estimatedCapacity, result = c.estimateCapacity(ctx, clusterObjs, *unschedulableThreshold)
		autoMigrationInfo := &framework.AutoMigrationInfo{EstimatedCapacity: estimatedCapacity}

		// Compare with the existing autoMigration annotation
		existingAutoMigrationInfo := &framework.AutoMigrationInfo{EstimatedCapacity: nil}
		if existingAutoMigrationInfoBytes, exists := annotations[common.AutoMigrationInfoAnnotation]; exists {
			err := json.Unmarshal([]byte(existingAutoMigrationInfoBytes), existingAutoMigrationInfo)
			if err != nil {
				keyedLogger.Error(err, "Existing auto migration annotation is invalid, ignoring")
				// we treat invalid existing annotation as if it doesn't exist
			}
		}

		if !equality.Semantic.DeepEqual(existingAutoMigrationInfo, autoMigrationInfo) {
			autoMigrationInfoBytes, err := json.Marshal(autoMigrationInfo)
			if err != nil {
				keyedLogger.Error(err, "Failed to marshal auto migration")
				return worker.StatusAllOK
			}
			annotations[common.AutoMigrationInfoAnnotation] = string(autoMigrationInfoBytes)
			needsUpdate = true
		}
	}

	keyedLogger.V(3).Info("Observed migration information", "estimatedCapacity", estimatedCapacity)
	if needsUpdate {
		fedObject = fedObject.DeepCopy()
		fedObject.SetAnnotations(annotations)

		keyedLogger.V(1).Info("Updating federated object with auto migration information", "estimatedCapacity", estimatedCapacity)
		_, err = c.federatedObjectClient.
			Namespace(qualifiedName.Namespace).
			Update(ctx, fedObject, metav1.UpdateOptions{})
		if err != nil {
			keyedLogger.Error(err, "Failed to update federated object for auto migration")
			if apierrors.IsConflict(err) {
				return worker.StatusConflict
			}
			return worker.StatusError
		}

		keyedLogger.V(1).Info("Updated federated object with auto migration information", "estimatedCapacity", estimatedCapacity)
		c.eventRecorder.Eventf(
			fedObject,
			corev1.EventTypeNormal,
			EventReasonAutoMigrationInfoUpdated,
			"Auto migration information updated: estimatedCapacity=%+v",
			estimatedCapacity,
		)
	}

	if result == nil {
		return worker.StatusAllOK
	} else {
		return *result
	}
}

func (c *Controller) estimateCapacity(
	ctx context.Context,
	clusterObjs []util.FederatedObject,
	unschedulableThreshold time.Duration,
) (map[string]int64, *worker.Result) {
	keyedLogger := klog.FromContext(ctx)
	needsBackoff := false
	var retryAfter *time.Duration

	estimatedCapacity := make(map[string]int64, len(clusterObjs))

	for _, clusterObj := range clusterObjs {
		keyedLogger := keyedLogger.WithValues("cluster", clusterObj.ClusterName)
		ctx := klog.NewContext(ctx, keyedLogger)

		unsClusterObj := clusterObj.Object.(*unstructured.Unstructured)

		// This is an optimization to skip pod listing when there are no unschedulable pods.
		totalReplicas, readyReplicas, err := c.getTotalAndReadyReplicas(unsClusterObj)
		if err == nil && totalReplicas == readyReplicas {
			keyedLogger.V(3).Info("No unschedulable pods found, skip estimating capacity")
			continue
		}

		desiredReplicas, err := c.getDesiredReplicas(unsClusterObj)
		if err != nil {
			keyedLogger.Error(err, "Failed to get desired replicas from object")
			continue
		}

		keyedLogger.V(2).Info("Getting pods from cluster")
		pods, clusterNeedsBackoff, err := c.getPodsFromCluster(ctx, unsClusterObj, clusterObj.ClusterName)
		if err != nil {
			keyedLogger.Error(err, "Failed to get pods from cluster")
			if clusterNeedsBackoff {
				needsBackoff = true
			}
			continue
		}

		unschedulable, nextCrossIn := countUnschedulablePods(pods, time.Now(), unschedulableThreshold)

		keyedLogger.V(2).Info("Analyzed pods",
			"total", len(pods),
			"desired", desiredReplicas,
			"unschedulable", unschedulable,
		)

		if nextCrossIn != nil && (retryAfter == nil || *nextCrossIn < *retryAfter) {
			retryAfter = nextCrossIn
		}

		var clusterEstimatedCapacity int64
		if len(pods) >= int(desiredReplicas) {
			// When len(pods) >= desiredReplicas, we can immediately determine the capacity by taking the number of
			// schedulable pods.
			clusterEstimatedCapacity = int64(len(pods) - unschedulable)
		} else {
			// If len(pods) < desiredReplicas, we have uncreated pods. We must treat the uncreated pods as schedulable
			// to prevent them from being unnecessarily migrated before creation.
			clusterEstimatedCapacity = desiredReplicas - int64(unschedulable)
		}

		if clusterEstimatedCapacity >= desiredReplicas {
			// There is no need to migrate any pods. We skip writing estimatedCapacity information to avoid unnecessary
			// reconciliation by the scheduler.
			continue
		} else if clusterEstimatedCapacity < 0 {
			// estimatedCapacity should never be < 0. Nonetheless, this safeguard is required since the planner's
			// algorithm does not support negative estimatedCapacity.
			clusterEstimatedCapacity = 0
		}

		estimatedCapacity[clusterObj.ClusterName] = clusterEstimatedCapacity
	}

	var result *worker.Result
	if needsBackoff || retryAfter != nil {
		result = &worker.Result{
			Success:      true,
			RequeueAfter: retryAfter,
			Backoff:      needsBackoff,
		}
	}
	return estimatedCapacity, result
}

func (c *Controller) getTotalAndReadyReplicas(
	unsObj *unstructured.Unstructured,
) (int64, int64, error) {
	// These values might not have been populated by the controller, in which case we default to 0

	totalReplicas := int64(0)
	if replicasPtr, err := utilunstructured.GetInt64FromPath(
		unsObj, c.typeConfig.Spec.PathDefinition.ReplicasStatus, nil,
	); err != nil {
		return 0, 0, fmt.Errorf("replicas: %w", err)
	} else if replicasPtr != nil {
		totalReplicas = *replicasPtr
	}

	readyReplicas := int64(0)
	if readyReplicasPtr, err := utilunstructured.GetInt64FromPath(
		unsObj, c.typeConfig.Spec.PathDefinition.ReadyReplicasStatus, nil,
	); err != nil {
		return 0, 0, fmt.Errorf("ready replicas: %w", err)
	} else if readyReplicasPtr != nil {
		readyReplicas = *readyReplicasPtr
	}

	return totalReplicas, readyReplicas, nil
}

func (c *Controller) getDesiredReplicas(unsObj *unstructured.Unstructured) (int64, error) {
	desiredReplicas, err := utilunstructured.GetInt64FromPath(unsObj, c.typeConfig.Spec.PathDefinition.ReplicasSpec, nil)
	if err != nil {
		return 0, fmt.Errorf("desired replicas: %w", err)
	} else if desiredReplicas == nil {
		return 0, fmt.Errorf("no desired replicas at %s", c.typeConfig.Spec.PathDefinition.ReplicasSpec)
	}

	return *desiredReplicas, nil
}

func (c *Controller) getPodsFromCluster(
	ctx context.Context,
	unsClusterObj *unstructured.Unstructured,
	clusterName string,
) ([]*corev1.Pod, bool, error) {
	plugin, err := plugins.ResolvePlugin(c.typeConfig)
	if err != nil {
		return nil, false, fmt.Errorf("failed to get plugin for FTC: %w", err)
	}

	client, err := c.federatedInformer.GetClientForCluster(clusterName)
	if err != nil {
		return nil, true, fmt.Errorf("failed to get client for cluster: %w", err)
	}

	pods, err := plugin.GetPodsForClusterObject(ctx, unsClusterObj, plugins.ClusterHandle{
		Client: client,
	})
	if err != nil {
		return nil, true, fmt.Errorf("failed to get pods for federated object: %w", err)
	}

	return pods, false, nil
}

func (c *Controller) getSourceObjFromCluster(
	ctx context.Context,
	pod *corev1.Pod,
	clusterName string,
) (sourceQualified *common.QualifiedName, found bool, err error) {
	plugin, err := plugins.ResolvePlugin(c.typeConfig)
	if err != nil {
		return nil, false, fmt.Errorf("failed to get plugin for FTC: %w", err)
	}

	client, err := c.federatedInformer.GetClientForCluster(clusterName)
	if err != nil {
		return nil, true, fmt.Errorf("failed to get client for cluster: %w", err)
	}

	object, found, err := plugin.GetTargetObjectFromPod(ctx, pod, plugins.ClusterHandle{
		Client: client,
	})
	if err != nil {
		return nil, false, fmt.Errorf("failed to get target object form pod: %w", err)
	}
	if !found {
		return nil, false, nil
	}

	gv, err := schema.ParseGroupVersion(object.GetAPIVersion())
	if err != nil {
		return nil, false, fmt.Errorf("failed to parse APIVersion form obj: %w", err)
	}
	qualifiedName := common.NewQualifiedName(object)
	targetType := c.typeConfig.GetTargetType()
	managed := object.GetLabels()[managedlabel.ManagedByKubeAdmiralLabelKey] == managedlabel.ManagedByKubeAdmiralLabelValue

	if targetType.Group != gv.Group || targetType.Kind != object.GetKind() || !managed {
		return nil, false, fmt.Errorf("got obj %s, but it's not the target type or not managed by admiral: {Group:%s Kind:%s Managed:%t}",
			qualifiedName.String(), gv.Group, object.GetKind(), managed)
	}

	return &qualifiedName, true, nil
}
