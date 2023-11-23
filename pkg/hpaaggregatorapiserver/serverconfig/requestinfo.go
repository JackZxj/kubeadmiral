package serverconfig

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/util/sets"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
	genericapiserver "k8s.io/apiserver/pkg/server"
)

type RequestInfoResolver struct {
	lock sync.Mutex

	customPrefixes  sets.Set[string]
	defaultResolver *apirequest.RequestInfoFactory
}

var _ apirequest.RequestInfoResolver = &RequestInfoResolver{}

func NewRequestInfoResolver(c *genericapiserver.Config, customPrefixes ...string) *RequestInfoResolver {
	return &RequestInfoResolver{
		customPrefixes:  sets.New(customPrefixes...),
		defaultResolver: genericapiserver.NewRequestInfoResolver(c),
	}
}

func (r *RequestInfoResolver) NewRequestInfo(req *http.Request) (*apirequest.RequestInfo, error) {
	reqCopy := req
	if prefix, matched := r.matchCustomPrefixs(reqCopy.URL.Path); matched {
		reqCopy = req.Clone(context.Background())
		reqCopy.URL.Path = strings.TrimPrefix(reqCopy.URL.Path, prefix)
	}
	return r.defaultResolver.NewRequestInfo(reqCopy)
}

func (r *RequestInfoResolver) InsertCustomPrefixes(customPrefixes ...string) {
	r.lock.Lock()
	defer r.lock.Unlock()

	r.customPrefixes.Insert(customPrefixes...)
}

func (r *RequestInfoResolver) matchCustomPrefixs(urlPath string) (string, bool) {
	r.lock.Lock()
	defer r.lock.Unlock()

	for key := range r.customPrefixes {
		if strings.HasPrefix(urlPath, key) {
			return key, true
		}
	}
	return "", false
}
