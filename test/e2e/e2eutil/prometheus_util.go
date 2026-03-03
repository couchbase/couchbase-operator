/*
Copyright 2020-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2eutil

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/metrics"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/netutil"
	"github.com/couchbase/couchbase-operator/pkg/util/portforward"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	testconstants "github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func getPodMetrics(k8s *types.Cluster, podName, podPort string, ctx *TLSContext) (string, error) {
	forwardedPort, err := netutil.GetFreePort()
	if err != nil {
		return "", fmt.Errorf("unable to allocate port: %w", err)
	}

	pf := portforward.PortForwarder{
		Config:    k8s.Config,
		Client:    k8s.KubeClient,
		Namespace: k8s.Namespace,
		Pod:       podName,
		Port:      forwardedPort + ":" + podPort,
	}
	if err := pf.ForwardPorts(); err != nil {
		return "", err
	}

	defer pf.Close()

	scheme := "http"
	client := &http.Client{}

	if ctx != nil {
		scheme = "https"

		clientCert, err := tls.X509KeyPair(ctx.ClientCert, ctx.ClientKey)
		if err != nil {
			return "", err
		}

		tlsConfig := &tls.Config{
			RootCAs: x509.NewCertPool(),
			Certificates: []tls.Certificate{
				clientCert,
			},
		}

		tlsConfig.RootCAs.AddCert(ctx.CA.certificate)

		client.Transport = &http.Transport{
			TLSClientConfig: tlsConfig,
		}
	}

	uri := fmt.Sprintf("%s://localhost:%s%s", scheme, forwardedPort, "/metrics")

	// Buffer up the responses
	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return "", err
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("unable to read response %s for pod %s", uri, podName)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("remote call failed with response: %s %s", resp.Status, string(body))
	}

	responseDataStr := string(body)
	if len(responseDataStr) == 0 {
		return responseDataStr, fmt.Errorf("empty response")
	}

	return responseDataStr, nil
}

func checkOperatorMetrics(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, ctx *TLSContext) error {
	operatorPodSelector := "app=couchbase-operator"
	operatorMetricsPort := "8383"
	metricPrefix := metrics.MetricNamespace + "_" + metrics.MetricSubsystem + "_"

	_, err := checkAllPodMetrics(k8s, couchbase, ctx, operatorMetricsPort, operatorPodSelector, metricPrefix+"server_http_")
	if err != nil {
		return err
	}

	_, err = checkAllPodMetrics(k8s, couchbase, ctx, operatorMetricsPort, operatorPodSelector, metricPrefix+"reconcile_")

	return err
}

func MustCheckOperatorMetrics(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, ctx *TLSContext) {
	err := checkOperatorMetrics(k8s, couchbase, ctx)
	if err != nil {
		Die(t, err)
	}
}

func checkPrometheusAnnotations(annotations map[string]string) error {
	expectedAnnotations := []string{
		constants.AnnotationPrometheusScrape,
		constants.AnnotationPrometheusPath,
		constants.AnnotationPrometheusPort,
	}

	for _, expected := range expectedAnnotations {
		_, exists := annotations[expected]
		if !exists {
			return fmt.Errorf("missing annotation %q", expected)
		}
	}

	return nil
}

func checkAllPodMetrics(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, ctx *TLSContext, podPort, labelSelector, testString string) (string, error) {
	listOptions := metav1.ListOptions{
		LabelSelector: labelSelector,
	}

	pods, err := k8s.KubeClient.CoreV1().Pods(couchbase.Namespace).List(context.Background(), listOptions)
	if err != nil {
		return "", err
	}

	// check all pods
	responseDataStr := ""

	for _, pod := range pods.Items {
		responseDataStr, err = getPodMetrics(k8s, pod.Name, podPort, ctx)
		if err != nil {
			return responseDataStr, err
		}

		if !strings.Contains(responseDataStr, testString) {
			return responseDataStr, fmt.Errorf("response data does not contain any %s metrics", testString)
		}

		err := checkPrometheusAnnotations(pod.Annotations)
		if err != nil {
			return responseDataStr, err
		}
	}

	return responseDataStr, nil
}

// check that prometheus sidecar container is exporting the correct metrics
// on all pods in the operator.
func CheckPrometheus(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, ctx *TLSContext) (string, error) {
	serverPodSelector := testconstants.CouchbaseServerClusterKey + "=" + couchbase.Name
	serverMetricPort := "9091"

	return checkAllPodMetrics(k8s, couchbase, ctx, serverMetricPort, serverPodSelector, "couchbase")
}

func MustCheckPrometheus(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, ctx *TLSContext) string {
	responseDataStr, err := CheckPrometheus(k8s, couchbase, ctx)
	if err != nil {
		Die(t, err)
	}

	return responseDataStr
}

func ExposeMetric(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, tls *TLSContext, metric, value string, timeout time.Duration) error {
	return retryutil.RetryFor(timeout, func() error {
		responseDataStr, _ := CheckPrometheus(k8s, couchbase, tls)
		if !strings.Contains(responseDataStr, fmt.Sprintf("%s %s", metric, value)) {
			return fmt.Errorf("response data does not contain expected value of the metric")
		}

		return nil
	})
}

// MustExposeMetric checks if the value of metric obtained from response data matches with the given value of that metric.
func MustExposeMetric(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, ctx *TLSContext, metric, value string, timeout time.Duration) {
	if err := ExposeMetric(k8s, couchbase, ctx, metric, value, timeout); err != nil {
		Die(t, err)
	}
}

func CheckPrometheusWithAuthSecret(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, ctx *TLSContext, token []byte) (string, error) {
	// storing the prometheus metrics
	responseDataStr := ""

	listOptions := metav1.ListOptions{
		LabelSelector: testconstants.CouchbaseServerClusterKey + "=" + couchbase.Name,
	}

	pods, err := k8s.KubeClient.CoreV1().Pods(couchbase.Namespace).List(context.Background(), listOptions)
	if err != nil {
		return responseDataStr, err
	}

	// CBS 7+ uses port 8091, exporter uses port 9091
	metricsPort := "9091"
	serverVersionPrometheus := false

	tag, err := k8sutil.CouchbaseVersion(couchbase.Spec.Image)
	if err != nil {
		serverVersionPrometheus, _ = couchbaseutil.VersionAfter(tag, "7.0.0")
	}

	if serverVersionPrometheus {
		metricsPort = "8091"
	}

	// check all pods
	for _, pod := range pods.Items {
		port, err := netutil.GetFreePort()
		if err != nil {
			return responseDataStr, fmt.Errorf("unable to allocate port %w", err)
		}

		pf := portforward.PortForwarder{
			Config:    k8s.Config,
			Client:    k8s.KubeClient,
			Namespace: k8s.Namespace,
			Pod:       pod.Name,
			Port:      port + ":" + metricsPort,
		}
		if err := pf.ForwardPorts(); err != nil {
			return responseDataStr, err
		}

		defer pf.Close()

		// Create a Bearer string by appending string access token
		scheme := "http"
		client := &http.Client{}

		if ctx != nil {
			scheme = "https"

			clientCert, err := tls.X509KeyPair(ctx.ClientCert, ctx.ClientKey)
			if err != nil {
				return "", err
			}

			tlsConfig := &tls.Config{
				RootCAs: x509.NewCertPool(),
				Certificates: []tls.Certificate{
					clientCert,
				},
			}

			tlsConfig.RootCAs.AddCert(ctx.CA.certificate)

			client.Transport = &http.Transport{
				TLSClientConfig: tlsConfig,
			}
		}

		uri := fmt.Sprintf("%s://localhost:%s%s", scheme, port, "/metrics")

		// Buffer up the responses
		req, err := http.NewRequest(http.MethodGet, uri, nil)
		if err != nil {
			return "", err
		}

		req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", token))

		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}

		defer resp.Body.Close()

		// Buffer up the responses
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			fmt.Printf("unable to read response %s for pod %s\n", uri, pod.Name)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			return responseDataStr, fmt.Errorf("remote call failed with response: %s %s", resp.Status, string(body))
		}

		responseDataStr = string(body)
		if len(responseDataStr) == 0 {
			return responseDataStr, fmt.Errorf("empty response")
		}

		if !strings.Contains(responseDataStr, "couchbase") {
			return responseDataStr, fmt.Errorf("response data does not contain any couchbase metrics")
		}
	}

	return responseDataStr, nil
}

func MustCheckPrometheusWithAuthSecret(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, ctx *TLSContext, token []byte) string {
	responseDataStr, err := CheckPrometheusWithAuthSecret(k8s, couchbase, ctx, token)
	if err != nil {
		Die(t, err)
	}

	return responseDataStr
}
