/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package portforward

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// TestPortForwarder_WebsocketsSuccess verifies that when the primary WebSocket
// handshake completes successfully, the fallback layer is bypassed entirely.
// Works as following
// Creates a test http server which upgrades the connection to websocket
// reads the first message so the client thinks the connection is successful
// ensures the fallback dialer was not used by checking the protocol sequence.
func TestPortForwarder_WebsocketsSuccess(t *testing.T) {
	var mu sync.Mutex
	var protocolSequence []string

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgradeHeader := strings.ToLower(r.Header.Get("Upgrade"))

		mu.Lock()
		if strings.Contains(upgradeHeader, "websocket") {
			protocolSequence = append(protocolSequence, "websocket")
		} else if strings.Contains(upgradeHeader, "spdy") || r.Method == "POST" {
			protocolSequence = append(protocolSequence, "spdy")
		}
		mu.Unlock()

		if strings.Contains(upgradeHeader, "websocket") {
			// Dynamically extract the requested subprotocol string to echo back
			// This is a requirement of the Websocket RFC, and is required for the client to consider the handshake successful.
			// https://datatracker.ietf.org/doc/html/rfc6455#section-11.3.1
			clientProtocols := r.Header.Get("Sec-WebSocket-Protocol")
			var responseHeader http.Header
			if clientProtocols != "" {
				parts := strings.Split(clientProtocols, ",")
				responseHeader = http.Header{
					"Sec-WebSocket-Protocol": {strings.TrimSpace(parts[0])}, // client-go sends SPDY/3.1+portforward.k8s.io
				}
			}

			conn, err := upgrader.Upgrade(w, r, responseHeader)
			if err != nil {
				t.Errorf("failed to upgrade connection to websocket: %v", err)
				return
			}
			defer conn.Close()

			// Block by reading the next message frame. client-go will immediately push initial multiplexed stream setup bytes
			// the instant its Dial() routing succeeds, safely unblocking this read.
			_, _, _ = conn.ReadMessage()
			return
		}

		if strings.Contains(upgradeHeader, "spdy") || r.Method == "POST" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	}))
	defer server.Close()

	config := &rest.Config{
		Host:    server.URL,
		Timeout: 5 * time.Second,
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatalf("Failed to create kubernetes client: %v", err)
	}

	// This configuration is mostly irrelevant, since the test server is not actually a Kubernetes API server,
	// but it is required to satisfy the PortForwarder struct.
	pf := &PortForwarder{
		Config:    config,
		Client:    client,
		Namespace: "default",
		Pod:       "test-pod",
		Port:      "8080",
	}

	_ = pf.ForwardPorts()

	mu.Lock()
	defer mu.Unlock()

	if len(protocolSequence) == 0 {
		t.Fatal("Expected at least one connection attempt, but recorded none")
	}

	if len(protocolSequence) > 1 || protocolSequence[0] != "websocket" {
		t.Fatalf("Security Flaw: Fallback dialer incorrectly dropped back to SPDY! Sequence: %v", protocolSequence)
	}
}

// TestPortForwarder_FallbackDialerSequence verifies that when the primary WebSocket
// handshake fails, the dialer automatically falls back to SPDY.
func TestPortForwarder_FallbackDialerSequence(t *testing.T) {
	var mu sync.Mutex
	var protocolSequence []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgradeHeader := strings.ToLower(r.Header.Get("Upgrade"))

		mu.Lock()
		if strings.Contains(upgradeHeader, "websocket") {
			protocolSequence = append(protocolSequence, "websocket")
		} else if strings.Contains(upgradeHeader, "spdy") || r.Method == "POST" {
			protocolSequence = append(protocolSequence, "spdy")
		}
		mu.Unlock()

		w.WriteHeader(http.StatusBadRequest)
		_, err := w.Write([]byte("Upgrade rejected by mock server"))
		if err != nil {
			t.Errorf("error writing response: %v", err)
		}
	}))
	defer server.Close()

	config := &rest.Config{
		Host:    server.URL,
		Timeout: 2 * time.Second,
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatalf("Failed to build kubernetes client: %v", err)
	}

	pf := &PortForwarder{
		Config:    config,
		Client:    client,
		Namespace: "test-namespace",
		Pod:       "test-couchbase-pod",
		Port:      "8080:8091",
	}

	if err := pf.ForwardPorts(); err == nil {
		t.Fatal("Expected ForwardPorts to return a handshake failure error, but got nil")
	}

	mu.Lock()
	defer mu.Unlock()

	if len(protocolSequence) < 2 {
		t.Fatalf("Fallback dialer did not execute full sequence. Protocols: %v", protocolSequence)
	}

	if protocolSequence[0] != "websocket" {
		t.Fatalf("Expected primary attempt to be 'websocket', got: %q", protocolSequence[0])
	}

	if protocolSequence[1] != "spdy" {
		t.Fatalf("Expected secondary attempt to be 'spdy', got: %q", protocolSequence[1])
	}
}
