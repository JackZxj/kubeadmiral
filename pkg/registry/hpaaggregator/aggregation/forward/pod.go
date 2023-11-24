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

package forward

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/endpoints/handlers"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/printers"
	printersinternal "k8s.io/kubernetes/pkg/printers/internalversion"
	printerstorage "k8s.io/kubernetes/pkg/printers/storage"

	"github.com/kubewharf/kubeadmiral/pkg/hpaaggregatorapiserver/aggregatedlister"
	"github.com/kubewharf/kubeadmiral/pkg/util/informermanager"
)

//func NewPodHandler(podPath []string, r rest.Responder) http.Handler {
//	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
//		fmt.Println("#### hpaa podPath:", podPath)
//		enc := json.NewEncoder(writer)
//		// TODO: implement pod
//		if len(podPath) > 3 {
//			writer.WriteHeader(http.StatusInternalServerError)
//			_ = enc.Encode(apierrors.NewInternalError(
//				fmt.Errorf("the path %s is not supported now", path.Join(podPath...))),
//			)
//			return
//		}
//		pl := &corev1.PodList{
//			TypeMeta: metav1.TypeMeta{
//				Kind:       "PodList",
//				APIVersion: "v1",
//			},
//			ListMeta: metav1.ListMeta{
//				ResourceVersion:    time.Now().Format(time.RFC3339),
//				Continue:           "",
//				RemainingItemCount: nil,
//			},
//			Items: []corev1.Pod{
//				{
//					ObjectMeta: metav1.ObjectMeta{
//						Name:              "test1",
//						Namespace:         "default",
//						ResourceVersion:   "123",
//						CreationTimestamp: metav1.Now(),
//						UID:               "fa8255dc-adb1-4d25-b5b7-b41860e11bb2",
//					},
//					Spec: corev1.PodSpec{
//						Containers: []corev1.Container{
//							{
//								Name:                     "c1",
//								Image:                    "nginx:alpine",
//								Resources:                corev1.ResourceRequirements{},
//								TerminationMessagePath:   "/dev/termination-log",
//								TerminationMessagePolicy: "File",
//								ImagePullPolicy:          "Always",
//							},
//						},
//						RestartPolicy:                 "Always",
//						TerminationGracePeriodSeconds: pointer.Int64(30),
//						DNSPolicy:                     "ClusterFirst",
//					},
//					Status: corev1.PodStatus{
//						Phase:    "Pending",
//						QOSClass: "BestEffort",
//					},
//				},
//				{
//					ObjectMeta: metav1.ObjectMeta{
//						Name:              "test2",
//						Namespace:         "default",
//						ResourceVersion:   "321",
//						CreationTimestamp: metav1.Now(),
//						UID:               "fa8255dc-adb1-4d25-b5b7-b41860e11bb3",
//					},
//					Spec: corev1.PodSpec{
//						Containers: []corev1.Container{
//							{
//								Name:                     "c1",
//								Image:                    "nginx:alpine",
//								Resources:                corev1.ResourceRequirements{},
//								TerminationMessagePath:   "/dev/termination-log",
//								TerminationMessagePolicy: "File",
//								ImagePullPolicy:          "Always",
//							},
//						},
//						RestartPolicy:                 "Always",
//						TerminationGracePeriodSeconds: pointer.Int64(30),
//						DNSPolicy:                     "ClusterFirst",
//					},
//					Status: corev1.PodStatus{
//						Phase:    "Pending",
//						QOSClass: "BestEffort",
//					},
//				},
//			},
//		}
//		writer.Header().Set("Content-Type", "application/json")
//		_ = enc.Encode(pl)
//	})
//}

var scheme = runtime.NewScheme()
var codecs = serializer.NewCodecFactory(scheme)
var localSchemeBuilder = runtime.SchemeBuilder{
	corev1.AddToScheme,
}
var addToScheme = localSchemeBuilder.AddToScheme

func init() {
	metav1.AddToGroupVersion(scheme, schema.GroupVersion{Version: "v1"})
	utilruntime.Must(addToScheme(scheme))
}

type PodHandler interface {
	RunHandler(ctx context.Context) (http.Handler, error)
}

type PodREST struct {
	podLister                cache.GenericLister
	federatedInformerManager informermanager.FederatedInformerManager
	minRequestTimeout        time.Duration

	serializer runtime.NegotiatedSerializer
	scheme     *runtime.Scheme

	tableConvertor rest.TableConvertor
}

var _ rest.Getter = &PodREST{}
var _ rest.Lister = &PodREST{}
var _ rest.Watcher = &PodREST{}

func NewPodREST(
	f informermanager.FederatedInformerManager,
	podLister cache.GenericLister,
	//serializer runtime.NegotiatedSerializer,
	//scheme *runtime.Scheme,
	minRequestTimeout time.Duration,
) *PodREST {
	return &PodREST{
		federatedInformerManager: f,
		podLister:                podLister,
		minRequestTimeout:        minRequestTimeout,
		//serializer:               serializer,
		//scheme:                   scheme,
		tableConvertor: printerstorage.TableConvertor{TableGenerator: printers.NewTableGenerator().With(printersinternal.AddHandlers)},
	}
}

func (p *PodREST) RunHandler(ctx context.Context) (http.Handler, error) {
	requestInfo, found := genericapirequest.RequestInfoFrom(ctx)
	if !found {
		return nil, errors.New("no RequestInfo found in the context")
	}

	scope := p.newScoper(requestInfoResolver)

	switch requestInfo.Verb {
	case "get":
		return handlers.GetResource(p, scope), nil
	case "list", "watch":
		return handlers.ListResource(p, p, scope, false, p.minRequestTimeout), nil
	default:
		return nil, apierrors.NewMethodNotSupported(schema.GroupResource{
			Group:    requestInfo.APIGroup,
			Resource: requestInfo.Resource,
		}, requestInfo.Verb)
	}
}

func (p *PodREST) newScoper(r *genericapirequest.RequestInfoFactory) *handlers.RequestScope {
	return &handlers.RequestScope{
		Namer: &ContextBasedNaming{
			Namer:         runtime.Namer(meta.NewAccessor()),
			ClusterScoped: false,
			resolver:      r,
		},
		Serializer:       codecs,
		Kind:             corev1.SchemeGroupVersion.WithKind("Pod"),
		TableConvertor:   p,
		Convertor:        scheme,
		MetaGroupVersion: metav1.SchemeGroupVersion,
		Resource:         corev1.SchemeGroupVersion.WithResource("pods"),
	}
}

//func (p *PodREST) newScoper2() *handlers.RequestScope {
//	return &handlers.RequestScope{
//		Namer: handlers.ContextBasedNaming{
//			Namer:         runtime.Namer(meta.NewAccessor()),
//			ClusterScoped: false,
//		},
//		Serializer:       p.serializer,
//		Kind:             corev1.SchemeGroupVersion.WithKind("Pod"),
//		TableConvertor:   p,
//		Convertor:        p.scheme,
//		MetaGroupVersion: metav1.SchemeGroupVersion,
//		Resource:         corev1.SchemeGroupVersion.WithResource("pods"),
//	}
//}

// Get ...
func (p *PodREST) Get(ctx context.Context, name string, opts *metav1.GetOptions) (runtime.Object, error) {
	namespace := genericapirequest.NamespaceValue(ctx)
	obj, err := p.podLister.ByNamespace(namespace).Get(name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// return not-found errors directly
			return &corev1.Pod{}, err
		}
		klog.ErrorS(err, "Failed getting pod", "pod", klog.KRef(namespace, name))
		return &corev1.Pod{}, fmt.Errorf("failed getting pod: %w", err)
	}
	return obj, nil
}

func (p *PodREST) NewList() runtime.Object {
	return &corev1.PodList{}
}

func (p *PodREST) List(ctx context.Context, options *metainternalversion.ListOptions) (runtime.Object, error) {
	label := labels.Everything()
	if options != nil && options.LabelSelector != nil {
		label = options.LabelSelector
	}

	namespace := genericapirequest.NamespaceValue(ctx)
	objs, err := p.podLister.ByNamespace(namespace).List(label)
	if err != nil {
		klog.ErrorS(err, "Failed listing pods", "labelSelector", label, "namespace", klog.KRef("", namespace))
		return nil, fmt.Errorf("failed listing pods: %w", err)
	}

	field := fields.Everything()
	if options != nil && options.FieldSelector != nil {
		field = options.FieldSelector
	}
	pods := filterPodObject(objs, field)
	return &corev1.PodList{Items: pods}, nil
}

func (p *PodREST) ConvertToTable(ctx context.Context, object runtime.Object, tableOptions runtime.Object) (*metav1.Table, error) {
	return p.tableConvertor.ConvertToTable(ctx, object, tableOptions)
}

func (p *PodREST) Watch(ctx context.Context, options *metainternalversion.ListOptions) (watch.Interface, error) {
	label := labels.Everything()
	if options != nil && options.LabelSelector != nil {
		label = options.LabelSelector
	}

	namespace := genericapirequest.NamespaceValue(ctx)

	clusters, err := p.federatedInformerManager.GetReadyClusters()
	if err != nil {
		klog.ErrorS(err, "Failed watching pods", "labelSelector", label, "namespace", klog.KRef("", namespace))
		return nil, fmt.Errorf("failed watching pods: %w", err)
	}

	watchClusters := sets.Set[string]{}
	proxyCh := make(chan watch.Event)
	proxyWatcher := watch.NewProxyWatcher(proxyCh)
	for i := range clusters {
		client, exist := p.federatedInformerManager.GetClusterKubeClient(clusters[i].Name)
		if !exist {
			continue
		}
		watcher, err := client.CoreV1().Pods(namespace).Watch(ctx, metav1.ListOptions{
			LabelSelector: label.String(),
			FieldSelector: options.FieldSelector.String(),
		})
		if err != nil {
			continue
		}
		watchClusters.Insert(clusters[i].Name)
		go func(cluster string) {
			defer watcher.Stop()
			for {
				select {
				case <-proxyWatcher.StopChan():
					return
				case event, ok := <-watcher.ResultChan():
					if !ok {
						watchClusters.Delete(cluster)
						if watchClusters.Len() == 0 {
							close(proxyCh)
						}
						return
					}
					if pod, ok := event.Object.(*corev1.Pod); ok {
						informermanager.PrunePod(pod)
						aggregatedlister.MakePodUnique(pod, cluster)
					}
					proxyCh <- event
				}
			}
		}(clusters[i].Name)
	}
	return proxyWatcher, nil
}

func filterPodObject(objs []runtime.Object, selector fields.Selector) []corev1.Pod {
	newObjs := make([]corev1.Pod, 0, len(objs))
	for _, obj := range objs {
		pod, ok := obj.(*corev1.Pod)
		if !ok {
			continue
		}
		fields := ToSelectableFields(pod)
		if !selector.Matches(fields) {
			continue
		}
		newObjs = append(newObjs, *pod)
	}
	return newObjs
}

// ToSelectableFields returns a field set that represents the object
// TODO: fields are not labels, and the validation rules for them do not apply.
func ToSelectableFields(pod *corev1.Pod) fields.Set {
	// The purpose of allocation with a given number of elements is to reduce
	// amount of allocations needed to create the fields.Set. If you add any
	// field here or the number of object-meta related fields changes, this should
	// be adjusted.
	podSpecificFieldsSet := make(fields.Set, 10)
	podSpecificFieldsSet["spec.nodeName"] = pod.Spec.NodeName
	podSpecificFieldsSet["spec.restartPolicy"] = string(pod.Spec.RestartPolicy)
	podSpecificFieldsSet["spec.schedulerName"] = pod.Spec.SchedulerName
	podSpecificFieldsSet["spec.serviceAccountName"] = pod.Spec.ServiceAccountName
	podSpecificFieldsSet["spec.hostNetwork"] = strconv.FormatBool(pod.Spec.HostNetwork)
	podSpecificFieldsSet["status.phase"] = string(pod.Status.Phase)
	// TODO: add podIPs as a downward API value(s) with proper format
	podIP := ""
	if len(pod.Status.PodIPs) > 0 {
		podIP = pod.Status.PodIPs[0].IP
	}
	podSpecificFieldsSet["status.podIP"] = podIP
	podSpecificFieldsSet["status.nominatedNodeName"] = pod.Status.NominatedNodeName
	return generic.AddObjectMetaFieldsSet(podSpecificFieldsSet, &pod.ObjectMeta, true)
}
