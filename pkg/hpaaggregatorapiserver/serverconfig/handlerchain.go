package serverconfig

import (
	"encoding/json"
	"net/http"
	"strings"

	authenticationv1 "k8s.io/api/authentication/v1"
	genericapiserver "k8s.io/apiserver/pkg/server"
)

const (
	Authorization = "KubeAdmiral-HPA-Aggregator-Authorization"

	ImpersonateUserHeader      = "KubeAdmiral-HPA-Aggregator-Impersonate-User"
	ImpersonateGroupHeader     = "KubeAdmiral-HPA-Aggregator-Impersonate-Group"
	ImpersonateUIDHeader       = "KubeAdmiral-HPA-Aggregator-Impersonate-Uid"
	ImpersonateUserExtraHeader = "KubeAdmiral-HPA-Aggregator-Impersonate-Extra"
)

func CopyAuthHandlerChain(apiHandler http.Handler, c *genericapiserver.Config) http.Handler {
	handler := genericapiserver.DefaultBuildHandlerChain(apiHandler, c)
	handler = WithImpersonationCopy(handler)
	handler = WithAuthenticationCopy(handler)
	return handler
}

func WithAuthenticationCopy(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		auth := req.Header.Get("Authorization")
		if auth != "" {
			req.Header.Add(Authorization, auth)
		}
		handler.ServeHTTP(w, req)
	})
}

func WithImpersonationCopy(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if user := req.Header.Get(authenticationv1.ImpersonateUserHeader); user != "" {
			req.Header.Add(ImpersonateUserHeader, user)
		}

		if group := req.Header.Get(authenticationv1.ImpersonateGroupHeader); group != "" {
			req.Header.Add(ImpersonateGroupHeader, group)
		}

		if uid := req.Header.Get(authenticationv1.ImpersonateUIDHeader); uid != "" {
			req.Header.Add(ImpersonateUIDHeader, uid)
		}

		extra := map[string]string{}
		for headerName := range req.Header {
			if strings.HasPrefix(headerName, authenticationv1.ImpersonateUserExtraHeaderPrefix) {
				extra[headerName] = req.Header.Get(headerName)
			}
		}

		if len(extra) > 0 {
			if result, err := json.Marshal(extra); err == nil {
				req.Header.Add(ImpersonateUserExtraHeader, string(result))
			}
		}
		handler.ServeHTTP(w, req)
	})
}

func RecoverAuthentication(req *http.Request) bool {
	if key := req.Header.Get(Authorization); key != "" {
		req.Header.Add("Authorization", key)
		req.Header.Del(Authorization)
		return true
	}
	return false
}

func RecoverImpersonation(req *http.Request) bool {
	recovered := false
	if key := req.Header.Get(ImpersonateUserHeader); key != "" {
		req.Header.Add(authenticationv1.ImpersonateUserHeader, key)
		req.Header.Del(ImpersonateUserHeader)
		recovered = true
	}
	if key := req.Header.Get(ImpersonateGroupHeader); key != "" {
		req.Header.Add(authenticationv1.ImpersonateGroupHeader, key)
		req.Header.Del(ImpersonateGroupHeader)
		recovered = true
	}
	if key := req.Header.Get(ImpersonateUIDHeader); key != "" {
		req.Header.Add(authenticationv1.ImpersonateUIDHeader, key)
		req.Header.Del(ImpersonateUIDHeader)
		recovered = true
	}
	if key := req.Header.Get(ImpersonateUserExtraHeader); key != "" {
		extra := map[string]string{}
		if json.Unmarshal([]byte(key), &extra) == nil {
			for k, v := range extra {
				req.Header.Add(k, v)
			}
			recovered = true
		}
		req.Header.Del(ImpersonateUserExtraHeader)
	}
	return recovered
}
