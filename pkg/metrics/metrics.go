package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// GRPCMetrics holds standard gRPC server metrics.
type GRPCMetrics struct {
	RequestsTotal *prometheus.CounterVec
	LatencyMS     *prometheus.HistogramVec
}

// NewGRPCMetrics registers Prometheus metrics for a gRPC service.
func NewGRPCMetrics(service string) *GRPCMetrics {
	return &GRPCMetrics{
		RequestsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "fleetops",
			Subsystem: service,
			Name:      "grpc_requests_total",
			Help:      "Total number of gRPC requests.",
		}, []string{"method", "status"}),

		LatencyMS: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "fleetops",
			Subsystem: service,
			Name:      "grpc_latency_ms",
			Help:      "gRPC request latency in milliseconds.",
			Buckets:   []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000},
		}, []string{"method"}),
	}
}

// ServeHTTP starts a /metrics HTTP endpoint on addr (e.g. ":9100").
// Call this in a goroutine.
func ServeHTTP(addr string) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	_ = http.ListenAndServe(addr, mux)
}
