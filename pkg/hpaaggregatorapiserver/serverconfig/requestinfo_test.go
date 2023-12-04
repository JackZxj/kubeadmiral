package serverconfig

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/sets"
	genericapiserver "k8s.io/apiserver/pkg/server"
)

func TestReq(t *testing.T) {
	tests := []struct {
		name         string
		url          string
		method       string
		expectedVerb string
	}{
		{
			name:         "1",
			url:          "/apis/hpaaggregator.kubeadmiral.io/v1alpha1/aggregation/apis/policy/v1/namespaces/default/poddisruptionbudgets?resourceVersion=2238&watch=true",
			method:       "GET",
			expectedVerb: "get",
		},
		{
			name:         "2",
			url:          "/apis/hpaaggregator.kubeadmiral.io/v1alpha1/aggregation/proxy/apis/policy/v1/namespaces/default/poddisruptionbudgets?resourceVersion=2238&watch=true",
			method:       "GET",
			expectedVerb: "watch",
		},
		{
			name:         "3",
			url:          "/apis/hpaaggregator.kubeadmiral.io/v1alpha1/proxy/apis/policy/v1/namespaces/default/poddisruptionbudgets?resourceVersion=2238&watch=true",
			method:       "GET",
			expectedVerb: "watch",
		},
		{
			name:         "4",
			url:          "/apis/cluster.karmada.io/v1alpha1/clusters/clustername/proxy/apis/policy/v1/namespaces/default/poddisruptionbudgets?resourceVersion=2238&watch=true",
			method:       "GET",
			expectedVerb: "watch",
		},
		{
			name:         "5",
			url:          "/apis/cluster.karmada.io/v1alpha1/clusters/clustername?resourceVersion=2238&watch=true",
			method:       "GET",
			expectedVerb: "watch",
		},
	}

	resolver := genericapiserver.NewRequestInfoResolver(&genericapiserver.Config{LegacyAPIGroupPrefixes: sets.NewString("/api")})

	for _, test := range tests {
		u, err := url.Parse(test.url)
		assert.NoError(t, err, test.url)

		info, err := resolver.NewRequestInfo(&http.Request{URL: u, Method: test.method})
		assert.NoError(t, err, test.url)
		assert.EqualValues(t, test.expectedVerb, info.Verb, test.url)
	}
}
