package metrics

import (
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/version"

	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

type UUIDorName int

const (
	UUIDonly UUIDorName = iota
	UUIDandName
	None

	MetricNamespace = "couchbase"
	MetricSubsystem = "operator"
	ClusterUUID     = "cluster_uuid"
	ClusterName     = "cluster_name"
)

var (
	SeparateNameAndNamespace  bool
	UseHighCardinalityMetrics bool

	OptionalLabels UUIDorName = None
	// ReconcileTotalMetric
	// name: reconcile_total
	// type: counter
	// help: Total reconcile operations performed on a specific cluster
	// unit:
	// added: 2.3.0
	// stability: committed
	// labels: namespace, name, result
	// optionalLabels: cluster_uuid, cluster_name
	// nolint:godot
	ReconcileTotalMetric = prometheus.CounterVec{}

	// ReconcileFailureMetric
	// name: reconcile_failures
	// type: counter
	// help: Total failed reconcile operations performed on a specific cluster
	// unit:
	// added: 2.3.0
	// stability: committed
	// labels: namespace, name
	// optionalLabels: cluster_uuid, cluster_name
	// nolint:godot
	ReconcileFailureMetric = prometheus.CounterVec{}

	// ReconcileDurationMetric
	// name: reconcile_time_seconds
	// type: histogram
	// help: Length of time per reconcile for a specific cluster
	// unit: seconds
	// added: 2.3.0
	// stability: committed
	// labels: namespace, name
	// optionalLabels: cluster_uuid, cluster_name
	// nolint:godot
	ReconcileDurationMetric = prometheus.HistogramVec{}

	// lowCardinalityHTTPRequestMetricPaths is a list of paths that are considered low cardinality for the http request metrics.
	// Requests that have a path which is not in this list will have their "service" label set to "".
	// This is used to reduce the number of time series metrics by avoiding creating new ones for each dynamic path.
	// Users can disable this filtering by setting the "use-high-cardinality-metrics" environment variable to true.
	lowCardinalityHTTPRequestMetricPaths = []string{
		"/controller",
		"/node/controller",
		"/nodes/self",
		"/pools",
		"/pools/default",
		"/pools/default/stats/range",
		"/settings",
		"/settings/replications",
	}

	// HTTPRequestTotalMetric
	// name: server_http_requests_total
	// type: counter
	// help: Total HTTP requests to Couchbase Server for a specific cluster
	// unit:
	// added: 2.3.0
	// stability: committed
	// labels: name, method, service, host
	// optionalLabels: name, namespace
	// nolint:godot
	HTTPRequestTotalMetric = prometheus.CounterVec{}

	// HTTPRequestTotalCodeMetric
	// name: server_http_request_codes_total
	// type: counter
	// help: Total HTTP requests to Couchbase Server for a specific cluster, method and status code returned
	// unit:
	// added: 2.3.0
	// stability: committed
	// labels: name, method, code, service, host
	// optionalLabels: name, namespace
	// nolint:godot
	HTTPRequestTotalCodeMetric = prometheus.CounterVec{}

	// HTTPRequestFailureMetric
	// name: server_http_request_failures
	// type: counter
	// help: Total failed HTTP requests to Couchbase Server for a specific cluster
	// unit:
	// added: 2.3.0
	// stability: committed
	// labels: name, method, service, host
	// optionalLabels: name, namespace
	// nolint:godot
	HTTPRequestFailureMetric = prometheus.CounterVec{}

	// HTTPRequestDurationMSMetric
	// name: server_http_requests_time_milliseconds
	// type: histogram
	// help: Length of time per request for a specific cluster
	// unit: milliseconds
	// added: 2.3.0
	// stability: committed
	// labels: name, method, service, host
	// optionalLabels: name, namespace
	// nolint:godot
	HTTPRequestDurationMSMetric = prometheus.HistogramVec{}

	// VolumeExpansionMetric
	// name: volume_expansions_total
	// type: counter
	// help: Total number of times the size of volumes have been increased under management
	// unit:
	// added: 2.7.0
	// stability: committed
	// labels: name, volumeName
	// optionalLabels: cluster_uuid, cluster_name
	// nolint:godot
	VolumeExpansionMetric = prometheus.CounterVec{}

	// SwapRebalancesTotalMetric
	// name: swap_rebalances_total
	// type: counter
	// help: Total number of swap rebalances performed by the operator
	// unit:
	// added: 2.7.0
	// stability: committed
	// labels: name
	// optionalLabels: cluster_uuid, cluster_name
	// nolint:godot
	SwapRebalancesTotalMetric = prometheus.CounterVec{}

	// UpgradeDurationMSMetric
	// name: upgrade_duration
	// help: The time taken to perform an upgrade
	// unit: milliseconds
	// added: 2.7.0
	// stability: committed
	// labels: name
	// optionalLabels: cluster_uuid, cluster_name
	// nolint:godot
	UpgradeDurationMSMetric = prometheus.GaugeVec{}

	// PodReplacementsMetric
	// name: pod_replacements_total
	// type: counter
	// help: The amount of times operator has replaced a couchbase server pod due to a change in a couchbase cluster resources
	// unit:
	// added: 2.7.0
	// stability: committed
	// labels: name
	// optionalLabels: cluster_uuid, cluster_name
	// nolint:godot
	PodReplacementsMetric = prometheus.CounterVec{}

	// InPlaceUpgradeTotalMetric
	// name: in_place_upgrades_total
	// type: counter
	// help: Total number of in place upgrades performed by operator
	// unit:
	// added: 2.7.0
	// stability: committed
	// labels: name
	// optionalLabels: cluster_uuid, cluster_name
	// nolint:godot
	InPlaceUpgradeTotalMetric = prometheus.CounterVec{}

	// SwapRebalanceFailuresMetric
	// name: swap_rebalance_failures
	// type: counter
	// help: Total number of times swap rebalances have failed
	// unit:
	// added: 2.7.0
	// stability: committed
	// labels: name
	// optionalLabels: cluster_uuid, cluster_name
	// nolint:godot
	SwapRebalanceFailuresMetric = prometheus.CounterVec{}

	// InPlaceUpgradeFailuresMetric
	// name: in_place_upgrade_failures
	// type: counter
	// help: The number of times in place upgrades have failed
	// unit:
	// added: 2.7.0
	// stability: committed
	// labels: name
	// optionalLabels: cluster_uuid, cluster_name
	// nolint:godot
	InPlaceUpgradeFailuresMetric = prometheus.CounterVec{}

	// PodReplacementsFailedMetric
	// name: pod_replacements_failed
	// type: counter
	// help: Total number of times pods have failed to be recovered by the operator
	// unit:
	// added: 2.7.0
	// stability: committed
	// labels: name
	// optionalLabels: cluster_uuid, cluster_name
	// nolint:godot
	PodReplacementsFailedMetric = prometheus.CounterVec{}

	// PodRecoveriesMetric
	// name: pod_recoveries_total
	// type: counter
	// help: Total number of times operator has recovered a pod when the pod has been down
	// unit:
	// added: 2.7.0
	// stability: committed
	// labels: name, podName
	// optionalLabels: cluster_uuid, cluster_name
	// nolint:godot
	PodRecoveriesMetric = prometheus.CounterVec{}

	// PodRecoveryFailuresMetric
	// name: pod_recovery_failures_total
	// type: counter
	// help: Total number of times operator has failed to recover a pod
	// unit:
	// added: 2.7.0
	// stability: committed
	// labels: name, podName
	// optionalLabels: cluster_uuid, cluster_name
	// nolint:godot
	PodRecoveryFailuresMetric = prometheus.CounterVec{}

	// PodReadinessDurationMetric
	// name: pod_readiness_duration
	// type: gauge
	// help: The time it takes for a pod to enter a ready state
	// unit: milliseconds
	// added: 2.7.0
	// stability: committed
	// labels: name, serverClass
	// optionalLabels: cluster_uuid, cluster_name
	// nolint:godot
	PodReadinessDurationMetric = prometheus.HistogramVec{}

	// KubernetesAPIRequestTotalMetric
	// name: kubernetes_api_requests_total
	// type: counter
	// help: Total requests made to the Kubernetes API by the operator
	// unit:
	// added: 2.8.0
	// stability: committed
	// labels: method, host
	KubernetesAPIRequestTotalMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "kubernetes_api_requests_total",
		Help:      "Total requests made to the Kubernetes API by the operator",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, []string{"method", "host"})

	// KubernetesAPIRequestFailureMetric
	// name: kubernetes_api_request_failures
	// type: counter
	// help: Total failed requests to the Kubernetes API by the operator
	// unit:
	// added: 2.8.0
	// stability: committed
	// labels: method, host
	KubernetesAPIRequestFailureMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "kubernetes_api_request_failures",
		Help:      "Total failed requests to the Kubernetes API by the operator",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, []string{"method", "host"})

	// KubernetesAPIRequestDurationMSMetric
	// name: kubernetes_api_requests_time_milliseconds
	// type: histogram
	// help: Length of time per request to the Kubernetes API
	// unit: milliseconds
	// added: 2.8.0
	// stability: committed
	// labels: method, host
	KubernetesAPIRequestDurationMSMetric = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:      "kubernetes_api_requests_time_milliseconds",
		Help:      "Length of time per request to the Kubernetes API",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, []string{"method", "host"})

	// VolumeSizeUnderManagementBytesMetric
	// name: volume_size_under_management_bytes
	// type: gauge
	// help: Total memory claimed by volumes under management by the operator in bytes
	// unit: bytes
	// added: 2.8.0
	// stability: committed
	// labels: namespace, name
	// optionalLabels: cluster_uuid, cluster_name
	// nolint:godot
	VolumeSizeUnderManagementBytesMetric = prometheus.GaugeVec{}

	// MemoryUnderManagementBytesMetric
	// name: memory_under_management_bytes
	// type: gauge
	// help: Total memory requests for operator managed pods in bytes
	// unit: bytes
	// added: 2.8.0
	// stability: committed
	// labels: namespace, name
	// optionalLabels: cluster_uuid, cluster_name
	// nolint:godot
	MemoryUnderManagementBytesMetric = prometheus.GaugeVec{}

	// CPUUnderManagementMetric
	// name: cpu_under_management
	// type: gauge
	// help: Total cpu requests for operator managed pods in k8s cpu units
	// unit:
	// added: 2.8.0
	// stability: committed
	// labels: namespace, name
	// optionalLabels: cluster_uuid, cluster_name
	// nolint:godot
	CPUUnderManagementMetric = prometheus.GaugeVec{}

	// BackupJobsCreatedTotalMetric
	// name: backup_jobs_created_total
	// type: counter
	// help: Total number of backup jobs that have been created by the operator
	// unit:
	// added: 2.8.0
	// stability: committed
	// labels: namespace, backup_type
	// optionalLabels: cluster_uuid, cluster_name
	// nolint:godot
	BackupJobsCreatedTotalMetric = prometheus.CounterVec{}

	// ManualInterventionRequiredMetric
	// name: cluster_manual_intervention
	// type: gauge
	// help: Indicates whether manual intervention is required for the cluster
	// unit:
	// added: 2.9.0
	// stability: committed
	// labels: namespace, name
	// optionalLabels: cluster_uuid, cluster_name
	// nolint:godot
	ManualInterventionRequiredMetric = prometheus.GaugeVec{}

	buildInfoCollector = version.NewCollector("couchbase_operator")
)

func addOptionalLabels(existingLabels []string) []string {
	switch OptionalLabels {
	case UUIDonly:
		existingLabels = append(existingLabels, ClusterUUID)
	case UUIDandName:
		existingLabels = append(existingLabels, ClusterUUID, ClusterName)
	default:
		break
	}

	return existingLabels
}

func separateNameAndNamespaceWithConstrainedLabels(labels prometheus.ConstrainedLabels) prometheus.ConstrainedLabels {
	if SeparateNameAndNamespace {
		return append(labels, prometheus.ConstrainedLabel{Name: "namespace"}, prometheus.ConstrainedLabel{Name: "name"})
	}

	return append(labels, prometheus.ConstrainedLabel{Name: "name"})
}

func InitMetrics() {
	additionalLabels := os.Getenv("additional-prometheus-labels")

	SeparateNameAndNamespace, _ = strconv.ParseBool(os.Getenv("separate-cluster-name-and-namespace"))
	UseHighCardinalityMetrics, _ = strconv.ParseBool(os.Getenv("use-high-cardinality-metrics"))

	if strings.Compare(additionalLabels, "uuid-only") == 0 {
		OptionalLabels = UUIDonly
	} else if strings.Compare(additionalLabels, "uuid-and-name") == 0 {
		OptionalLabels = UUIDandName
	}

	var httpRequestServiceLabelConstraint prometheus.LabelConstraint

	if !UseHighCardinalityMetrics {
		sort.Slice(lowCardinalityHTTPRequestMetricPaths, func(i, j int) bool {
			return len(lowCardinalityHTTPRequestMetricPaths[i]) > len(lowCardinalityHTTPRequestMetricPaths[j])
		})

		httpRequestServiceLabelConstraint = NormalizeServicePath
	}

	HTTPRequestTotalMetric = *prometheus.V2.NewCounterVec(prometheus.CounterVecOpts{
		CounterOpts: prometheus.CounterOpts{
			Name:      "server_http_requests_total",
			Help:      "Total HTTP requests to Couchbase Server for a specific cluster.",
			Namespace: MetricNamespace,
			Subsystem: MetricSubsystem,
		},
		VariableLabels: separateNameAndNamespaceWithConstrainedLabels(prometheus.ConstrainedLabels{
			{Name: "method"},
			{Name: "service", Constraint: httpRequestServiceLabelConstraint},
			{Name: "host"},
		}),
	})

	HTTPRequestTotalCodeMetric = *prometheus.V2.NewCounterVec(prometheus.CounterVecOpts{
		CounterOpts: prometheus.CounterOpts{
			Name:      "server_http_request_codes_total",
			Help:      "Total HTTP requests to Couchbase Server for a specific cluster, method and status code returned",
			Namespace: MetricNamespace,
			Subsystem: MetricSubsystem,
		},
		VariableLabels: separateNameAndNamespaceWithConstrainedLabels(prometheus.ConstrainedLabels{
			{Name: "method"},
			{Name: "code"},
			{Name: "service", Constraint: httpRequestServiceLabelConstraint},
			{Name: "host"},
		}),
	})

	HTTPRequestFailureMetric = *prometheus.V2.NewCounterVec(prometheus.CounterVecOpts{
		CounterOpts: prometheus.CounterOpts{
			Name:      "server_http_request_failures",
			Help:      "Total failed HTTP requests to Couchbase Server for a specific cluster.",
			Namespace: MetricNamespace,
			Subsystem: MetricSubsystem,
		},
		VariableLabels: separateNameAndNamespaceWithConstrainedLabels(prometheus.ConstrainedLabels{
			{Name: "method"},
			{Name: "service", Constraint: httpRequestServiceLabelConstraint},
			{Name: "host"},
		}),
	})

	HTTPRequestDurationMSMetric = *prometheus.V2.NewHistogramVec(prometheus.HistogramVecOpts{
		HistogramOpts: prometheus.HistogramOpts{
			Name:      "server_http_requests_time_milliseconds",
			Help:      "Length of time per request for a specific cluster",
			Namespace: MetricNamespace,
			Subsystem: MetricSubsystem,
		},
		VariableLabels: separateNameAndNamespaceWithConstrainedLabels(prometheus.ConstrainedLabels{
			{Name: "method"},
			{Name: "service", Constraint: httpRequestServiceLabelConstraint},
			{Name: "host"},
		}),
	})

	ReconcileTotalMetric = *prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "reconcile_total",
		Help:      "Total reconcile operations performed on a specific cluster",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, addOptionalLabels([]string{"namespace", "name", "result"}))

	ReconcileFailureMetric = *prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "reconcile_failures",
		Help:      "Total failed reconcile operations performed on a specific cluster",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, addOptionalLabels([]string{"namespace", "name"}))

	ReconcileDurationMetric = *prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:      "reconcile_time_seconds",
		Help:      "Length of time per reconcile for a specific cluster",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, addOptionalLabels([]string{"namespace", "name"}))

	VolumeExpansionMetric = *prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "volume_expansions_total",
		Help:      "Total number of times the size of volumes have been increased under management",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, addOptionalLabels([]string{"name", "volumeName"}))

	SwapRebalancesTotalMetric = *prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "swap_rebalances_total",
		Help:      "Total number of swap rebalances performed by the operator",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, addOptionalLabels([]string{"name"}))

	UpgradeDurationMSMetric = *prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:      "upgrade_duration",
		Help:      "The time taken to perform an upgrade",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, addOptionalLabels([]string{"name"}))

	PodReplacementsMetric = *prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "pod_replacements_total",
		Help:      "The amount of times operator has replaced a couchbase server pod due to a change in a couchbase cluster resources",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, addOptionalLabels([]string{"name"}))

	InPlaceUpgradeTotalMetric = *prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "in_place_upgrades_total",
		Help:      "Total number of in place upgrades performed by operator",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, addOptionalLabels([]string{"name"}))

	SwapRebalanceFailuresMetric = *prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "swap_rebalance_failures",
		Help:      "Total number of times swap rebalances have failed",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, addOptionalLabels([]string{"name"}))

	InPlaceUpgradeFailuresMetric = *prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "in_place_upgrade_failures",
		Help:      "The number of times in place upgrades have failed",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, addOptionalLabels([]string{"name"}))

	PodReplacementsFailedMetric = *prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "pod_replacements_failed",
		Help:      "Total number of times pods have failed to be recovered by the operator",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, addOptionalLabels([]string{"name"}))

	PodRecoveriesMetric = *prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "pod_recoveries_total",
		Help:      "Total number of times operator has recovered a pod when the pod has been down",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, addOptionalLabels([]string{"name", "podName"}))

	PodRecoveryFailuresMetric = *prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "pod_recovery_failures_total",
		Help:      "Total number of times operator has failed to recover a pod",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, addOptionalLabels([]string{"name", "podName"}))

	PodReadinessDurationMetric = *prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:      "pod_readiness_duration",
		Help:      "The time it takes for a pod to enter a ready state",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, addOptionalLabels([]string{"name", "serverClass"}))

	VolumeSizeUnderManagementBytesMetric = *prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:      "volume_size_under_management_bytes",
		Help:      "Total memory claimed by volumes under management by the operator in bytes",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, addOptionalLabels([]string{"namespace", "name"}))

	MemoryUnderManagementBytesMetric = *prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:      "memory_under_management_bytes",
		Help:      "Total memory requests by operator managed pods in bytes",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, addOptionalLabels([]string{"namespace", "name"}))

	CPUUnderManagementMetric = *prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:      "cpu_under_management",
		Help:      "Total cpu requests by operator managed pods in k8s cpu units",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, addOptionalLabels([]string{"namespace", "name"}))

	BackupJobsCreatedTotalMetric = *prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "backup_jobs_created_total",
		Help:      "Total number of backup jobs that have been created by the operator",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, addOptionalLabels([]string{"namespace", "backup_type"}))

	ManualInterventionRequiredMetric = *prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:      "cluster_manual_intervention",
		Help:      "Indicates whether manual intervention is required for the cluster",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, addOptionalLabels([]string{"namespace", "name"}))

	metrics.Registry.MustRegister(
		ReconcileTotalMetric,
		ReconcileFailureMetric,
		ReconcileDurationMetric,
		HTTPRequestTotalMetric,
		HTTPRequestTotalCodeMetric,
		HTTPRequestFailureMetric,
		HTTPRequestDurationMSMetric,
		buildInfoCollector,
		VolumeExpansionMetric,
		SwapRebalancesTotalMetric,
		SwapRebalanceFailuresMetric,
		InPlaceUpgradeTotalMetric,
		InPlaceUpgradeFailuresMetric,
		PodReplacementsMetric,
		PodReplacementsFailedMetric,
		PodRecoveriesMetric,
		PodRecoveryFailuresMetric,
		UpgradeDurationMSMetric,
		PodReadinessDurationMetric,
		KubernetesAPIRequestTotalMetric,
		KubernetesAPIRequestFailureMetric,
		KubernetesAPIRequestDurationMSMetric,
		VolumeSizeUnderManagementBytesMetric,
		MemoryUnderManagementBytesMetric,
		CPUUnderManagementMetric,
		BackupJobsCreatedTotalMetric,
		ManualInterventionRequiredMetric,
	)
}

// NormalizeURLPath acts as a label constraint for the path label and reduces URL path cardinality for Couchbase API met metrics.
// Note that when fetching a metric using .WithLabelValues(), assuming the metric has been initialized with a constraint func, that func will also
// be applied to the label values in the fetch statement.
func NormalizeServicePath(path string) string {
	for _, p := range lowCardinalityHTTPRequestMetricPaths {
		// If the path is a prefix of the low cardinality path, return it. The list of paths should be ordered with the longest paths first such that these are matched first where
		// possible.
		if strings.HasPrefix(path, p) {
			return p
		}
	}

	return ""
}
