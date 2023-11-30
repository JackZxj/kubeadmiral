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
	"errors"
	"net/http"
	"net/url"

	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/apimachinery/pkg/util/proxy"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/endpoints/handlers/responsewriters"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
	restclient "k8s.io/client-go/rest"
)

func NewForwardHandler(
	location url.URL,
	info *request.RequestInfo,
	r rest.Responder,
	adminConfig *restclient.Config,
	isHPA bool,
) (http.Handler, error) {
	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		requester, exist := request.UserFrom(req.Context())
		if !exist {
			responsewriters.InternalError(rw, req, errors.New("no user found for request"))
			return
		}
		req.Header.Set(authenticationv1.ImpersonateUserHeader, requester.GetName())
		for _, group := range requester.GetGroups() {
			if group != user.AllAuthenticated && group != user.AllUnauthenticated {
				req.Header.Add(authenticationv1.ImpersonateGroupHeader, group)
			}
		}

		// we use admin config and ImpersonateUser to forward
		transport, err := restclient.TransportFor(adminConfig)
		if err != nil {
			responsewriters.InternalError(rw, req, errors.New("failed to new transport"))
			return
		}

		if isHPA {
			switch info.Verb {
			case "get", "list", "watch":
				q := req.URL.Query()
				q.Add("labelSelector", "kubeadmiral.io/centralized-hpa-enabled=true")
				req.URL.RawQuery = q.Encode()
			}
		}
		location.Path = info.Path
		location.RawQuery = req.URL.Query().Encode()

		handler := proxy.NewUpgradeAwareHandler(
			&location,
			transport,
			true,
			httpstream.IsUpgradeRequest(req),
			proxy.NewErrorResponder(r),
		)
		// TODO: make it configurable
		handler.MaxBytesPerSec = 0

		handler.ServeHTTP(rw, req)
	}), nil
}
