/*
Copyright 2024-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package client

import (
	"github.com/couchbase/couchbase-operator/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type MockedRoundTrip struct {
	response *http.Response
	err      error
}

func (m *MockedRoundTrip) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.response, m.err
}

func TestKubeAPIWrapper(t *testing.T) {
	type kubeAPIWrapperTestCase struct {
		roundTrip      *MockedRoundTrip
		request        *http.Request
		expectedLabels []string
	}

	testcases := []kubeAPIWrapperTestCase{
		{
			roundTrip: &MockedRoundTrip{
				response: &http.Response{StatusCode: http.StatusOK, Body: http.NoBody},
				err:      nil,
			},
			request:        httptest.NewRequest("POST", "http://kube-api-server/resource/path/post", nil),
			expectedLabels: []string{"POST", "kube-api-server"},
		},
		{
			roundTrip: &MockedRoundTrip{
				response: &http.Response{StatusCode: http.StatusBadGateway, Body: http.NoBody},
				err:      nil,
			},
			request:        httptest.NewRequest("GET", "http://kube-api-server/resource/path/get", nil),
			expectedLabels: []string{"GET", "kube-api-server"},
		},
		{
			roundTrip: &MockedRoundTrip{
				response: &http.Response{Body: http.NoBody},
				err:      errors.ErrUnsupported,
			},
			request:        httptest.NewRequest("PATCH", "http://kube-api-server/resource/path/patch", nil),
			expectedLabels: []string{"PATCH", "kube-api-server"},
		},
	}

	metrics.InitMetrics()

	for _, testcase := range testcases {
		tw := &TransportWrapper{testcase.roundTrip}

		initialTotal := testutil.ToFloat64(metrics.KubernetesAPIRequestTotalMetric.WithLabelValues(testcase.expectedLabels...))
		initialFailureTotal := testutil.ToFloat64(metrics.KubernetesAPIRequestFailureMetric.WithLabelValues(testcase.expectedLabels...))

		resp, err := tw.RoundTrip(testcase.request)

		// Verify the total request count has increased by 1
		if newTotal := testutil.ToFloat64(metrics.KubernetesAPIRequestTotalMetric.WithLabelValues(testcase.expectedLabels...)); newTotal != initialTotal+1 {
			t.Errorf("expected kubernetes_api_requests_total metric to be %v, got %v", initialTotal+1, newTotal)
		}

		// Verify the failure counts have increased if the test case has an error
		newFailureTotal := testutil.ToFloat64(metrics.KubernetesAPIRequestFailureMetric.WithLabelValues(testcase.expectedLabels...))
		expectedFailureTotal := initialFailureTotal

		if err != nil {
			expectedFailureTotal++
		}

		if newFailureTotal != expectedFailureTotal {
			t.Errorf("expected kubernetes_api_request_failures metric to be %v, got %v", expectedFailureTotal, newFailureTotal)
		}

		resp.Body.Close()
	}
}
