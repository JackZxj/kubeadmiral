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
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"time"

	autoscalingv2beta2 "k8s.io/api/autoscaling/v2beta2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/utils/pointer"
)

func NewHPAHandler(hpaPath []string, r rest.Responder) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		enc := json.NewEncoder(writer)
		// TODO: implement hpa
		if len(hpaPath) > 3 {
			writer.WriteHeader(http.StatusInternalServerError)
			_ = enc.Encode(apierrors.NewInternalError(
				fmt.Errorf("the path %s is not supported now", path.Join(hpaPath...))),
			)
			return
		}
		hl := &autoscalingv2beta2.HorizontalPodAutoscalerList{
			TypeMeta: metav1.TypeMeta{
				Kind:       "HorizontalPodAutoscalerList",
				APIVersion: "v1",
			},
			ListMeta: metav1.ListMeta{
				ResourceVersion:    time.Now().Format(time.RFC3339),
				Continue:           "",
				RemainingItemCount: nil,
			},
			Items: []autoscalingv2beta2.HorizontalPodAutoscaler{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test1",
						Namespace:         "default",
						ResourceVersion:   "123",
						CreationTimestamp: metav1.Now(),
						UID:               "fa8255dc-adb1-4d25-b5b7-b41860e11hpa",
					},
					Spec: autoscalingv2beta2.HorizontalPodAutoscalerSpec{
						ScaleTargetRef: autoscalingv2beta2.CrossVersionObjectReference{
							Kind:       "Deployment",
							Name:       "test",
							APIVersion: "apps/v1",
						},
						MinReplicas: pointer.Int32(1),
						MaxReplicas: 10,
						Metrics: []autoscalingv2beta2.MetricSpec{
							{
								Type: "ResourceMetricSourceType",
								Resource: &autoscalingv2beta2.ResourceMetricSource{
									Name: "cpu",
									Target: autoscalingv2beta2.MetricTarget{
										Type:               "Utilization",
										AverageUtilization: pointer.Int32(10),
									},
								},
							},
						},
					},
				},
			},
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = enc.Encode(hl)
	})
}
