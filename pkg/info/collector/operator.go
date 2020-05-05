package collector

import (
	"fmt"
	"github.com/couchbase/couchbase-operator/pkg/util/netutil"
	"io/ioutil"
	"net/http"

	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/k8s"
	"github.com/couchbase/couchbase-operator/pkg/info/resource"
	"github.com/couchbase/couchbase-operator/pkg/info/util"
	"github.com/couchbase/couchbase-operator/pkg/util/portforward"

	corev1 "k8s.io/api/core/v1"
)

type operatorCollector struct {
	// context contains information to control execution
	context *context.Context
	// data records the raw operator output
	data map[string][]byte
	// resource keeps a record of what the operator collections are for
	resource resource.Reference
}

// NewOperatorCollector initializes a new logs resource.
func NewOperatorCollector(context *context.Context) Collector {
	return &operatorCollector{
		context: context,
		data:    map[string][]byte{},
	}
}

func (r *operatorCollector) Kind() string {
	return "Operator"
}

// collectHTTP generically collects paths from a specific port and saves the data as the specified key.
func (r *operatorCollector) collectHTTP(pod *corev1.Pod, targetPort string, paths map[string]string) error {
	port, err := netutil.GetFreePort()
	if err != nil {
		return fmt.Errorf("unable to allocate port %v", err)
	}

	// Open a channel to the operator http endpoint
	pf := portforward.PortForwarder{
		Config:    r.context.KubeConfig,
		Client:    r.context.KubeClient,
		Namespace: r.context.Namespace(),
		Pod:       pod.Name,
		Port:      port + ":" + targetPort,
	}
	if err := pf.ForwardPorts(); err != nil {
		return fmt.Errorf("unable to forward port %s for pod %s", r.context.Config.OperatorRestPort, pod.Name)
	}

	defer pf.Close()

	// The port-forwarder is a bit chatty on error, but we can shut it up
	restorer := portforward.Silent()
	defer restorer()

	for name, path := range paths {
		// Get the debug info, we need debug=1 here or it spits out binary
		uri := fmt.Sprintf("http://localhost:%s%s", port, path)

		resp, err := http.Get(uri)
		if err != nil {
			fmt.Printf("unable to collect %s for pod %s\n", uri, pod.Name)
			continue
		}

		defer resp.Body.Close()

		// Buffer up the responses
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			fmt.Printf("unable to read response %s for pod %s\n", uri, pod.Name)
			continue
		}

		r.data[name] = body
	}

	return nil
}

// Fetch collects all operator data as defined for the resource.
func (r *operatorCollector) Fetch(resource resource.Reference) error {
	// Will match both deployments and pods without this.
	if resource.Kind() != "Deployment" {
		return nil
	}

	// Get a pod from the resource kind
	pod, err := k8s.GetPod(r.context, resource)
	if err != nil {
		return err
	}

	if pod == nil {
		return err
	}

	if len(pod.Spec.Containers) == 0 {
		return fmt.Errorf("pod %s for resource %s/%s contains no containers", pod.Name, resource.Kind(), resource.Name())
	}

	// Filter out anything that isn't the operator rather than being very intrusive
	if pod.Spec.Containers[0].Image != r.context.Config.OperatorImage {
		return nil
	}

	// Collect pprof data.  Technically these are available via /debug/pprof but that returns
	// HTML, which we aren't touching without some sane Xpath support.
	err = r.collectHTTP(pod, r.context.Config.OperatorRestPort, map[string]string{
		"pprof.block":        "/debug/pprof/block?debug=1",
		"pprof.goroutine":    "/debug/pprof/goroutine?debug=1",
		"pprof.heap":         "/debug/pprof/heap?debug=1",
		"pprof.mutex":        "/debug/pprof/mutex?debug=1",
		"pprof.threadcreate": "/debug/pprof/threadcreate?debug=1",
	})
	if err != nil {
		return err
	}

	// Collect prometheus metrics.
	err = r.collectHTTP(pod, r.context.Config.OperatorMetricsPort, map[string]string{
		"stats.cluster": "/metrics",
	})
	if err != nil {
		return err
	}

	r.resource = resource

	return nil
}

func (r *operatorCollector) Write(b backend.Backend) error {
	for name, data := range r.data {
		if len(data) > 0 {
			_ = b.WriteFile(util.ArchivePath(r.context.Namespace(), r.resource.Kind(), r.resource.Name(), name), string(data))
		}
	}

	return nil
}
