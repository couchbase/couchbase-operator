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
	// ReconcileTotalMetric
	// name: reconcile_total
	// type: counter
	// help: Total reconcile operations performed on a specific cluster
	// unit:
	// added: 2.3.0
	// stability: committed
	// labels: namespace, name, result
	ReconcileTotalMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "reconcile_total",
		Help:      "Total reconcile operations performed on a specific cluster",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, []string{"namespace", "name", "result"})

	// ReconcileFailureMetric
	// name: reconcile_failures
	// type: counter
	// help: Total failed reconcile operations performed on a specific cluster
	// unit:
	// added: 2.3.0
	// stability: committed
	// labels: namespace, name
	ReconcileFailureMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "reconcile_failures",
		Help:      "Total failed reconcile operations performed on a specific cluster",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, []string{"namespace", "name"})

	// ReconcileDurationMetric
	// name: reconcile_time_seconds
	// type: histogram
	// help: Length of time per reconcile for a specific cluster
	// unit: seconds
	// added: 2.3.0
	// stability: committed
	// labels: namespace, name
	ReconcileDurationMetric = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:      "reconcile_time_seconds",
		Help:      "Length of time per reconcile for a specific cluster",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, []string{"namespace", "name"})

	// HTTPRequestTotalMetric
	// name: server_http_requests_total
	// type: counter
	// help: Total HTTP requests to Couchbase Server for a specific cluster
	// unit:
	// added: 2.3.0
	// stability: committed
	// labels: name, method, service, host
	HTTPRequestTotalMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "server_http_requests_total",
		Help:      "Total HTTP requests to Couchbase Server for a specific cluster.",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, []string{"name", "method", "service", "host"})

	// HTTPRequestTotalCodeMetric
	// name: server_http_request_codes_total
	// type: counter
	// help: Total HTTP requests to Couchbase Server for a specific cluster, method and status code returned
	// unit:
	// added: 2.3.0
	// stability: committed
	// labels: name, method, code, service, host
	HTTPRequestTotalCodeMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "server_http_request_codes_total",
		Help:      "Total HTTP requests to Couchbase Server for a specific cluster, method and status code returned",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, []string{"name", "method", "code", "service", "host"})

	// HTTPRequestFailureMetric
	// name: server_http_request_failures
	// type: counter
	// help: Total failed HTTP requests to Couchbase Server for a specific cluster
	// unit:
	// added: 2.3.0
	// stability: committed
	// labels: name, method, service, host
	HTTPRequestFailureMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "server_http_request_failures",
		Help:      "Total failed HTTP requests to Couchbase Server for a specific cluster.",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, []string{"name", "method", "service", "host"})

	// HTTPRequestDurationMSMetric
	// name: server_http_requests_time_milliseconds
	// type: histogram
	// help: Length of time per request for a specific cluster
	// unit: milliseconds
	// added: 2.3.0
	// stability: committed
	// labels: name, method, service, host
	HTTPRequestDurationMSMetric = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:      "server_http_requests_time_milliseconds",
		Help:      "Length of time per request for a specific cluster",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, []string{"name", "method", "service", "host"})

	// VolumeExpansionMetric
	// name: volume_expansions_total
	// type: counter
	// help: Total number of times the size of volumes have been increased under management
	// unit:
	// added: 2.7.0
	// stability: committed
	// labels: name, volumeName
	VolumeExpansionMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "volume_expansions_total",
		Help:      "Total number of times the size of volumes have been increased under management",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, []string{"name", "volumeName"})

	// SwapRebalancesTotalMetric
	// name: swap_rebalances_total
	// type: counter
	// help: Total number of swap rebalances performed by the operator
	// unit:
	// added: 2.7.0
	// stability: committed
	// labels: name
	SwapRebalancesTotalMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "swap_rebalances_total",
		Help:      "Total number of swap rebalances performed by the operator",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, []string{"name"})

	// DeltaRecoveriesTotalMetric
	// name: delta_recoveries_total
	// type: counter
	// help: Total number of delta recoveries performed by operator
	// unit:
	// added: 2.7.0
	// stability: committed
	// labels: name
	DeltaRecoveriesTotalMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "delta_recoveries_total",
		Help:      "Total number of delta recoveries performed by operator",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, []string{"name"})

	// SwapRebalanceFailuresMetric
	// name: swap_rebalance_failures
	// type: counter
	// help: Total number of times swap rebalances have failed
	// unit:
	// added: 2.7.0
	// stability: committed
	// labels: name
	SwapRebalanceFailuresMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "swap_rebalance_failures",
		Help:      "Total number of times swap rebalances have failed",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, []string{"name"})

	// DeltaRecoveryFailuresMetric
	// name: delta_recovery_failures
	// type: counter
	// help: The number of times delta recoveries have failed
	// unit:
	// added: 2.7.0
	// stability: committed
	// labels: name
	DeltaRecoveryFailuresMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "delta_recovery_failures",
		Help:      "The number of times delta recoveries have failed",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, []string{"name"})

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
		VolumeExpansionMetric,
		SwapRebalancesTotalMetric,
		SwapRebalanceFailuresMetric,
		DeltaRecoveriesTotalMetric,
		DeltaRecoveryFailuresMetric,
	)
}
