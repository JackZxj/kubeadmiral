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
	"crypto/tls"
	"errors"
	"net/http"
	"net/url"

	authenticationv1 "k8s.io/api/authentication/v1"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apimachinery/pkg/util/proxy"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/endpoints/handlers/responsewriters"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
	restclient "k8s.io/client-go/rest"

	"github.com/kubewharf/kubeadmiral/pkg/hpaaggregatorapiserver/serverconfig"
)

var transport = defaultProxyTransport()

func NewForwardHandler(
	location url.URL,
	info *request.RequestInfo,
	r rest.Responder,
	adminConfig *restclient.Config,
	isHPA bool,
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
		var transport http.RoundTripper = transport
		if !serverconfig.RecoverAuthentication(req) {
			// If the request without an auth token (eg: tls auth),
			// we use admin config and ImpersonateUser to forward
			transport, err = restclient.TransportFor(adminConfig)
			if err != nil {
				responsewriters.InternalError(rw, req, errors.New("failed to new transport"))
				return
			}
		}

		location.Path = info.Path
		if isHPA {
			switch info.Verb {
			case "get", "list", "watch":
				q := req.URL.Query()
				q.Add("labelSelector", "kubeadmiral.io/centralized-hpa-enabled=true")
				req.URL.RawQuery = q.Encode()
			}
		}

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

// defaultProxyTransport creates the dialer infrastructure to connect to the clusters.
func defaultProxyTransport() *http.Transport {
	var proxyDialerFn utilnet.DialFunc
	// Proxying to pods and services is IP-based... don't expect to be able to verify the hostname
	proxyTLSClientConfig := &tls.Config{InsecureSkipVerify: true}
	proxyTransport := utilnet.SetTransportDefaults(&http.Transport{
		DialContext:     proxyDialerFn,
		TLSClientConfig: proxyTLSClientConfig,
	})
	return proxyTransport
}
