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

package externalmetricadaptor

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/storage"
	"k8s.io/apiserver/pkg/storage/names"

	"github.com/kubewharf/kubeadmiral/pkg/apis/hpaaggregator/v1alpha1"
	"github.com/kubewharf/kubeadmiral/pkg/apis/hpaaggregator/validation"
)

// NewStrategy creates and returns a ExternalMetricAdaptorStrategy instance
func NewStrategy(typer runtime.ObjectTyper) ExternalMetricAdaptorStrategy {
	return ExternalMetricAdaptorStrategy{typer, names.SimpleNameGenerator}
}

// GetAttrs returns labels.Set, fields.Set, and error in case the given runtime.Object is not a ExternalMetricAdaptor
func GetAttrs(obj runtime.Object) (labels.Set, fields.Set, error) {
	apiserver, ok := obj.(*v1alpha1.ExternalMetricAdaptor)
	if !ok {
		return nil, nil, fmt.Errorf("given object is not a ExternalMetricAdaptor")
	}
	return apiserver.ObjectMeta.Labels, SelectableFields(apiserver), nil
}

// MatchExternalMetricAdaptor is the filter used by the generic etcd backend to watch events
// from etcd to clients of the apiserver only interested in specific labels/fields.
func MatchExternalMetricAdaptor(label labels.Selector, field fields.Selector) storage.SelectionPredicate {
	return storage.SelectionPredicate{
		Label:    label,
		Field:    field,
		GetAttrs: GetAttrs,
	}
}

// SelectableFields returns a field set that represents the object.
func SelectableFields(obj *v1alpha1.ExternalMetricAdaptor) fields.Set {
	return generic.ObjectMetaFieldsSet(&obj.ObjectMeta, true)
}

type ExternalMetricAdaptorStrategy struct {
	runtime.ObjectTyper
	names.NameGenerator
}

func (ExternalMetricAdaptorStrategy) NamespaceScoped() bool {
	return true
}

func (ExternalMetricAdaptorStrategy) PrepareForCreate(context.Context, runtime.Object) {
}

func (ExternalMetricAdaptorStrategy) PrepareForUpdate(context.Context, runtime.Object, runtime.Object) {
}

func (ExternalMetricAdaptorStrategy) Validate(ctx context.Context, obj runtime.Object) field.ErrorList {
	e := obj.(*v1alpha1.ExternalMetricAdaptor)
	return validation.ValidateExternalMetricAdaptor(e)
}

// WarningsOnCreate returns warnings for the creation of the given object.
func (ExternalMetricAdaptorStrategy) WarningsOnCreate(context.Context, runtime.Object) []string {
	return nil
}

func (ExternalMetricAdaptorStrategy) AllowCreateOnUpdate() bool {
	return false
}

func (ExternalMetricAdaptorStrategy) AllowUnconditionalUpdate() bool {
	return false
}

func (ExternalMetricAdaptorStrategy) Canonicalize(runtime.Object) {
}

func (ExternalMetricAdaptorStrategy) ValidateUpdate(context.Context, runtime.Object, runtime.Object) field.ErrorList {
	return field.ErrorList{}
}

// WarningsOnUpdate returns warnings for the given update.
func (ExternalMetricAdaptorStrategy) WarningsOnUpdate(context.Context, runtime.Object, runtime.Object) []string {
	return nil
}
