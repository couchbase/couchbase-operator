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
