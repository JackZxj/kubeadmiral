package forward

import (
	"context"
	"fmt"
	"net/http"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/endpoints/handlers"
	"k8s.io/apiserver/pkg/endpoints/request"
)

var requestInfoResolver = &request.RequestInfoFactory{
	APIPrefixes:          sets.NewString("api", "apis"),
	GrouplessAPIPrefixes: sets.NewString("api"),
}

type ContextBasedNaming struct {
	Namer         runtime.Namer
	ClusterScoped bool

	resolver *request.RequestInfoFactory
}

// ContextBasedNaming implements ScopeNamer
var _ handlers.ScopeNamer = &ContextBasedNaming{}

func (n *ContextBasedNaming) Namespace(req *http.Request) (namespace string, err error) {
	requestInfo, ok := request.RequestInfoFrom(req.Context())
	if !ok {
		return "", fmt.Errorf("missing requestInfo")
	}
	return requestInfo.Namespace, nil
}

func (n *ContextBasedNaming) Name(req *http.Request) (namespace, name string, err error) {
	requestInfo, ok := request.RequestInfoFrom(req.Context())
	if !ok {
		return "", "", fmt.Errorf("missing requestInfo")
	}

	// renew requestInfo
	reqCopy := req.Clone(context.Background())
	reqCopy.URL.Path = requestInfo.Path
	newInfo, err := n.resolver.NewRequestInfo(reqCopy)
	if err != nil {
		return "", "", fmt.Errorf("failed to renew requestInfo: %w", err)
	}

	if len(newInfo.Name) == 0 {
		return "", "", errEmptyName
	}
	return newInfo.Namespace, newInfo.Name, nil
}

func (n *ContextBasedNaming) ObjectName(obj runtime.Object) (namespace, name string, err error) {
	name, err = n.Namer.Name(obj)
	if err != nil {
		return "", "", err
	}
	if len(name) == 0 {
		return "", "", errEmptyName
	}
	namespace, err = n.Namer.Namespace(obj)
	if err != nil {
		return "", "", err
	}
	return namespace, name, err
}

// errEmptyName is returned when API requests do not fill the name section of the path.
var errEmptyName = errors.NewBadRequest("name must be provided")
