/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

This file may have been modified by The KubeAdmiral Authors
("KubeAdmiral Modifications"). All KubeAdmiral Modifications
are Copyright 2023 The KubeAdmiral Authors.
*/

package sync

import (
	"encoding/json"

	jsonpatch "github.com/evanphx/json-patch"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"

	fedcorev1a1 "github.com/kubewharf/kubeadmiral/pkg/apis/core/v1alpha1"
)

// Namespace and name may not be overridden since these fields are the primary
// mechanism of association between a federated resource in the host cluster and
// the target resources in the member clusters.
//
// Kind should always be sourced from source object and should not vary across
// member clusters.
//
// apiVersion can be overridden to support managing resources like Ingress which
// can exist in different groups at different versions. Users will need to take
// care not to abuse this capability.
var invalidOverridePaths = sets.New(
	"/metadata/namespace",
	"/metadata/name",
	"/metadata/generateName",
	"/kind",
)

// ApplyJSONPatch applies the override on to the given unstructured object.
func ApplyJSONPatch(obj *unstructured.Unstructured, overrides fedcorev1a1.OverridePatches) error {
	// TODO: Do the defaulting of "op" field to "replace" in API defaulting
	for i, overrideItem := range overrides {
		if overrideItem.Op == "" {
			overrides[i].Op = "replace"
		}
	}
	jsonPatchBytes, err := json.Marshal(overrides)
	if err != nil {
		return err
	}

	patch, err := jsonpatch.DecodePatch(jsonPatchBytes)
	if err != nil {
		return err
	}

	objectJSONBytes, err := obj.MarshalJSON()
	if err != nil {
		return err
	}

	patchedObjectJSONBytes, err := patch.Apply(objectJSONBytes)
	if err != nil {
		return err
	}

	err = obj.UnmarshalJSON(patchedObjectJSONBytes)
	return err
}
