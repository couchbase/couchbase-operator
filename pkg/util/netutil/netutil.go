/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

// Network utility functions

package netutil

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"syscall"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/errors"
)

// Cloud providers expose instance credentials on these addresses, and
// they're only meant to be reached from inside the VM. We never need to talk to
// them, so we block them outright to stop a bad URL from being turned into a
// credential stealing request.
var metadataEndpoints = []net.IP{
	net.ParseIP("169.254.169.254"), // AWS, Azure, GCP, OpenStack, Oracle.
	net.ParseIP("100.100.100.200"), // Alibaba Cloud.
	net.ParseIP("fd00:ec2::254"),   // AWS over IPv6.
}

// ErrBlockedMetadataEndpoint says we refused to dial a cloud metadata address.
var ErrBlockedMetadataEndpoint = fmt.Errorf("refusing to connect to cloud metadata endpoint")

// BlockMetadataEndpoints can be plugged into a net.Dialer to reject any
// connection to a cloud metadata address. It checks the IP we're actually about
// to dial, so it also catches a hostname that quietly resolves to one of them.
func BlockMetadataEndpoints(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		// Not in host:port form, so treat the whole string as the host.
		host = address
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return nil
	}

	for _, blocked := range metadataEndpoints {
		if blocked.Equal(ip) {
			return ErrBlockedMetadataEndpoint
		}
	}

	return nil
}

// SafeDialer is a dialer that won't connect to cloud metadata endpoints. Use it
// for HTTP clients whose target URL comes from config or user input.
func SafeDialer() *net.Dialer {
	return &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   BlockMetadataEndpoints,
	}
}

// Wait for a TCP port to become available
// Checks the port once a second until success or cancelled by the context.
// Returns nil on success or the last error on failure.
func WaitForHostPort(ctx context.Context, hostport string) error {
	// Setup a ticker to retry every second
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	var err error

	for {
		var conn net.Conn

		if conn, err = net.DialTimeout("tcp", hostport, 1*time.Second); err == nil {
			conn.Close()
			return nil
		}

		// Block until the next tick, or we are cancelled
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return errors.NewStackTracedError(err)
		}
	}
}

// Wait for a TCP port to become available and for a TLS handshake to succeed
// Checks the port once a second until success or cancelled by the context.
// Returns nil on success or the last error on failure.
func WaitForHostPortTLS(ctx context.Context, hostport string, cacert []byte) error {
	// Configure TLS with our CA certificate
	tlsClientConfig := tls.Config{
		RootCAs: x509.NewCertPool(),
	}
	if ok := tlsClientConfig.RootCAs.AppendCertsFromPEM(cacert); !ok {
		return errors.NewStackTracedError(errors.ErrCertificateInvalid)
	}

	// Setup a ticker to retry every second
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	var err error

	for {
		// Try establish a TCP connection and perform a TLS handshake which
		// validates the host is using certificates signed by the CA
		var conn *tls.Conn

		if conn, err = tls.Dial("tcp", hostport, &tlsClientConfig); err == nil {
			conn.Close()
			return nil
		}

		// Block until the next tick, or we are cancelled
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return errors.NewStackTracedError(err)
		}
	}
}

// GetTLSState dials the target host returning an error if normal X.509 verification
// fails.  On success it returns the certificate chain presented by the server.
func GetTLSState(hostport string, cacert, clientCert, clientKey []byte) ([]*x509.Certificate, error) {
	// Configure TLS with our CA certificate
	tlsClientConfig := tls.Config{
		RootCAs: x509.NewCertPool(),
	}
	if ok := tlsClientConfig.RootCAs.AppendCertsFromPEM(cacert); !ok {
		return nil, errors.NewStackTracedError(errors.ErrCertificateInvalid)
	}

	if clientCert != nil {
		clientCertificate, err := tls.X509KeyPair(clientCert, clientKey)
		if err != nil {
			return nil, errors.NewStackTracedError(err)
		}

		tlsClientConfig.Certificates = append(tlsClientConfig.Certificates, clientCertificate)
	}

	conn, err := tls.Dial("tcp", hostport, &tlsClientConfig)
	if err != nil {
		return nil, errors.NewStackTracedError(err)
	}

	defer conn.Close()

	state := conn.ConnectionState()

	return state.VerifiedChains[0], nil
}

// GetFreePort probes the kernel for a randomly allocated port to use for port forwarding.
func GetFreePort() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", errors.NewStackTracedError(err)
	}

	defer listener.Close()

	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		return "", errors.NewStackTracedError(err)
	}

	return port, nil
}

// GetTLSHandshakeCertificateChainInsecure initiates a tls handshake with the target host and returns the certificate chain presented by the server.
// CA's that are self-signed are excluded from the chain such that it should contain a leaf and intermediates which can be climbed to the root.
// This should not be relied upon as a trusted or valid certificate chain as it is not verified.
// The order of the returned slice should be respected as this is the order presented by the server.
// clientCert is optional; when provided it is presented to the server during the handshake (required for mandatory mTLS).
func GetTLSHandshakeCertificateChainInsecure(hostport string, clientCert *tls.Certificate) ([]*x509.Certificate, error) {
	cfg := &tls.Config{
		InsecureSkipVerify: true, // intentionally skip verification to capture chain.
	}

	if clientCert != nil {
		cfg.Certificates = []tls.Certificate{*clientCert}
	}

	d := &net.Dialer{Timeout: 5 * time.Second}
	conn, err := tls.DialWithDialer(d, "tcp", hostport, cfg)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	var chain []*x509.Certificate
	for _, c := range conn.ConnectionState().PeerCertificates {
		if c.IsCA && c.CheckSignatureFrom(c) == nil {
			break
		}
		chain = append(chain, c)
	}

	return chain, nil
}
