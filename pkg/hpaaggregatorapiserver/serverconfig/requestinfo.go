package serverconfig

import (
	"context"
	"net/http"
	"strings"
	"sync"

	apirequest "k8s.io/apiserver/pkg/endpoints/request"
	genericapiserver "k8s.io/apiserver/pkg/server"
)

type RequestInfoResolver struct {
	lock sync.Mutex

	customPrefixes  map[string]*CustomResolver
	defaultResolver *apirequest.RequestInfoFactory
}

type CustomResolver struct {
	Prefix   string
	Resolver func(rawPath, prefix string) string
}

var _ apirequest.RequestInfoResolver = &RequestInfoResolver{}

func NewRequestInfoResolver(c *genericapiserver.Config) *RequestInfoResolver {
	return &RequestInfoResolver{
		customPrefixes:  map[string]*CustomResolver{},
		defaultResolver: genericapiserver.NewRequestInfoResolver(c),
	}
}

func NewDefaultResolver(prefix string) *CustomResolver {
	return &CustomResolver{
		Prefix:   prefix,
		Resolver: defaultResolver,
	}
}

func (r *RequestInfoResolver) NewRequestInfo(req *http.Request) (*apirequest.RequestInfo, error) {
	reqCopy := req
	if result, matched := r.runMatchedResolver(reqCopy.URL.Path); matched {
		reqCopy = req.Clone(context.Background())
		reqCopy.URL.Path = result
	}
	return r.defaultResolver.NewRequestInfo(reqCopy)
}

func (r *RequestInfoResolver) InsertCustomPrefixes(resolver *CustomResolver) {
	if resolver.Resolver == nil {
		resolver.Resolver = defaultResolver
	}

	r.lock.Lock()
	defer r.lock.Unlock()
	r.customPrefixes[resolver.Prefix] = resolver
}

func (r *RequestInfoResolver) runMatchedResolver(urlPath string) (result string, matched bool) {
	r.lock.Lock()
	defer r.lock.Unlock()

	for key, resolver := range r.customPrefixes {
		if strings.HasPrefix(urlPath, key) {
			return resolver.Resolver(urlPath, key), true
		}
	}
	return "", false
}

func defaultResolver(rawPath, prefix string) string {
	subPath := strings.TrimPrefix(rawPath, prefix)
	subPath = strings.Trim(subPath, "/")
	if len(strings.Split(subPath, "/")) < 3 {
		return rawPath
	}
	return subPath
}
