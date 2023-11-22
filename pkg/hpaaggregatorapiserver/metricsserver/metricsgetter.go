package metricsserver

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/metrics-server/pkg/scraper/client"
	"sigs.k8s.io/metrics-server/pkg/storage"
)

type MetricsGetter struct {
}

var _ client.KubeletMetricsGetter = &MetricsGetter{}

func (m *MetricsGetter) GetMetrics(ctx context.Context, node *corev1.Node) (*storage.MetricsBatch, error) {
	//TODO implement me
	panic("implement me")
}
