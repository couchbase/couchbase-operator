/*
Copyright 2021-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

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

	// UpgradeDurationMSMetric
	// name: upgrade_duration
	// help: The time taken to perform an upgrade
	// unit: milliseconds
	// added: 2.7.0
	// stability: committed
	// labels: name
	UpgradeDurationMSMetric = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:      "upgrade_duration",
		Help:      "The time taken to perform an upgrade",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, []string{"name"})

	// PodReplacementsMetric
	// name: pod_replacements_total
	// type: counter
	// help: The amount of times operator has replaced a couchbase server pod due to a change in a couchbase cluster resources
	// unit:
	// added: 2.7.0
	// stability: committed
	// labels: name
	PodReplacementsMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "pod_replacements_total",
		Help:      "The amount of times operator has replaced a couchbase server pod due to a change in a couchbase cluster resources",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, []string{"name"})

	// InPlaceUpgradeTotalMetric
	// name: in_place_upgrades_total
	// type: counter
	// help: Total number of in place upgrades performed by operator
	// unit:
	// added: 2.7.0
	// stability: committed
	// labels: name
	InPlaceUpgradeTotalMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "in_place_upgrades_total",
		Help:      "Total number of in place upgrades performed by operator",
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

	// InPlaceUpgradeFailuresMetric
	// name: in_place_upgrade_failures
	// type: counter
	// help: The number of times in place upgrades have failed
	// unit:
	// added: 2.7.0
	// stability: committed
	// labels: name
	InPlaceUpgradeFailuresMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "in_place_upgrade_failures",
		Help:      "The number of times in place upgrades have failed",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, []string{"name"})

	// PodReplacementsFailedMetric
	// name: pod_replacements_failed
	// type: counter
	// help: Total number of times pods have failed to be recovered by the operator
	// unit:
	// added: 2.7.0
	// stability: committed
	// labels: name
	PodReplacementsFailedMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "pod_replacements_failed",
		Help:      "Total number of times pods have failed to be recovered by the operator",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, []string{"name"})

	// PodRecoveriesMetric
	// name: pod_recoveries_total
	// type: counter
	// help: Total number of times operator has recovered a pod when the pod has been down
	// unit:
	// added: 2.7.0
	// stability: committed
	// labels: name, podName
	PodRecoveriesMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "pod_recoveries_total",
		Help:      "Total number of times operator has recovered a pod when the pod has been down",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, []string{"name", "podName"})

	// PodRecoveryFailuresMetric
	// name: pod_recovery_failures_total
	// type: counter
	// help: Total number of times operator has failed to recover a pod
	// unit:
	// added: 2.7.0
	// stability: committed
	// labels: name, podName
	PodRecoveryFailuresMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "pod_recovery_failures_total",
		Help:      "Total number of times operator has failed to recover a pod",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, []string{"name", "podName"})

	// PodReadinessDurationMetric
	// name: pod_readiness_duration
	// type: gauge
	// help: The time it takes for a pod to enter a ready state
	// unit: milliseconds
	// added: 2.7.0
	// stability: committed
	// labels: name, serverClass
	PodReadinessDurationMetric = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:      "pod_readiness_duration",
		Help:      "The time it takes for a pod to enter a ready state",
		Namespace: MetricNamespace,
		Subsystem: MetricSubsystem,
	}, []string{"name", "serverClass"})

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
		InPlaceUpgradeTotalMetric,
		InPlaceUpgradeFailuresMetric,
		PodReplacementsMetric,
		PodReplacementsFailedMetric,
		PodRecoveriesMetric,
		PodRecoveryFailuresMetric,
		UpgradeDurationMSMetric,
		PodReadinessDurationMetric,
	)
}
