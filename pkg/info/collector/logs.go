package collector

import (
	"bytes"
	"fmt"
	"io"

	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/resource"
	"github.com/couchbase/couchbase-operator/pkg/info/util"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
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

// getPod takes a resource reference and returns a pod from which we are able to collect logs,
// For collections such as deployments it simply picks one.
func (r *logCollector) getPod(resource resource.ResourceReference) (*v1.Pod, error) {
	// Inspect the resource kind and perform type specific processing
	switch resource.Kind() {
	case "Pod":
		return r.context.KubeClient.CoreV1().Pods(r.context.Config.Namespace).Get(resource.Name(), metav1.GetOptions{})

	case "Deployment":
		// Deployments will set a "app" label to that of the deployment
		nameLabelRequirement, err := labels.NewRequirement("app", selection.Equals, []string{resource.Name()})
		if err != nil {
			return nil, err
		}
		selector := labels.NewSelector()
		selector = selector.Add(*nameLabelRequirement)
		pods, err := r.context.KubeClient.CoreV1().Pods(r.context.Config.Namespace).List(metav1.ListOptions{LabelSelector: selector.String()})
		if err != nil {
			return nil, err
		}

		// Select just one instance
		if len(pods.Items) == 0 {
			return nil, fmt.Errorf("No pods delected for Deployment %s", resource.Name())
		}
		return &pods.Items[0], nil
	}

	return nil, nil
}

// Fetch collects all logs as defined for the resource
func (r *logCollector) Fetch(resource resource.ResourceReference) error {
	// Get a pod from the resource kind
	pod, err := r.getPod(resource)
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
		req := r.context.KubeClient.CoreV1().Pods(r.context.Config.Namespace).GetLogs(pod.Name, logOptions)

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
		b.WriteFile(util.ArchivePath(r.context.Config.Namespace, r.resource.Kind(), r.resource.Name(), name+".log"), string(logs.Bytes()))
	}
	return nil
}
