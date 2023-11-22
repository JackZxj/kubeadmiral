package aggregatedlister

import (
	"reflect"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fedcorev1a1 "github.com/kubewharf/kubeadmiral/pkg/apis/core/v1alpha1"
)

func genFederatedCluster(name string) *fedcorev1a1.FederatedCluster {
	return &fedcorev1a1.FederatedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func TestGetPossibleClusters(t *testing.T) {
	longName := strings.Repeat("abcde12345", 24)
	type args struct {
		clusters   []*fedcorev1a1.FederatedCluster
		targetName string
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{
			name: "one cluster matched",
			args: args{
				clusters:   []*fedcorev1a1.FederatedCluster{genFederatedCluster("foo")},
				targetName: "foo-1",
			},
			want: []string{"foo"},
		},
		{
			name: "two clusters matched with long name",
			args: args{
				clusters: []*fedcorev1a1.FederatedCluster{
					genFederatedCluster(longName + "1234567"),
					genFederatedCluster(longName + "123abcd"),
				},
				targetName: longName + "1234567890",
			},
			want: []string{longName + "1234567", longName + "123abcd"},
		},
		{
			name: "one cluster matched with long name",
			args: args{
				clusters: []*fedcorev1a1.FederatedCluster{
					genFederatedCluster(longName + "1234567"),
					genFederatedCluster(longName + "1abcdef"),
				},
				targetName: longName + "1234567890",
			},
			want: []string{longName + "1234567"},
		},
		{
			name: "no cluster matched with long name",
			args: args{
				clusters: []*fedcorev1a1.FederatedCluster{
					genFederatedCluster(longName + "1234567"),
				},
				targetName: longName + "123",
			},
			want: []string{},
		},
		{
			name: "no cluster matched",
			args: args{
				clusters:   []*fedcorev1a1.FederatedCluster{genFederatedCluster("foo")},
				targetName: "bar",
			},
			want: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetPossibleClusters(tt.args.clusters, tt.args.targetName); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetPossibleClusters() = %v, want %v", got, tt.want)
			}
		})
	}
}
