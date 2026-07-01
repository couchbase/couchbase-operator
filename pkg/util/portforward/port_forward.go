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
	"io"
	"net/http"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

var (
	// mutex is used for avoiding races where multiple instances are alive at once.
	mutex = &sync.Mutex{}
	// instances is a counter of how many instances are alive.
	instances = 0
	// handlers backs up port forward error handlers when in silent mode.
	handlers []func(error)
)

// PortForwarder forwards a pod port to localhost in the background.
type PortForwarder struct {
	Config    *rest.Config
	Client    kubernetes.Interface
	Namespace string
	Pod       string
	Port      string
	stopChan  chan struct{}
	readyChan chan struct{}
	errorChan chan error
}

// ForwardPorts accepts a Kubernetes configuration, pod and port and forwards
// the target port to the localhost.
func (pf *PortForwarder) ForwardPorts() error {
	// Create the URL
	req := pf.Client.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(pf.Namespace).
		Name(pf.Pod).
		SubResource("portforward")

	dialer, err := pf.createPortForwardDialer(req)
	if err != nil {
		return err
	}

	// Finally do the actual work
	pf.stopChan = make(chan struct{})
	pf.readyChan = make(chan struct{})

	portForwarder, err := portforward.New(dialer, []string{pf.Port}, pf.stopChan, pf.readyChan, io.Discard, io.Discard)
	if err != nil {
		return err
	}

	// The forwarder is blocking so run in a separate go routine
	// then await the forwarder either erroring or becoming ready
	pf.errorChan = make(chan error)

	go func() {
		pf.errorChan <- portForwarder.ForwardPorts()
	}()

	select {
	case err := <-pf.errorChan:
		return err
	case <-pf.readyChan:
		return nil
	}
}

// createPortForwardDialer creates a http stream dialer using websockets and falls back to SPDY if websocket support is not available.
// Mostly copied from https://github.com/kubernetes/kubectl/blob/e1fe5406cffb25c0b563e53194fd12d02e5940eb/pkg/cmd/portforward/portforward.go#L139
func (pf *PortForwarder) createPortForwardDialer(req *rest.Request) (httpstream.Dialer, error) {
	wsDialer, err := portforward.NewSPDYOverWebsocketDialer(req.URL(), pf.Config)
	if err != nil {
		return nil, err
	}

	// Setup the SPDY (HTTP/2.0) transport - secondary incase customer still runs <1.30 kube version
	transport, upgrader, err := spdy.RoundTripperFor(pf.Config)
	if err != nil {
		return nil, err
	}
	spdyDialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport, Timeout: 10 * time.Second}, "POST", req.URL())

	// Create a fallback dialer configuration which will use websockets by default
	// and fallback to spdy incase websockets dialing isn't available
	// Ideally we should never fallback since we're not supposed to support <1.30 clusters anyway
	// but keeping in case customer has explicitly turned it off.
	// Also it helps to be good people and try to be backwards compatible as much as possible.
	dialer := portforward.NewFallbackDialer(wsDialer, spdyDialer, func(err error) bool {
		return httpstream.IsUpgradeFailure(err) || httpstream.IsHTTPSProxyError(err)
	})

	return dialer, nil
}

// Close cleanly terminates a port forward.
func (pf *PortForwarder) Close() error {
	close(pf.stopChan)
	return <-pf.errorChan
}

// Silent makes any runtime print statements go away.  Returns a restorer function that should
// be called once silence is no longer needed.
func Silent() func() {
	// Only perform the backup on the first call.
	mutex.Lock()

	if instances == 0 {
		handlers = runtime.ErrorHandlers
		runtime.ErrorHandlers = []func(error){} //nolint:reassign
	}

	instances++

	mutex.Unlock()

	return func() {
		// Only perform the restore on the last call.
		mutex.Lock()

		instances--
		if instances == 0 {
			runtime.ErrorHandlers = handlers //nolint:reassign
		}

		mutex.Unlock()
	}
}
