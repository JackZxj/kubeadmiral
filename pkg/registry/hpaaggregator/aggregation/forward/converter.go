package forward

import (
	"fmt"

	autoscalingv1 "k8s.io/api/autoscaling/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	typedscheme "k8s.io/client-go/kubernetes/scheme"

	fedcorev1a1 "github.com/kubewharf/kubeadmiral/pkg/apis/core/v1alpha1"
)

// NewConverter returns a new CR converter based on the conversion settings in crd object.
func NewConverter(ftc *fedcorev1a1.FederatedTypeConfig) (safe, unsafe runtime.ObjectConvertor, err error) {
	validVersions := map[schema.GroupVersion]bool{
		ftc.GetSourceTypeGVK().GroupVersion(): true,
	}

	unsafe = &crConverter{
		convertScale:  false,
		validVersions: validVersions,
		clusterScoped: !ftc.GetNamespaced(),
		converter:     &nopConverter{},
	}
	return &safeConverterWrapper{unsafe}, unsafe, nil
}

// crConverterInterface is the interface all cr converters must implement
type crConverterInterface interface {
	// Convert converts in object to the given gvk and returns the converted object.
	// Note that the function may mutate in object and return it. A safe wrapper will make sure
	// a safe converter will be returned.
	Convert(in runtime.Object, targetGVK schema.GroupVersion) (runtime.Object, error)
}

// crConverter extends the delegate converter with generic CR conversion behaviour. The delegate will implement the
// user defined conversion strategy given in the CustomResourceDefinition.
type crConverter struct {
	convertScale  bool
	converter     crConverterInterface
	validVersions map[schema.GroupVersion]bool
	clusterScoped bool
}

func (c *crConverter) ConvertFieldLabel(gvk schema.GroupVersionKind, label, value string) (string, string, error) {
	// We currently only support metadata.namespace and metadata.name.
	switch {
	case label == "metadata.name":
		return label, value, nil
	case !c.clusterScoped && label == "metadata.namespace":
		return label, value, nil
	default:
		return "", "", fmt.Errorf("field label not supported: %s", label)
	}
}

func (c *crConverter) Convert(in, out, context interface{}) error {
	// Special-case typed scale conversion if this custom resource supports a scale endpoint
	if c.convertScale {
		_, isInScale := in.(*autoscalingv1.Scale)
		_, isOutScale := out.(*autoscalingv1.Scale)
		if isInScale || isOutScale {
			return typedscheme.Scheme.Convert(in, out, context)
		}
	}

	unstructIn, ok := in.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("input type %T in not valid for unstructured conversion to %T", in, out)
	}

	unstructOut, ok := out.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("output type %T in not valid for unstructured conversion from %T", out, in)
	}

	outGVK := unstructOut.GroupVersionKind()
	converted, err := c.ConvertToVersion(unstructIn, outGVK.GroupVersion())
	if err != nil {
		return err
	}
	unstructuredConverted, ok := converted.(runtime.Unstructured)
	if !ok {
		// this should not happened
		return fmt.Errorf("CR conversion failed")
	}
	unstructOut.SetUnstructuredContent(unstructuredConverted.UnstructuredContent())
	return nil
}

// ConvertToVersion converts in object to the given gvk in place and returns the same `in` object.
// The in object can be a single object or a UnstructuredList. CRD storage implementation creates an
// UnstructuredList with the request's GV, populates it from storage, then calls conversion to convert
// the individual items. This function assumes it never gets a v1.List.
func (c *crConverter) ConvertToVersion(in runtime.Object, target runtime.GroupVersioner) (runtime.Object, error) {
	fromGVK := in.GetObjectKind().GroupVersionKind()
	toGVK, ok := target.KindForGroupVersionKinds([]schema.GroupVersionKind{fromGVK})
	if !ok {
		// TODO: should this be a typed error?
		return nil, fmt.Errorf("%v is unstructured and is not suitable for converting to %q", fromGVK.String(), target)
	}
	// Special-case typed scale conversion if this custom resource supports a scale endpoint
	if c.convertScale {
		if _, isInScale := in.(*autoscalingv1.Scale); isInScale {
			return typedscheme.Scheme.ConvertToVersion(in, target)
		}
	}

	if !c.validVersions[toGVK.GroupVersion()] {
		return nil, fmt.Errorf("request to convert CR to an invalid group/version: %s", toGVK.GroupVersion().String())
	}
	// Note that even if the request is for a list, the GV of the request UnstructuredList is what
	// is expected to convert to. As mentioned in the function's document, it is not expected to
	// get a v1.List.
	if !c.validVersions[fromGVK.GroupVersion()] {
		return nil, fmt.Errorf("request to convert CR from an invalid group/version: %s", fromGVK.GroupVersion().String())
	}
	// Check list item's apiVersion
	if list, ok := in.(*unstructured.UnstructuredList); ok {
		for i := range list.Items {
			expectedGV := list.Items[i].GroupVersionKind().GroupVersion()
			if !c.validVersions[expectedGV] {
				return nil, fmt.Errorf("request to convert CR list failed, list index %d has invalid group/version: %s", i, expectedGV.String())
			}
		}
	}
	return c.converter.Convert(in, toGVK.GroupVersion())
}

// safeConverterWrapper is a wrapper over an unsafe object converter that makes copy of the input and then delegate to the unsafe converter.
type safeConverterWrapper struct {
	unsafe runtime.ObjectConvertor
}

var _ runtime.ObjectConvertor = &safeConverterWrapper{}

// ConvertFieldLabel delegate the call to the unsafe converter.
func (c *safeConverterWrapper) ConvertFieldLabel(gvk schema.GroupVersionKind, label, value string) (string, string, error) {
	return c.unsafe.ConvertFieldLabel(gvk, label, value)
}

// Convert makes a copy of in object and then delegate the call to the unsafe converter.
func (c *safeConverterWrapper) Convert(in, out, context interface{}) error {
	inObject, ok := in.(runtime.Object)
	if !ok {
		return fmt.Errorf("input type %T in not valid for object conversion", in)
	}
	return c.unsafe.Convert(inObject.DeepCopyObject(), out, context)
}

// ConvertToVersion makes a copy of in object and then delegate the call to the unsafe converter.
func (c *safeConverterWrapper) ConvertToVersion(in runtime.Object, target runtime.GroupVersioner) (runtime.Object, error) {
	return c.unsafe.ConvertToVersion(in.DeepCopyObject(), target)
}

// nopConverter is a converter that only sets the apiVersion fields, but does not real conversion.
type nopConverter struct {
}

var _ crConverterInterface = &nopConverter{}

// ConvertToVersion converts in object to the given gv in place and returns the same `in` object.
func (c *nopConverter) Convert(in runtime.Object, targetGV schema.GroupVersion) (runtime.Object, error) {
	// Run the converter on the list items instead of list itself
	if list, ok := in.(*unstructured.UnstructuredList); ok {
		for i := range list.Items {
			list.Items[i].SetGroupVersionKind(targetGV.WithKind(list.Items[i].GroupVersionKind().Kind))
		}
	}
	in.GetObjectKind().SetGroupVersionKind(targetGV.WithKind(in.GetObjectKind().GroupVersionKind().Kind))
	return in, nil
}
