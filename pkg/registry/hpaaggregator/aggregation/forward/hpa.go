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
	"time"

	autoscalingv1 "k8s.io/api/autoscaling/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	structuralschema "k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/endpoints/handlers"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
	restclient "k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/apis/autoscaling"

	fedcorev1a1 "github.com/kubewharf/kubeadmiral/pkg/apis/core/v1alpha1"
	"github.com/kubewharf/kubeadmiral/pkg/controllers/common"
)

//func NewHPAHandler(hpaPath []string, r rest.Responder) http.Handler {
//	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
//		enc := json.NewEncoder(writer)
//		// TODO: implement hpa
//		if len(hpaPath) > 3 {
//			writer.WriteHeader(http.StatusInternalServerError)
//			_ = enc.Encode(apierrors.NewInternalError(
//				fmt.Errorf("the path %s is not supported now", path.Join(hpaPath...))),
//			)
//			return
//		}
//		hl := &autoscalingv2beta2.HorizontalPodAutoscalerList{
//			TypeMeta: metav1.TypeMeta{
//				Kind:       "HorizontalPodAutoscalerList",
//				APIVersion: "v1",
//			},
//			ListMeta: metav1.ListMeta{
//				ResourceVersion:    time.Now().Format(time.RFC3339),
//				Continue:           "",
//				RemainingItemCount: nil,
//			},
//			Items: []autoscalingv2beta2.HorizontalPodAutoscaler{
//				{
//					ObjectMeta: metav1.ObjectMeta{
//						Name:              "test1",
//						Namespace:         "default",
//						ResourceVersion:   "123",
//						CreationTimestamp: metav1.Now(),
//						UID:               "fa8255dc-adb1-4d25-b5b7-b41860e11hpa",
//					},
//					Spec: autoscalingv2beta2.HorizontalPodAutoscalerSpec{
//						ScaleTargetRef: autoscalingv2beta2.CrossVersionObjectReference{
//							Kind:       "Deployment",
//							Name:       "test",
//							APIVersion: "apps/v1",
//						},
//						MinReplicas: pointer.Int32(1),
//						MaxReplicas: 10,
//						Metrics: []autoscalingv2beta2.MetricSpec{
//							{
//								Type: "ResourceMetricSourceType",
//								Resource: &autoscalingv2beta2.ResourceMetricSource{
//									Name: "cpu",
//									Target: autoscalingv2beta2.MetricTarget{
//										Type:               "Utilization",
//										AverageUtilization: pointer.Int32(10),
//									},
//								},
//							},
//						},
//					},
//				},
//			},
//		}
//		writer.Header().Set("Content-Type", "application/json")
//		_ = enc.Encode(hl)
//	})
//}

var supportedTypes = []string{
	string(types.JSONPatchType),
	string(types.MergePatchType),
	string(types.StrategicMergePatchType),
	string(types.ApplyPatchType),
}

type HPAHandler interface {
	Handler(ctx context.Context, ftc *fedcorev1a1.FederatedTypeConfig) (http.Handler, error)
}

type HPAREST struct {
	restConfig *restclient.Config

	scheme            *runtime.Scheme
	tableConvertor    rest.TableConvertor
	minRequestTimeout time.Duration
}

var _ rest.Getter = &HPAREST{}
var _ rest.Lister = &HPAREST{}
var _ rest.Watcher = &HPAREST{}
var _ rest.Patcher = &HPAREST{}
var _ rest.Updater = &HPAREST{}
var _ HPAHandler = &HPAREST{}

func NewHPAREST(
	adminConfig *restclient.Config,
	scheme *runtime.Scheme,
	minRequestTimeout time.Duration,
) *HPAREST {
	return &HPAREST{
		restConfig:        adminConfig,
		scheme:            scheme,
		tableConvertor:    tableConvertor,
		minRequestTimeout: minRequestTimeout,
	}
}

func (h *HPAREST) Handler(ctx context.Context, ftc *fedcorev1a1.FederatedTypeConfig) (http.Handler, error) {
	requestInfo, found := genericapirequest.RequestInfoFrom(ctx)
	if !found {
		return nil, errors.New("no RequestInfo found in the context")
	}

	scope := h.newScoper(requestInfoResolver, ftc, requestInfo)

	switch requestInfo.Verb {
	case "get":
		return handlers.GetResource(h, scope), nil
	case "list", "watch":
		return handlers.ListResource(h, h, scope, false, h.minRequestTimeout), nil
	case "update":
		return handlers.UpdateResource(h, scope, nil), nil
	case "patch":
		return handlers.PatchResource(h, scope, nil, supportedTypes), nil
	default:
		return nil, apierrors.NewMethodNotSupported(schema.GroupResource{
			Group:    requestInfo.APIGroup,
			Resource: requestInfo.Resource,
		}, requestInfo.Verb)
	}
}

func (h *HPAREST) New() runtime.Object {
	return &unstructured.Unstructured{}
}

func (h *HPAREST) Update(
	ctx context.Context,
	name string,
	objInfo rest.UpdatedObjectInfo,
	createValidation rest.ValidateObjectFunc,
	updateValidation rest.ValidateObjectUpdateFunc,
	forceAllowCreate bool,
	options *metav1.UpdateOptions) (runtime.Object, bool, error) {
	requestInfo, found := genericapirequest.RequestInfoFrom(ctx)
	if !found {
		return nil, false, errors.New("no RequestInfo found in the context")
	}
	gvr := getGVRFromRequestInfo(requestInfo)

	client, err := newUserClientFromContext(ctx, h.restConfig)
	if err != nil {
		return nil, false, err
	}

	if obj, err := client.
		Resource(gvr).
		Namespace(requestInfo.Namespace).
		Get(ctx, name, metav1.GetOptions{}); err != nil {
		return nil, false, err
	} else if obj.GetLabels()[common.CentralizedHPAEnableKey] != common.AnnotationValueTrue {
		return nil, false, apierrors.NewNotFound(gvr.GroupResource(), name)
	}

	// we don't have any transformers, so no need for oldObj
	obj, err := objInfo.UpdatedObject(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	uns, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return nil, false, fmt.Errorf("objInfo.UpdatedObject() returned an object that is not Unstructured: %T", obj)
	}

	opts := metav1.UpdateOptions{}
	if options != nil {
		opts = *options
	}
	if requestInfo.Subresource == "status" {
		uns, err = client.Resource(gvr).Namespace(requestInfo.Namespace).UpdateStatus(ctx, uns, opts)
	} else {
		uns, err = client.Resource(gvr).Namespace(requestInfo.Namespace).Update(ctx, uns, opts)
	}
	return uns, false, err
}

func (h *HPAREST) Watch(ctx context.Context, options *metainternalversion.ListOptions) (watch.Interface, error) {
	requestInfo, found := genericapirequest.RequestInfoFrom(ctx)
	if !found {
		return nil, errors.New("no RequestInfo found in the context")
	}
	gvr := getGVRFromRequestInfo(requestInfo)

	client, err := newUserClientFromContext(ctx, h.restConfig)
	if err != nil {
		return nil, err
	}

	opt, err := h.convertListOptions(options)
	if err != nil {
		return nil, errors.New("failed to convert ListOptions in the context")
	}
	return client.Resource(gvr).Namespace(requestInfo.Namespace).Watch(ctx, opt)
}

func (h *HPAREST) NewList() runtime.Object {
	return &unstructured.UnstructuredList{}
}

func (h *HPAREST) List(ctx context.Context, options *metainternalversion.ListOptions) (runtime.Object, error) {
	requestInfo, found := genericapirequest.RequestInfoFrom(ctx)
	if !found {
		return nil, errors.New("no RequestInfo found in the context")
	}
	gvr := getGVRFromRequestInfo(requestInfo)

	client, err := newUserClientFromContext(ctx, h.restConfig)
	if err != nil {
		return nil, err
	}

	opt, err := h.convertListOptions(options)
	if err != nil {
		return nil, errors.New("failed to convert ListOptions in the context")
	}

	objs, err := client.Resource(gvr).
		Namespace(requestInfo.Namespace).
		List(ctx, opt)
	if err != nil {
		klog.ErrorS(err, "Failed listing hpas",
			"labelSelector", opt.LabelSelector,
			"namespace", klog.KRef("", requestInfo.Namespace),
			"gvr", gvr.String(),
		)
		return nil, fmt.Errorf("failed listing hpas: %w", err)
	}
	return objs, nil
}

func (h *HPAREST) ConvertToTable(ctx context.Context, object runtime.Object, tableOptions runtime.Object) (*metav1.Table, error) {
	requestInfo, found := genericapirequest.RequestInfoFrom(ctx)
	if !found {
		return nil, errors.New("no RequestInfo found in the context")
	}

	convertor := rest.NewDefaultTableConvertor(getGVRFromRequestInfo(requestInfo).GroupResource())
	if requestInfo.APIGroup == autoscaling.GroupName {
		switch t := object.(type) {
		case *unstructured.UnstructuredList:
			hpaList := &autoscaling.HorizontalPodAutoscalerList{}
			hpaList.Items = make([]autoscaling.HorizontalPodAutoscaler, 0, len(t.Items))
			_ = t.EachListItem(func(object runtime.Object) error {
				item, err := h.newHPAforTable(requestInfo.APIVersion, object.(*unstructured.Unstructured))
				if err == nil {
					hpaList.Items = append(hpaList.Items, *item)
				}
				return nil
			})
			object = hpaList
		case *unstructured.Unstructured:
			hpa, err := h.newHPAforTable(requestInfo.APIVersion, t)
			if err == nil {
				object = hpa
			}
		}
		convertor = h.tableConvertor
	}
	return convertor.ConvertToTable(ctx, object, tableOptions)
}

func (h *HPAREST) Get(ctx context.Context, name string, options *metav1.GetOptions) (runtime.Object, error) {
	requestInfo, found := genericapirequest.RequestInfoFrom(ctx)
	if !found {
		return nil, errors.New("no RequestInfo found in the context")
	}
	gvr := getGVRFromRequestInfo(requestInfo)

	client, err := newUserClientFromContext(ctx, h.restConfig)
	if err != nil {
		return nil, err
	}

	opt := metav1.GetOptions{}
	if options != nil {
		opt = *options
	}
	obj, err := client.
		Resource(gvr).
		Namespace(requestInfo.Namespace).
		Get(ctx, name, opt, requestInfo.Subresource)
	if err != nil {
		return nil, err
	}
	if obj.GetLabels()[common.CentralizedHPAEnableKey] != common.AnnotationValueTrue {
		return nil, apierrors.NewNotFound(gvr.GroupResource(), name)
	}
	return obj, nil
}

func (h *HPAREST) newScoper(
	r *genericapirequest.RequestInfoFactory,
	ftc *fedcorev1a1.FederatedTypeConfig,
	info *genericapirequest.RequestInfo,
) *handlers.RequestScope {
	ftc = ftc.DeepCopy()
	ftc.Spec.SourceType.Version = info.APIVersion
	gvk := ftc.GetSourceTypeGVK()
	gvr := ftc.GetSourceTypeGVR()

	// In addition to Unstructured objects (Custom Resources), we also may sometimes need to
	// decode unversioned Options objects, so we delegate to parameterScheme for such types.
	parameterScheme := runtime.NewScheme()
	parameterScheme.AddUnversionedTypes(
		gvk.GroupVersion(),
		&metav1.ListOptions{},
		&metav1.GetOptions{},
		&metav1.DeleteOptions{},
	)
	typer := newUnstructuredObjectTyper(parameterScheme)
	creator := unstructuredCreator{}
	safeConverter, unsafeConverter, _ := NewConverter(ftc)

	// CRDs explicitly do not support protobuf, but some objects returned by the API server do
	negotiatedSerializer := unstructuredNegotiatedSerializer{
		typer:                 typer,
		creator:               creator,
		converter:             safeConverter,
		structuralSchemas:     map[string]*structuralschema.Structural{},
		structuralSchemaGK:    gvk.GroupKind(),
		preserveUnknownFields: true,
	}
	var standardSerializers []runtime.SerializerInfo
	for _, s := range negotiatedSerializer.SupportedMediaTypes() {
		if s.MediaType == runtime.ContentTypeProtobuf {
			continue
		}
		standardSerializers = append(standardSerializers, s)
	}

	return &handlers.RequestScope{
		Namer: &ContextBasedNaming{
			Namer:         runtime.Namer(meta.NewAccessor()),
			ClusterScoped: false,
			resolver:      r,
		},
		Serializer:          negotiatedSerializer,
		StandardSerializers: standardSerializers,

		TableConvertor: h,
		Creater:        creator,
		Convertor:      safeConverter,
		Defaulter: unstructuredDefaulter{
			delegate:           parameterScheme,
			structuralSchemas:  map[string]*structuralschema.Structural{},
			structuralSchemaGK: gvk.GroupKind(),
		},
		Typer:           typer,
		UnsafeConvertor: unsafeConverter,

		MetaGroupVersion: metav1.SchemeGroupVersion,
		Resource:         gvr,
		Kind:             gvk,
	}
}

func getGVRFromRequestInfo(request *genericapirequest.RequestInfo) schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    request.APIGroup,
		Version:  request.APIVersion,
		Resource: request.Resource,
	}
}

func (h *HPAREST) convertListOptions(options *metainternalversion.ListOptions) (metav1.ListOptions, error) {
	opt := metav1.ListOptions{}
	if options != nil {
		if err := h.scheme.Convert(options, &opt, nil); err != nil {
			return opt, errors.New("failed to convert ListOptions in the context")
		}
	}

	selector, err := newLabelSelector(opt.LabelSelector)
	if err != nil {
		return opt, err
	}

	opt.LabelSelector = selector
	return opt, nil
}

func newLabelSelector(old string) (string, error) {
	selector, err := labels.Parse(old)
	if err != nil {
		return old, errors.New("failed to parse label selector")
	}
	innerRequirement, _ := labels.NewRequirement(
		common.CentralizedHPAEnableKey,
		selection.Equals,
		[]string{common.AnnotationValueTrue},
	)
	return selector.Add(*innerRequirement).String(), nil
}

func (h *HPAREST) newHPAforTable(
	version string,
	uns *unstructured.Unstructured,
) (*autoscaling.HorizontalPodAutoscaler, error) {
	hpa := &autoscaling.HorizontalPodAutoscaler{}
	switch version {
	case "v1":
		hpaV1 := &autoscalingv1.HorizontalPodAutoscaler{}
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(uns.UnstructuredContent(), hpaV1)
		if err != nil {
			return nil, err
		}
		if err := h.scheme.Convert(hpaV1, hpa, nil); err != nil {
			return nil, err
		}
	case "v2", "v2beta2", "v2beta1":
		hpaV2 := &autoscalingv2.HorizontalPodAutoscaler{}
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(uns.UnstructuredContent(), hpaV2)
		if err != nil {
			return nil, err
		}
		if err := h.scheme.Convert(hpaV2, hpa, nil); err != nil {
			return nil, err
		}
	}
	return hpa, nil
}
