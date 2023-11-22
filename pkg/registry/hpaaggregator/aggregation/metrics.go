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

package aggregation

//const (
//	ResourceGroupName = "metrics.k8s.io"
//	CustomGroupName   = "custom.metrics.k8s.io"
//	ExternalGroupName = "external.metrics.k8s.io"
//)
//
//var (
//	ResourceSchemeGroupVersion = schema.GroupVersion{Group: ResourceGroupName, Version: "v1beta1"}
//	CustomSchemeGroupVersion   = schema.GroupVersion{Group: CustomGroupName, Version: "v1beta2"}
//	ExternalSchemeGroupVersion = schema.GroupVersion{Group: ExternalGroupName, Version: "v1beta1"}
//
//	groupVersions = map[string]schema.GroupVersion{
//		ResourceGroupName: ResourceSchemeGroupVersion,
//		CustomGroupName:   CustomSchemeGroupVersion,
//		ExternalGroupName: ExternalSchemeGroupVersion,
//	}
//)
//
//func isMetrics(requestParts []string) bool {
//	return len(requestParts) >= 4 &&
//		(requestParts[1] == ResourceGroupName ||
//			requestParts[1] == CustomGroupName ||
//			requestParts[1] == ExternalGroupName)
//}
//
//func NewMetricsHandler(metricsPath []string, r rest.Responder) http.Handler {
//	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
//		enc := json.NewEncoder(writer)
//		// TODO: implement metrics
//		switch metricsPath[1] {
//		case ResourceGroupName:
//			switch len(metricsPath) {
//			case 2:
//				api := newAPIGroup(ResourceGroupName)
//				writer.Header().Set("Content-Type", "application/json")
//				_ = enc.Encode(api)
//			case 3:
//
//			}
//
//			// /apis/metrics.k8s.io/v1beta1/nodes
//			// /apis/metrics.k8s.io/v1beta1/pods
//			// /apis/metrics.k8s.io/v1beta1/namespaces/default/pods
//
//		case CustomGroupName:
//		case ExternalGroupName:
//		default:
//			writer.WriteHeader(http.StatusInternalServerError)
//			_ = enc.Encode(apierrors.NewInternalError(
//				fmt.Errorf("the path %s is not supported now", path.Join(metricsPath...))),
//			)
//			return
//		}
//	})
//}
//
//func newAPIGroup(group string) *metav1.APIGroup {
//	return &metav1.APIGroup{
//		Name: group,
//		Versions: []metav1.GroupVersionForDiscovery{
//			{
//				GroupVersion: groupVersions[group].String(),
//				Version:      groupVersions[group].Version,
//			},
//		},
//		PreferredVersion: metav1.GroupVersionForDiscovery{
//			GroupVersion: groupVersions[group].String(),
//			Version:      groupVersions[group].Version,
//		},
//	}
//}

