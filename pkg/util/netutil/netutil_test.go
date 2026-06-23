/*
Copyright 2026-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package netutil

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBlockMetadataEndpoints(t *testing.T) {
	tests := []struct {
		name    string
		address string
		blocked bool
	}{
		{"aws azure gcp imds", "169.254.169.254:80", true},
		{"alibaba imds", "100.100.100.200:80", true},
		{"aws imds ipv6", "[fd00:ec2::254]:80", true},
		{"aws imds ipv6 expanded", "[fd00:ec2:0:0:0:0:0:254]:443", true},
		{"normal public ip", "93.184.216.34:443", false},
		{"loopback allowed", "127.0.0.1:8091", false},
		{"private pod ip allowed", "10.1.2.3:8091", false},
		// Usually an IP here, but a name shouldn't cause an error.
		{"unresolved hostname", "example.com:80", false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := BlockMetadataEndpoints("tcp", test.address, nil)

			if test.blocked && !errors.Is(err, ErrBlockedMetadataEndpoint) {
				t.Fatalf("expected %q to be blocked, got err=%v", test.address, err)
			}

			if !test.blocked && err != nil {
				t.Fatalf("expected %q to be allowed, got err=%v", test.address, err)
			}
		})
	}
}

// TestSafeDialerHTTPClient checks that the guard is actually wired into an HTTP
// client built with SafeDialer, a request to a metadata endpoint is refused,
// while a request to a normal server still works.
func TestSafeDialerHTTPClient(t *testing.T) {
	client := &http.Client{
		Timeout:   2 * time.Second,
		Transport: &http.Transport{DialContext: SafeDialer().DialContext},
	}

	// A metadata endpoint must be refused before any connection happens.
	blockedResp, err := client.Get("http://169.254.169.254/latest/meta-data/")
	if blockedResp != nil {
		blockedResp.Body.Close()
	}

	if !errors.Is(err, ErrBlockedMetadataEndpoint) {
		t.Fatalf("expected request to metadata endpoint to be blocked, got err=%v", err)
	}

	// A normal server must still be reachable through the same client.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("expected request to normal server to succeed, got err=%v", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}
}
