package client

import (
	"net/http"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/metrics"
)

func KubeAPIWrapper(rt http.RoundTripper) http.RoundTripper {
	return &TransportWrapper{RoundTripper: rt}
}

type TransportWrapper struct {
	http.RoundTripper
}

func (t *TransportWrapper) RoundTrip(req *http.Request) (*http.Response, error) {
	metricLabels := []string{req.Method, req.URL.Hostname(), req.URL.Path}

	metrics.KubernetesAPIRequestTotalMetric.WithLabelValues(metricLabels...).Inc()

	start := time.Now()

	defer func() {
		metrics.KubernetesAPIRequestDurationMSMetric.WithLabelValues(metricLabels...).Observe(float64(time.Since(start).Milliseconds()))
	}()

	resp, err := t.RoundTripper.RoundTrip(req)

	if err != nil {
		metrics.KubernetesAPIRequestFailureMetric.WithLabelValues(metricLabels...).Inc()
	}

	return resp, err
}
