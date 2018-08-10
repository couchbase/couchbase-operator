package portforward

import (
	"bytes"
	"net/http"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// PortForwarder forwards a pod port to localhost in the background
type PortForwarder struct {
	Config    *rest.Config
	Client    *kubernetes.Clientset
	Namespace string
	Pod       string
	Port      string
	stopChan  chan struct{}
	readyChan chan struct{}
	errorChan chan error
	out       *bytes.Buffer
	errOut    *bytes.Buffer
}

// NodePort creates the service from pod with matching selectors to k8s cluster node
type NodePortService struct {
	ServiceName        string
	RemotePortToExpose int32
	Labels             map[string]string
	Selectors          map[string]string
	Namespace          string
}

// ForwardPorts accepts a Kubernetes configuration, pod and port and forwards
// the target port to the localhost
func (pf *PortForwarder) ForwardPorts() error {
	// Create the URL
	req := pf.Client.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(pf.Namespace).
		Name(pf.Pod).
		SubResource("portforward")

	// Setup the SPDY (HTTP/2.0) transport
	transport, upgrader, err := spdy.RoundTripperFor(pf.Config)
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
func (pf *PortForwarder) Close() error {
	close(pf.stopChan)
	return <-pf.errorChan
}
