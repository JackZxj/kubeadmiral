package metricsserver

import (
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/client-go/tools/cache"
	"k8s.io/component-base/metrics"
	"k8s.io/klog/v2"
	"sigs.k8s.io/metrics-server/pkg/api"

	"sigs.k8s.io/metrics-server/pkg/scraper"
	"sigs.k8s.io/metrics-server/pkg/storage"
	"sigs.k8s.io/metrics-server/pkg/utils"
)

var (
	// initialized below to an actual value by a call to RegisterTickDuration
	// (acts as a no-op by default), but we can't just register it in the constructor,
	// since it could be called multiple times during setup.
	tickDuration = metrics.NewHistogram(&metrics.HistogramOpts{})
)

// RegisterServerMetrics creates and registers a histogram metric for
// scrape duration.
func RegisterServerMetrics(registrationFunc func(metrics.Registerable) error, resolution time.Duration) error {
	tickDuration = metrics.NewHistogram(
		&metrics.HistogramOpts{
			Namespace: "metrics_server",
			Subsystem: "manager",
			Name:      "tick_duration_seconds",
			Help:      "The total time spent collecting and storing metrics in seconds.",
			Buckets:   utils.BucketsForScrapeDuration(resolution),
		},
	)
	return registrationFunc(tickDuration)
}

func NewServer(
	nodes cache.Controller,
	pods cache.Controller,
	storage storage.Storage,
	scraper scraper.Scraper,
	resolution time.Duration,
) *Server {
	return &Server{
		nodes:      nodes,
		pods:       pods,
		storage:    storage,
		scraper:    scraper,
		resolution: resolution,
	}
}

// Server scrapes metrics and serves then using k8s api.
type Server struct {
	pods  cache.Controller
	nodes cache.Controller

	storage    storage.Storage
	scraper    scraper.Scraper
	resolution time.Duration

	// tickStatusMux protects tick fields
	tickStatusMux sync.RWMutex
	// tickLastStart is equal to start time of last unfinished tick
	tickLastStart time.Time
}

// RunUntil starts background scraping goroutine and runs apiserver serving metrics.
func (s *Server) RunUntil(stopCh <-chan struct{}) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start informers
	go s.nodes.Run(stopCh)
	go s.pods.Run(stopCh)

	// Ensure cache is up to date
	ok := cache.WaitForCacheSync(stopCh, s.nodes.HasSynced)
	if !ok {
		return nil
	}
	ok = cache.WaitForCacheSync(stopCh, s.pods.HasSynced)
	if !ok {
		return nil
	}

	// Start serving API and scrape loop
	s.runScrape(ctx)
	return nil
}

func (s *Server) runScrape(ctx context.Context) {
	ticker := time.NewTicker(s.resolution)
	defer ticker.Stop()
	s.tick(ctx, time.Now())

	for {
		select {
		case startTime := <-ticker.C:
			s.tick(ctx, startTime)
		case <-ctx.Done():
			return
		}
	}
}

func (s *Server) tick(ctx context.Context, startTime time.Time) {
	s.tickStatusMux.Lock()
	s.tickLastStart = startTime
	s.tickStatusMux.Unlock()

	ctx, cancelTimeout := context.WithTimeout(ctx, s.resolution)
	defer cancelTimeout()

	klog.V(6).InfoS("Scraping metrics")
	data := s.scraper.Scrape(ctx)

	klog.V(6).InfoS("Storing metrics")
	s.storage.Store(data)

	collectTime := time.Since(startTime)
	tickDuration.Observe(float64(collectTime) / float64(time.Second))
	klog.V(6).InfoS("Scraping cycle complete")
}

// RegisterMetrics registers
func RegisterMetrics(r metrics.KubeRegistry, metricResolution time.Duration) error {
	// register metrics Server components metrics
	err := RegisterServerMetrics(r.Register, metricResolution)
	if err != nil {
		return fmt.Errorf("unable to register Server metrics: %v", err)
	}
	err = scraper.RegisterScraperMetrics(r.Register)
	if err != nil {
		return fmt.Errorf("unable to register scraper metrics: %v", err)
	}
	err = api.RegisterAPIMetrics(r.Register)
	if err != nil {
		return fmt.Errorf("unable to register API metrics: %v", err)
	}
	err = storage.RegisterStorageMetrics(r.Register)
	if err != nil {
		return fmt.Errorf("unable to register storage metrics: %v", err)
	}

	return nil
}
