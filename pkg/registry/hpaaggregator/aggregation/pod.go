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

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/utils/pointer"
)

func NewPodHandler(podPath []string, r rest.Responder) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		fmt.Println("#### hpaa podPath:", podPath)
		enc := json.NewEncoder(writer)
		// TODO: implement pod
		if len(podPath) > 3 {
			writer.WriteHeader(http.StatusInternalServerError)
			_ = enc.Encode(apierrors.NewInternalError(
				fmt.Errorf("the path %s is not supported now", path.Join(podPath...))),
			)
			return
		}
		pl := &corev1.PodList{
			TypeMeta: metav1.TypeMeta{
				Kind:       "PodList",
				APIVersion: "v1",
			},
			ListMeta: metav1.ListMeta{
				ResourceVersion:    time.Now().Format(time.RFC3339),
				Continue:           "",
				RemainingItemCount: nil,
			},
			Items: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test1",
						Namespace:         "default",
						ResourceVersion:   "123",
						CreationTimestamp: metav1.Now(),
						UID:               "fa8255dc-adb1-4d25-b5b7-b41860e11bb2",
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:                     "c1",
								Image:                    "nginx:alpine",
								Resources:                corev1.ResourceRequirements{},
								TerminationMessagePath:   "/dev/termination-log",
								TerminationMessagePolicy: "File",
								ImagePullPolicy:          "Always",
							},
						},
						RestartPolicy:                 "Always",
						TerminationGracePeriodSeconds: pointer.Int64(30),
						DNSPolicy:                     "ClusterFirst",
					},
					Status: corev1.PodStatus{
						Phase:    "Pending",
						QOSClass: "BestEffort",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test2",
						Namespace:         "default",
						ResourceVersion:   "321",
						CreationTimestamp: metav1.Now(),
						UID:               "fa8255dc-adb1-4d25-b5b7-b41860e11bb3",
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:                     "c1",
								Image:                    "nginx:alpine",
								Resources:                corev1.ResourceRequirements{},
								TerminationMessagePath:   "/dev/termination-log",
								TerminationMessagePolicy: "File",
								ImagePullPolicy:          "Always",
							},
						},
						RestartPolicy:                 "Always",
						TerminationGracePeriodSeconds: pointer.Int64(30),
						DNSPolicy:                     "ClusterFirst",
					},
					Status: corev1.PodStatus{
						Phase:    "Pending",
						QOSClass: "BestEffort",
					},
				},
			},
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = enc.Encode(pl)
	})
}
