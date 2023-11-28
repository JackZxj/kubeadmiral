package forward

import (
	"context"
	"errors"
	"net/http"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/endpoints/handlers/responsewriters"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/client-go/discovery"
	restclient "k8s.io/client-go/rest"
)

type ExternalDiscovery struct {
	apiGroups []metav1.APIGroup
	config    *restclient.Config
}

func (e *ExternalDiscovery) Handler(ctx context.Context) (http.Handler, error) {
	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		requestInfo, found := genericapirequest.RequestInfoFrom(ctx)
		if !found {
			responsewriters.InternalError(rw, req, errors.New("no RequestInfo found in the context"))
			return
		}

		copyConfig, err := newCopyConfigForUser(ctx, e.config)
		if err != nil {
			responsewriters.InternalError(rw, req, errors.New("failed to get rest config"))
			return
		}
		client, err := discovery.NewDiscoveryClientForConfig(copyConfig)
		if err != nil {
			responsewriters.InternalError(rw, req, errors.New("failed to get client"))
			return
		}

		rw.Header().Set("Content-Type", "application/json")
		if len(requestInfo.Parts) == 1 {
			apis, err := client.ServerGroups()
			if err != nil {
				responsewriters.InternalError(rw, req, err)
				return
			}
			apis.Groups = append(apis.Groups, e.apiGroups...)
		} else {
			var api *metav1.APIGroup
			for i, g := range e.apiGroups {
				if g.Name == requestInfo.Parts[1] {
					api = &e.apiGroups[i]
					break
				}
			}
			if api != nil {

			}
			_, err := client.ServerGroups()
			if err != nil {
				responsewriters.InternalError(rw, req, err)
				return
			}
			return
		}

	}), nil
}
