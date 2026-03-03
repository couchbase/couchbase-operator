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
	"net"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/errors"
)

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
