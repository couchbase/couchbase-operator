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
	"io/ioutil"
	"net/http"
	"strings"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/netutil"
	"github.com/couchbase/couchbase-operator/pkg/util/portforward"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// check that prometheus sidecar container is exporting the correct metrics
// on all pods in the operator.
func CheckPrometheus(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, ctx *TLSContext) (string, error) {
	// storing the prometheus metrics
	responseDataStr := ""

	listOptions := &metav1.ListOptions{
		LabelSelector: constants.CouchbaseServerClusterKey + "=" + couchbase.Name,
	}

	pods, err := k8s.KubeClient.CoreV1().Pods(couchbase.Namespace).List(*listOptions)
	if err != nil {
		return responseDataStr, err
	}

	// check all pods
	for _, pod := range pods.Items {
		port, err := netutil.GetFreePort()
		if err != nil {
			return responseDataStr, fmt.Errorf("unable to allocate port %v", err)
		}

		pf := portforward.PortForwarder{
			Config:    k8s.Config,
			Client:    k8s.KubeClient,
			Namespace: k8s.Namespace,
			Pod:       pod.Name,
			Port:      port + ":9091",
		}
		if err := pf.ForwardPorts(); err != nil {
			return responseDataStr, err
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

		uri := fmt.Sprintf("%s://localhost:%s%s", scheme, port, "/metrics")

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

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("unable to read response %s for pod %s", uri, pod.Name)
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

func MustCheckPrometheus(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, ctx *TLSContext) string {
	responseDataStr, err := CheckPrometheus(k8s, couchbase, ctx)
	if err != nil {
		Die(t, err)
	}
	return responseDataStr
}

func ExposeMetric(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster, tls *TLSContext, metric, value string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return retryutil.RetryOnErr(ctx, 5*time.Second, func() error {
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

	listOptions := &metav1.ListOptions{
		LabelSelector: constants.CouchbaseServerClusterKey + "=" + couchbase.Name,
	}
	pods, err := k8s.KubeClient.CoreV1().Pods(couchbase.Namespace).List(*listOptions)
	if err != nil {
		return responseDataStr, err
	}

	// check all pods
	for _, pod := range pods.Items {
		port, err := netutil.GetFreePort()
		if err != nil {
			return responseDataStr, fmt.Errorf("unable to allocate port %v", err)
		}

		pf := portforward.PortForwarder{
			Config:    k8s.Config,
			Client:    k8s.KubeClient,
			Namespace: k8s.Namespace,
			Pod:       pod.Name,
			Port:      port + ":9091",
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
		body, err := ioutil.ReadAll(resp.Body)
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
