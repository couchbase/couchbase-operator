/*
Copyright 2020-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package couchbaseutil

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/metrics"
	"github.com/couchbase/couchbase-operator/pkg/version"
)

const (
	ContentTypeURLEncoded string = "application/x-www-form-urlencoded"
	ContentTypeJSON       string = "application/json"

	HeaderContentType string = "Content-Type"
	HeaderUserAgent   string = "User-Agent"

	tcpConnectTimeout = 5 * time.Second
)

var ErrCertificateError = fmt.Errorf("certificate error")
var ErrStatusError = fmt.Errorf("unexpected status code")
var ErrUUIDError = fmt.Errorf("cluster UUID error")

// newClient creates a new HTTP client which offers connection persistence and
// also checks that the UUID of a host is what we expect when dialing before
// allowing further HTTP requests.
//
// Here be dragons!  You have been warned...
func (c *Client) makeClient() {
	// dialContext is a closure which gives us full control over TCP connections.
	// It is called when a HTTP client first dials a host.
	dialContext := func(ctx context.Context, network, addr string) (net.Conn, error) {
		// Establish a TCP connection
		dialer := &net.Dialer{
			Timeout:   tcpConnectTimeout,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}

		conn, err := dialer.DialContext(ctx, network, addr)
		if err != nil {
			return nil, errors.NewStackTracedError(err)
		}

		return conn, nil
	}

	// dialTLS is a closure which gives us full control over TCP connections.
	// It is called when a HTTPS client first dials a host.
	dialTLS := func(network, addr string) (net.Conn, error) {
		// If the TLS configuration is explicitly set use that, otherwise
		// use a basic configuration (which won't ever work unless your cluster
		// is signed by a CA defined in the ca-certificates package)
		var tlsClientConfig *tls.Config

		if c.tls != nil {
			tlsClientConfig = &tls.Config{
				RootCAs: x509.NewCertPool(),
			}

			// At the very least we need a CA certificate to attain trust in the remote end
			if ok := tlsClientConfig.RootCAs.AppendCertsFromPEM(c.tls.CACert); !ok {
				return nil, fmt.Errorf("%w: failed to append CA certificate", ErrCertificateError)
			}

			// If the remote end needs to trust us too we add a client certificate and key pair
			if c.tls.ClientAuth != nil {
				cert, err := tls.X509KeyPair(c.tls.ClientAuth.Cert, c.tls.ClientAuth.Key)
				if err != nil {
					return nil, errors.NewStackTracedError(err)
				}

				tlsClientConfig.Certificates = append(tlsClientConfig.Certificates, cert)
			}
		}

		// Establish a TCP connection with TLS transport
		dialer := &net.Dialer{
			Timeout:   tcpConnectTimeout,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}

		conn, err := tls.DialWithDialer(dialer, network, addr, tlsClientConfig)
		if err != nil {
			return nil, errors.NewStackTracedError(err)
		}

		return conn, nil
	}

	// Create the basic client configuration to support HTTP
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DialContext:  dialContext,
			DialTLS:      dialTLS,
			MaxIdleConns: 100,
		},
	}

	c.Client = client
}

// doRequest is the generic request handler for all client calls.
// Always ensure the body is initialized by NewRequest, rather than being
// tempted to do it here.  I made the mistake of just setting Body and that
// resulted in the client using chunked encoding.  Everything works fine,
// with the exception of XDCR, so use the library!
func (c Client) doRequest(request *http.Request, requestBody []byte, result interface{}) error {
	// Do the request recording the time taken.
	start := time.Now()

	// All requests send the username and password.
	request.SetBasicAuth(c.username, c.password)

	// Request logging.  Careful here, higher levels of verbosity generate a huge
	// amount more log traffic. All requests have the following common attributes.
	logLabels := []interface{}{
		"cluster", c.cluster,
		"method", request.Method,
		"url", request.URL.String(),
	}

	if log.V(2).Enabled() {
		// Contains admin password.
		logLabels = append(logLabels, "headers", request.Header)

		// May contain passwords.
		if requestBody != nil {
			logLabels = append(logLabels, "request", string(requestBody))
		}
	}

	// Construct any additional variables we need for metrics
	metricPath := request.URL.Path
	metricHostname := request.URL.Hostname()

	metrics.HTTPRequestTotalMetric.WithLabelValues(c.cluster, request.Method, metricPath, metricHostname).Inc()

	// Logs are always emitted regardless of status.  Context is added to the log labels
	// depending on the path taken.
	defer func() {
		delta := time.Since(start)
		metrics.HTTPRequestDurationMSMetric.WithLabelValues(c.cluster, request.Method, metricPath, metricHostname).Observe(float64(delta.Milliseconds()))

		logLabels = append(logLabels, "time_ms", float64(delta.Nanoseconds())/1000000.0)

		log.V(1).Info("http", logLabels...)
	}()

	// Do the request.
	response, err := c.Client.Do(request)
	if err != nil {
		logLabels = append(logLabels, "error", err)

		metrics.HTTPRequestFailureMetric.WithLabelValues(c.cluster, request.Method, metricPath, metricHostname).Inc()

		return errors.NewStackTracedError(err)
	}

	logLabels = append(logLabels, "status", response.Status)
	metrics.HTTPRequestTotalCodeMetric.WithLabelValues(c.cluster, request.Method, IntToStr(response.StatusCode), metricPath, metricHostname).Inc()

	// Read the body so we can display it for really verbose debugging.
	defer response.Body.Close()

	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return errors.NewStackTracedError(err)
	}

	if log.V(2).Enabled() {
		if string(body) != "" {
			logLabels = append(logLabels, "response", string(body))
		}
	}

	// Anything outside of a 2XX we regard as an error.
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("%w: request failed %v %v %v: %v", errors.NewStackTracedError(ErrStatusError), request.Method, request.URL.String(), response.Status, string(body))
	}

	// Don't care about the returned data, just report success.
	if result == nil {
		return nil
	}

	// Handle the content types we expect.
	switch contentType := response.Header.Get("Content-Type"); contentType {
	case "application/json":
		if err := json.Unmarshal(body, result); err != nil {
			return errors.NewStackTracedError(err)
		}
	case "text/plain":
		s, ok := result.(*[]byte)
		if !ok {
			return fmt.Errorf("%w: unexpected type decode for text/plain", errors.NewStackTracedError(ErrTypeError))
		}

		copy(*s, body)
	default:
		return fmt.Errorf("%w: unexpected content type %s", errors.NewStackTracedError(ErrTypeError), contentType)
	}

	return nil
}

func (c *Client) Get(r *Request, host string) error {
	req, err := http.NewRequest("GET", host+r.Path, nil)
	if err != nil {
		return errors.NewStackTracedError(err)
	}

	req.Header = defaultHeaders()

	return c.doRequest(req, nil, r.Result)
}

func (c *Client) Post(r *Request, host string) error {
	req, err := http.NewRequest("POST", host+r.Path, bytes.NewReader(r.Body))
	if err != nil {
		return errors.NewStackTracedError(err)
	}

	req.Header = defaultHeaders()
	req.Header.Set(HeaderContentType, ContentTypeURLEncoded)

	return c.doRequest(req, r.Body, r.Result)
}

func (c *Client) PostJSON(r *Request, host string) error {
	req, err := http.NewRequest("POST", host+r.Path, bytes.NewReader(r.Body))
	if err != nil {
		return errors.NewStackTracedError(err)
	}

	req.Header = defaultHeaders()
	req.Header.Set(HeaderContentType, ContentTypeJSON)

	return c.doRequest(req, r.Body, r.Result)
}

func (c *Client) PostNoContentType(r *Request, host string) error {
	req, err := http.NewRequest("POST", host+r.Path, bytes.NewReader(r.Body))
	if err != nil {
		return errors.NewStackTracedError(err)
	}

	req.Header = defaultHeaders()

	return c.doRequest(req, r.Body, r.Result)
}

func (c *Client) Put(r *Request, host string) error {
	req, err := http.NewRequest("PUT", host+r.Path, bytes.NewReader(r.Body))
	if err != nil {
		return errors.NewStackTracedError(err)
	}

	req.Header = defaultHeaders()
	req.Header.Set(HeaderContentType, ContentTypeURLEncoded)

	return c.doRequest(req, r.Body, nil)
}

func (c *Client) PutJSON(r *Request, host string) error {
	req, err := http.NewRequest("PUT", host+r.Path, bytes.NewReader(r.Body))
	if err != nil {
		return errors.NewStackTracedError(err)
	}

	req.Header = defaultHeaders()
	req.Header.Set(HeaderContentType, ContentTypeJSON)

	return c.doRequest(req, r.Body, nil)
}

func (c *Client) Delete(r *Request, host string) error {
	req, err := http.NewRequest("DELETE", host+r.Path, nil)
	if err != nil {
		return errors.NewStackTracedError(err)
	}

	req.Header = defaultHeaders()

	return c.doRequest(req, nil, nil)
}

func defaultHeaders() http.Header {
	headers := http.Header{}
	headers.Set(HeaderUserAgent, version.UserAgent())
	headers.Set("Accept-Encoding", "application/json, text/plain, */*")

	return headers
}
