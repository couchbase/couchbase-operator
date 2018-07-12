package e2eutil

import (
	"bytes"
	"net/http"
	"testing"

	"k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/scheme"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
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

func (nodePort *NodePortService) CreateNodePortService(kubeClient kubernetes.Interface, nodePortNum int32) error {
	targetPortIntStr := intstr.IntOrString{IntVal: nodePort.RemotePortToExpose}
	servicePortSpec := v1.ServicePort{
		Name:       "analytics-np",
		Port:       nodePort.RemotePortToExpose,
		TargetPort: targetPortIntStr,
		NodePort:   nodePortNum,
	}

	// Create node-port service to export analytics service
	nodePortSpec := &v1.Service{
		TypeMeta:   metav1.TypeMeta{Kind: "NodePort"},
		ObjectMeta: metav1.ObjectMeta{Name: nodePort.ServiceName, Labels: nodePort.Labels},
		Spec: v1.ServiceSpec{
			Type:     "NodePort",
			Selector: nodePort.Selectors,
			Ports:    []v1.ServicePort{servicePortSpec},
		},
	}
	_, err := kubeClient.CoreV1().Services(nodePort.Namespace).Create(nodePortSpec)
	return err
}

func (nodePort *NodePortService) DeleteNodePortService(kubeClient kubernetes.Interface) error {
	return kubeClient.CoreV1().Services(nodePort.Namespace).Delete(nodePort.ServiceName, &metav1.DeleteOptions{})
}
