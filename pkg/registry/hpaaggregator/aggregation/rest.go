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
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/client-go/dynamic"
	corev1 "k8s.io/client-go/listers/core/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	fedcorev1a1 "github.com/kubewharf/kubeadmiral/pkg/apis/core/v1alpha1"
	"github.com/kubewharf/kubeadmiral/pkg/apis/hpaaggregator/v1alpha1"
	fedclient "github.com/kubewharf/kubeadmiral/pkg/client/clientset/versioned"
	"github.com/kubewharf/kubeadmiral/pkg/controllers/common"
	aggregatedlister2 "github.com/kubewharf/kubeadmiral/pkg/hpaaggregatorapiserver/aggregatedlister"
	"github.com/kubewharf/kubeadmiral/pkg/registry/hpaaggregator/aggregation/forward"
	resource2 "github.com/kubewharf/kubeadmiral/pkg/registry/hpaaggregator/aggregation/metrics/resource"
	"github.com/kubewharf/kubeadmiral/pkg/util/informermanager"
)

type REST struct {
	client     dynamic.Interface
	fedClient  fedclient.Interface
	restConfig *restclient.Config
	scheme     *runtime.Scheme

	federatedInformerManager informermanager.FederatedInformerManager

	APIServer      *url.URL
	ProxyTransport http.RoundTripper
	NodeSelector   string
	MetricsGetter  resource2.MetricsGetter
	PodLister      cache.GenericLister
	NodeLister     corev1.NodeLister

	podHandler forward.PodHandler
	hpaHandler forward.HPAHandler

	logger klog.Logger
}

var _ rest.Storage = &REST{}
var _ rest.Scoper = &REST{}
var _ rest.Connecter = &REST{}

var proxyMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}

// NewREST returns a RESTStorage object that will work against API services.
func NewREST(
	kubeClient dynamic.Interface,
	fedClient fedclient.Interface,
	federatedInformerManager informermanager.FederatedInformerManager,
	scheme *runtime.Scheme,
	endpoint string,
	nodeSelector string,
	config *restclient.Config,
	minRequestTimeout time.Duration,
	logger klog.Logger,
) (*REST, error) {
	api, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to parse APIServer endpoint %s: %v", endpoint, err)
	}

	nodeLister := aggregatedlister2.NewNodeLister(federatedInformerManager)
	podLister := aggregatedlister2.NewPodLister(federatedInformerManager)
	metricsGetter := resource2.NewMetricsGetter(federatedInformerManager, logger)

	podHandler := forward.NewPodREST(
		federatedInformerManager,
		scheme,
		podLister,
		minRequestTimeout,
	)

	hpaHandler := forward.NewHPAREST(
		config,
		scheme,
		minRequestTimeout,
	)

	return &REST{
		client:                   kubeClient,
		fedClient:                fedClient,
		restConfig:               config,
		scheme:                   scheme,
		federatedInformerManager: federatedInformerManager,
		APIServer:                api,
		ProxyTransport:           DefaultProxyTransport(),
		NodeSelector:             nodeSelector,
		MetricsGetter:            metricsGetter,
		PodLister:                podLister,
		NodeLister:               nodeLister,
		podHandler:               podHandler,
		hpaHandler:               hpaHandler,
		logger:                   logger,
	}, nil
}

// New returns an empty object that can be used with Create and Update after request data has been put into it.
// This object must be a pointer type for use with Codec.DecodeInto([]byte, runtime.Object)
func (r *REST) New() runtime.Object {
	return &v1alpha1.Aggregation{}
}

// Destroy cleans up its resources on shutdown.
func (r *REST) Destroy() {
}

// NamespaceScoped returns true if the storage is namespaced
func (r *REST) NamespaceScoped() bool {
	return false
}

func (r *REST) Connect(ctx context.Context, _ string, _ runtime.Object, resp rest.Responder) (http.Handler, error) {
	requestInfo, found := genericapirequest.RequestInfoFrom(ctx)
	if !found {
		return nil, errors.New("no RequestInfo found in the context")
	}
	// /apis/hpaaggregator.kubeadmiral.io/v1alpha1/aggregation/api/v1/pods
	// /apis/hpaaggregator.kubeadmiral.io/v1alpha1/aggregation/apis/storage.k8s.io/v1/storageclasses

	switch {
	case isSelf(requestInfo):
		return nil, errors.New("can't proxy to self")
	case isRequestForPod(requestInfo):
		return r.podHandler.Handler(ctx)
	default:
		if ftc, ok := r.isRequestForHPA(requestInfo); ok {
			return r.hpaHandler.Handler(ctx, ftc)
		}
		return forward.NewForwardHandler(*r.APIServer, requestInfo.Path, r.ProxyTransport, resp, r.restConfig)
	}
}

// NewConnectOptions returns an empty options object that will be used to pass
// options to the Connect method. If nil, then a nil options object is passed to
// Connect.
func (r *REST) NewConnectOptions() (runtime.Object, bool, string) {
	return nil, true, ""
}

func (r *REST) ConnectMethods() []string {
	return proxyMethods
}

// DefaultProxyTransport creates the dialer infrastructure to connect to the clusters.
func DefaultProxyTransport() *http.Transport {
	var proxyDialerFn utilnet.DialFunc
	// Proxying to pods and services is IP-based... don't expect to be able to verify the hostname
	proxyTLSClientConfig := &tls.Config{InsecureSkipVerify: true}
	proxyTransport := utilnet.SetTransportDefaults(&http.Transport{
		DialContext:     proxyDialerFn,
		TLSClientConfig: proxyTLSClientConfig,
	})
	return proxyTransport
}

func isSelf(request *genericapirequest.RequestInfo) bool {
	return request.APIGroup == v1alpha1.SchemeGroupVersion.Group
}

func isRequestForPod(request *genericapirequest.RequestInfo) bool {
	return request.APIGroup == "" && request.APIVersion == "v1" && request.Resource == "pods"
}

func isRequestForNode(request *genericapirequest.RequestInfo) bool {
	return request.APIGroup == "" && request.APIVersion == "v1" && request.Resource == "nodes"
}

func (r *REST) isRequestForHPA(request *genericapirequest.RequestInfo) (*fedcorev1a1.FederatedTypeConfig, bool) {
	if request.Resource == "" || request.APIGroup == "" {
		return nil, false
	}
	ftc, _ := r.federatedInformerManager.
		GetFederatedTypeConfigLister().
		Get(fmt.Sprintf("%s.%s", request.Resource, request.APIGroup))
	if ftc != nil && ftc.GetAnnotations()[common.HPAScaleTargetRefPath] != "" {
		return ftc, true
	}
	return nil, false
}
