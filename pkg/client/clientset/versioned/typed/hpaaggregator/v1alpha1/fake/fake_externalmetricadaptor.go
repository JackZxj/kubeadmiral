// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	"context"

	v1alpha1 "github.com/kubewharf/kubeadmiral/pkg/apis/hpaaggregator/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeExternalMetricAdaptors implements ExternalMetricAdaptorInterface
type FakeExternalMetricAdaptors struct {
	Fake *FakeHpaaggregatorV1alpha1
	ns   string
}

var externalmetricadaptorsResource = schema.GroupVersionResource{Group: "hpaaggregator.kubeadmiral.io", Version: "v1alpha1", Resource: "externalmetricadaptors"}

var externalmetricadaptorsKind = schema.GroupVersionKind{Group: "hpaaggregator.kubeadmiral.io", Version: "v1alpha1", Kind: "ExternalMetricAdaptor"}

// Get takes name of the externalMetricAdaptor, and returns the corresponding externalMetricAdaptor object, and an error if there is any.
func (c *FakeExternalMetricAdaptors) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1alpha1.ExternalMetricAdaptor, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(externalmetricadaptorsResource, c.ns, name), &v1alpha1.ExternalMetricAdaptor{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ExternalMetricAdaptor), err
}

// List takes label and field selectors, and returns the list of ExternalMetricAdaptors that match those selectors.
func (c *FakeExternalMetricAdaptors) List(ctx context.Context, opts v1.ListOptions) (result *v1alpha1.ExternalMetricAdaptorList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(externalmetricadaptorsResource, externalmetricadaptorsKind, c.ns, opts), &v1alpha1.ExternalMetricAdaptorList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1alpha1.ExternalMetricAdaptorList{ListMeta: obj.(*v1alpha1.ExternalMetricAdaptorList).ListMeta}
	for _, item := range obj.(*v1alpha1.ExternalMetricAdaptorList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested externalMetricAdaptors.
func (c *FakeExternalMetricAdaptors) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(externalmetricadaptorsResource, c.ns, opts))

}

// Create takes the representation of a externalMetricAdaptor and creates it.  Returns the server's representation of the externalMetricAdaptor, and an error, if there is any.
func (c *FakeExternalMetricAdaptors) Create(ctx context.Context, externalMetricAdaptor *v1alpha1.ExternalMetricAdaptor, opts v1.CreateOptions) (result *v1alpha1.ExternalMetricAdaptor, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(externalmetricadaptorsResource, c.ns, externalMetricAdaptor), &v1alpha1.ExternalMetricAdaptor{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ExternalMetricAdaptor), err
}

// Update takes the representation of a externalMetricAdaptor and updates it. Returns the server's representation of the externalMetricAdaptor, and an error, if there is any.
func (c *FakeExternalMetricAdaptors) Update(ctx context.Context, externalMetricAdaptor *v1alpha1.ExternalMetricAdaptor, opts v1.UpdateOptions) (result *v1alpha1.ExternalMetricAdaptor, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(externalmetricadaptorsResource, c.ns, externalMetricAdaptor), &v1alpha1.ExternalMetricAdaptor{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ExternalMetricAdaptor), err
}

// Delete takes name of the externalMetricAdaptor and deletes it. Returns an error if one occurs.
func (c *FakeExternalMetricAdaptors) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteActionWithOptions(externalmetricadaptorsResource, c.ns, name, opts), &v1alpha1.ExternalMetricAdaptor{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeExternalMetricAdaptors) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(externalmetricadaptorsResource, c.ns, listOpts)

	_, err := c.Fake.Invokes(action, &v1alpha1.ExternalMetricAdaptorList{})
	return err
}

// Patch applies the patch and returns the patched externalMetricAdaptor.
func (c *FakeExternalMetricAdaptors) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.ExternalMetricAdaptor, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(externalmetricadaptorsResource, c.ns, name, pt, data, subresources...), &v1alpha1.ExternalMetricAdaptor{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ExternalMetricAdaptor), err
}