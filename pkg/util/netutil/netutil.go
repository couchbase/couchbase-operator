// Network utility functions

package netutil

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"time"
)

// Wait for a TCP port to become available
// Checks the port once a second until success or cancelled by the context.
// Returns nil on success or the last error on failure
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
			return fmt.Errorf("%v: Connection error - %v", ctx.Err(), err)
		}
	}
}

// Wait for a TCP port to become available and for a TLS handshake to succeed
// Checks the port once a second until success or cancelled by the context.
// Returns nil on success or the last error on failure
func WaitForHostPortTLS(ctx context.Context, hostport string, cacert []byte) error {
	// Configure TLS with our CA certificate
	tlsClientConfig := tls.Config{
		RootCAs: x509.NewCertPool(),
	}
	if ok := tlsClientConfig.RootCAs.AppendCertsFromPEM(cacert); !ok {
		return fmt.Errorf("failed to append CA certificate")
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
			return fmt.Errorf("%v: TLS Configuration error - %v", ctx.Err(), err)
		}
	}
}
