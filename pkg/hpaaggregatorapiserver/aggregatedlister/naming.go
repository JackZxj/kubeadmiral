package aggregatedlister

import (
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"

	fedcorev1a1 "github.com/kubewharf/kubeadmiral/pkg/apis/core/v1alpha1"
	"github.com/kubewharf/kubeadmiral/pkg/controllers/common"
	"github.com/kubewharf/kubeadmiral/pkg/util/annotation"
	"github.com/kubewharf/kubeadmiral/pkg/util/naming"
)

// MaxHashLength is maxLength of uint32
const MaxHashLength = 10

const ClusterNameAnnotationKey = common.DefaultPrefix + "cluster"
const RawNameAnnotationKey = common.DefaultPrefix + "raw-name"

func GenUniqueName(cluster, rawName string) string {
	return naming.GenerateFederatedObjectName(cluster, rawName)
}

func GetPossibleClusters(clusters []*fedcorev1a1.FederatedCluster, targetName string) []string {
	possibleClusters := make([]string, 0, len(clusters))
	for _, cluster := range clusters {
		if len(cluster.Name) > len(targetName) {
			continue
		}
		if (len(cluster.Name) > common.MaxFederatedObjectNameLength-MaxHashLength-1 &&
			strings.HasPrefix(targetName, cluster.Name[:common.MaxFederatedObjectNameLength-MaxHashLength])) ||
			strings.HasPrefix(targetName, cluster.Name) {
			possibleClusters = append(possibleClusters, cluster.Name)
		}
	}
	return possibleClusters
}

func MakeObjectUnique(obj client.Object, clusterName string) {
	_, _ = annotation.AddAnnotation(obj, ClusterNameAnnotationKey, clusterName)
	_, _ = annotation.AddAnnotation(obj, RawNameAnnotationKey, obj.GetName())
	obj.SetName(GenUniqueName(clusterName, obj.GetName()))
}
