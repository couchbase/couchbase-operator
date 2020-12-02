package portforward

import (
	"io/ioutil"
	"net/http"
	"sync"
	"time"

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

	// Setup the SPDY (HTTP/2.0) transport
	transport, upgrader, err := spdy.RoundTripperFor(pf.Config)
	if err != nil {
		return err
	}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport, Timeout: 10 * time.Second}, "POST", req.URL())

	// Finally do the actual work
	pf.stopChan = make(chan struct{})
	pf.readyChan = make(chan struct{})

	portForwarder, err := portforward.New(dialer, []string{pf.Port}, pf.stopChan, pf.readyChan, ioutil.Discard, ioutil.Discard)
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
		runtime.ErrorHandlers = []func(error){}
	}

	instances++

	mutex.Unlock()

	return func() {
		// Only perform the restore on the last call.
		mutex.Lock()

		instances--
		if instances == 0 {
			runtime.ErrorHandlers = handlers
		}

		mutex.Unlock()
	}
}
