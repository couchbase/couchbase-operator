package collector

import (
	"bytes"
	"io"

	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/k8s"
	"github.com/couchbase/couchbase-operator/pkg/info/resource"
	"github.com/couchbase/couchbase-operator/pkg/info/util"

	"k8s.io/api/core/v1"
)

// logCollector represents a collection of logs
type logCollector struct {
	context *context.Context
	// logs is the raw output from listing logs
	logs map[string]*bytes.Buffer
	// resource keeps a record of what the logs are for
	resource resource.ResourceReference
}

// NewLogCollector initializes a new logs resource
func NewLogCollector(context *context.Context) Collector {
	return &logCollector{
		context: context,
	}
}

func (r *logCollector) Kind() string {
	return "Logs"
}

// Fetch collects all logs as defined for the resource
func (r *logCollector) Fetch(resource resource.ResourceReference) error {
	// Get a pod from the resource kind
	pod, err := k8s.GetPod(r.context, resource)
	if err != nil {
		return err
	}
	if pod == nil {
		return nil
	}

	// For each container read the logs and store in a mapping keyed on container name
	r.logs = map[string]*bytes.Buffer{}
	for _, container := range pod.Spec.Containers {
		logOptions := &v1.PodLogOptions{
			Container: container.Name,
		}
		req := r.context.KubeClient.CoreV1().Pods(r.context.Namespace()).GetLogs(pod.Name, logOptions)

		readCloser, err := req.Stream()
		if err != nil {
			return err
		}
		defer readCloser.Close()

		buf := &bytes.Buffer{}
		_, err = io.Copy(buf, readCloser)
		if err != nil {
			return err
		}

		// Discard logs which contain nothing
		if buf.Len() > 0 {
			r.logs[container.Name] = buf
		}
	}

	r.resource = resource
	return nil
}

func (r *logCollector) Write(b backend.Backend) error {
	for name, logs := range r.logs {
		_ = b.WriteFile(util.ArchivePath(r.context.Namespace(), r.resource.Kind(), r.resource.Name(), name+".log"), logs.String())
	}
	return nil
}
