package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/version"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	MetricNamespace = "couchbase"
	MetricSubsystem = "operator"
)

var (
	ReconcileTotalMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "reconcile_total",
		Help:      "Total reconcile operations performed on a specific cluster",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, []string{"namespace", "name", "result"})

	ReconcileFailureMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "reconcile_failures",
		Help:      "Total failed reconcile operations performed on a specific cluster",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, []string{"namespace", "name"})

	ReconcileDurationMetric = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:      "reconcile_time_seconds",
		Help:      "Length of time per reconcile for a specific cluster",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, []string{"namespace", "name"})

	HTTPRequestTotalMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "server_http_requests_total",
		Help:      "Total HTTP requests to Couchbase Server for a specific cluster.",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, []string{"name", "method", "service", "host"})

	HTTPRequestTotalCodeMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "server_http_request_codes_total",
		Help:      "Total HTTP requests to Couchbase Server for a specific cluster, method and status code returned",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, []string{"name", "method", "code", "service", "host"})

	HTTPRequestFailureMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "server_http_request_failures",
		Help:      "Total failed HTTP requests to Couchbase Server for a specific cluster.",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, []string{"name", "method", "service", "host"})

	HTTPRequestDurationMSMetric = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:      "server_http_requests_time_milliseconds",
		Help:      "Length of time per request for a specific cluster",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, []string{"name", "method", "service", "host"})

	buildInfoCollector = version.NewCollector("couchbase_operator")
)

func init() {
	metrics.Registry.MustRegister(
		ReconcileTotalMetric,
		ReconcileFailureMetric,
		ReconcileDurationMetric,
		HTTPRequestTotalMetric,
		HTTPRequestFailureMetric,
		HTTPRequestDurationMSMetric,
		buildInfoCollector,
	)
}
