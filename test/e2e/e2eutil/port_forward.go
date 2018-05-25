package e2eutil

import (
	"bytes"
	"net/http"
	"testing"

	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/scheme"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// PortForwarder forwards a pod port to localhost in the background
type PortForwarder struct {
	Config    *rest.Config
	Namespace string
	Pod       string
	Port      string
	stopChan  chan struct{}
	readyChan chan struct{}
	errorChan chan error
	out       *bytes.Buffer
	errOut    *bytes.Buffer
}

// ForwardPorts accepts a Kubernetes configuration, pod and port and forwards
// the target port to the localhost
func (pf *PortForwarder) ForwardPorts() error {
	// Ensure we don't mutate the existing configuration
	config := rest.CopyConfig(pf.Config)

	// All this is code is to bascially get a URL along the lines of
	// https://192.168.99.100:8443/api/v1/namespaces/default/pods/cb-example-0000/portforward
	config.APIPath = "/api"
	config.GroupVersion = &schema.GroupVersion{Version: "v1"}
	config.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: scheme.Codecs}
	restClient, err := rest.RESTClientFor(config)
	if err != nil {
		return err
	}

	req := restClient.Post().
		Resource("pods").
		Namespace(pf.Namespace).
		Name(pf.Pod).
		SubResource("portforward")

	// Setup the SPDY (HTTP/2.0) transport
	transport, upgrader, err := spdy.RoundTripperFor(config)
	if err != nil {
		return err
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", req.URL())

	// Finally do the actual work
	pf.stopChan = make(chan struct{})
	pf.readyChan = make(chan struct{})
	pf.out = &bytes.Buffer{}
	pf.errOut = &bytes.Buffer{}

	portForwarder, err := portforward.New(dialer, []string{pf.Port}, pf.stopChan, pf.readyChan, pf.out, pf.errOut)
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

// Close cleanly terminates a port forward
func (pf *PortForwarder) Close(t *testing.T) {
	close(pf.stopChan)
	err := <-pf.errorChan
	if err != nil {
		t.Fatal(err)
	}
}
