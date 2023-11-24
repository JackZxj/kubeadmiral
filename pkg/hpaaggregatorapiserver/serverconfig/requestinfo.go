package serverconfig

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/util/sets"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
	genericapiserver "k8s.io/apiserver/pkg/server"
)

type RequestInfoResolver struct {
	lock sync.RWMutex

	customPrefixes     map[string]*CustomResolver
	connecterBlacklist sets.Set[string]
	defaultResolver    *apirequest.RequestInfoFactory
}

type CustomResolver struct {
	Prefix   string
	Resolver func(rawPath, prefix string) string
}

var _ apirequest.RequestInfoResolver = &RequestInfoResolver{}

func NewRequestInfoResolver(c *genericapiserver.Config) *RequestInfoResolver {
	return &RequestInfoResolver{
		customPrefixes:     map[string]*CustomResolver{},
		defaultResolver:    genericapiserver.NewRequestInfoResolver(c),
		connecterBlacklist: sets.Set[string]{},
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
	info, err := r.defaultResolver.NewRequestInfo(reqCopy)
	if err == nil && !r.inBlacklist(req.URL.Path) && info.Name == "" {
		// The name is required but not used in our rest.Connecter, so we have to set a default value
		// into it to avoid "name must be provided" error.
		info.Name = "default"
	}

	fmt.Printf("#### serverconfig/requestinfo: %+v,%v\n", info, err)
	return info, err
}

func (r *RequestInfoResolver) InsertCustomPrefixes(resolver *CustomResolver) {
	if resolver.Resolver == nil {
		resolver.Resolver = defaultResolver
	}

	r.lock.Lock()
	defer r.lock.Unlock()
	r.customPrefixes[resolver.Prefix] = resolver
}

func (r *RequestInfoResolver) InsertConnecterBlacklist(fullPaths ...string) {
	r.lock.Lock()
	defer r.lock.Unlock()
	r.connecterBlacklist.Insert(fullPaths...)
}

func (r *RequestInfoResolver) runMatchedResolver(urlPath string) (result string, matched bool) {
	r.lock.RLock()
	defer r.lock.RUnlock()

	for key, resolver := range r.customPrefixes {
		if strings.HasPrefix(urlPath, key) {
			return resolver.Resolver(urlPath, key), true
		}
	}
	return "", false
}

func (r *RequestInfoResolver) inBlacklist(path string) bool {
	r.lock.RLock()
	defer r.lock.RUnlock()

	for key := range r.connecterBlacklist {
		if strings.HasPrefix(path, key) {
			return true
		}
	}
	return false
}

func defaultResolver(rawPath, prefix string) string {
	subPath := strings.TrimPrefix(rawPath, prefix)
	subPath = strings.Trim(subPath, "/")
	return subPath
}
