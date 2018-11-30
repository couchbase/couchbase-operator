package collector

import (
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/k8s"
	"github.com/couchbase/couchbase-operator/pkg/info/resource"
	"github.com/couchbase/couchbase-operator/pkg/info/util"
	"github.com/couchbase/couchbase-operator/pkg/util/portforward"
)

type operatorCollector struct {
	// context contains information to control execution
	context *context.Context
	// data records the raw operator output
	data map[string][]byte
	// resource keeps a record of what the operator collections are for
	resource resource.ResourceReference
}

// NewOperatorCollector initializes a new logs resource
func NewOperatorCollector(context *context.Context) Collector {
	return &operatorCollector{
		context: context,
		data:    map[string][]byte{},
	}
}

func (r *operatorCollector) Kind() string {
	return "Operator"
}

// Fetch collects all operator data as defined for the resource
func (r *operatorCollector) Fetch(resource resource.ResourceReference) error {
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

	// Open a channel to the operator http endpoint
	pf := portforward.PortForwarder{
		Config:    r.context.KubeConfig,
		Client:    r.context.KubeClient,
		Namespace: r.context.Config.Namespace,
		Pod:       pod.Name,
		Port:      r.context.Config.OperatorRestPort,
	}
	if err := pf.ForwardPorts(); err != nil {
		return fmt.Errorf("unable to forward port %s for pod %s", r.context.Config.OperatorRestPort, pod.Name)
	}
	defer pf.Close()

	// Paths to collect.  Technically these are available via /debug/pprof but that returns
	// HTML, which we aren't touching without some sane Xpath support.
	paths := map[string]string{
		"pprof.block":        "/debug/pprof/block?debug=1",
		"pprof.goroutine":    "/debug/pprof/goroutine?debug=1",
		"pprof.heap":         "/debug/pprof/heap?debug=1",
		"pprof.mutex":        "/debug/pprof/mutex?debug=1",
		"pprof.threadcreate": "/debug/pprof/threadcreate?debug=1",
		"stats.cluster":      "/v1/stats/cluster",
	}

	// The port-forwarder is a bit chatty on error, but we can shut it up
	restorer := portforward.Silent()
	defer restorer()

	for name, path := range paths {
		// Get the debug info, we need debug=1 here or it spits out binary
		uri := fmt.Sprintf("http://localhost:%s%s", r.context.Config.OperatorRestPort, path)
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

	r.resource = resource
	return nil
}

func (r *operatorCollector) Write(b backend.Backend) error {
	for name, data := range r.data {
		if len(data) > 0 {
			b.WriteFile(util.ArchivePath(r.context.Config.Namespace, r.resource.Kind(), r.resource.Name(), name), string(data))
		}
	}
	return nil
}
