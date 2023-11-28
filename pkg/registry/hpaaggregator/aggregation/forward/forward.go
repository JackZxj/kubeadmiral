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
	"k8s.io/apimachinery/pkg/util/proxy"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/endpoints/handlers/responsewriters"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
	restclient "k8s.io/client-go/rest"

	"github.com/kubewharf/kubeadmiral/pkg/hpaaggregatorapiserver/serverconfig"
)

func NewForwardHandler(
	location url.URL,
	forwardPath string,
	transport http.RoundTripper,
	r rest.Responder,
	adminConfig *restclient.Config,
) (http.Handler, error) {
	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if !serverconfig.RecoverImpersonation(req) {
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
		}
		var err error
		if !serverconfig.RecoverAuthentication(req) {
			// If the request without an auth token (eg: tls auth),
			// we use admin config and ImpersonateUser to forward
			transport, err = restclient.TransportFor(adminConfig)
			if err != nil {
				responsewriters.InternalError(rw, req, errors.New("failed to new transport"))
				return
			}
		}

		location.Path = forwardPath
		location.RawQuery = req.URL.RawQuery

		handler := proxy.NewUpgradeAwareHandler(
			&location,
			transport,
			true,
			false,
			proxy.NewErrorResponder(r),
		)
		// TODO: make it configurable
		handler.MaxBytesPerSec = 0

		handler.ServeHTTP(rw, req)
	}), nil
}
