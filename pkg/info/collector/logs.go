/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package collector

import (
	"bytes"
	ctx "context"
	"io"

	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/k8s"
	log_meta "github.com/couchbase/couchbase-operator/pkg/info/meta"
	"github.com/couchbase/couchbase-operator/pkg/info/resource"
	"github.com/couchbase/couchbase-operator/pkg/info/util"

	v1 "k8s.io/api/core/v1"
)

// logCollector represents a collection of logs.
type logCollector struct {
	context *context.Context
	// logs is the raw output from listing logs
	logs map[string]*bytes.Buffer
	// resource keeps a record of what the logs are for
	resource resource.Reference
}

// NewLogCollector initializes a new logs resource.
func NewLogCollector(context *context.Context) Collector {
	return &logCollector{
		context: context,
	}
}

func (r *logCollector) Kind() string {
	return "Logs"
}

// Fetch collects all logs as defined for the resource.
func (r *logCollector) Fetch(resource resource.Reference) error {
	// Get a pod from the resource kind
	pod, err := k8s.GetPod(r.context, resource.Kind(), resource.Name())
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

		readCloser, err := req.Stream(ctx.Background())
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
	var operatorLogsSet bool

	for name, logs := range r.logs {
		path := util.ArchivePath(r.context.Namespace(), r.resource.Kind(), r.resource.Name(), name+".log")

		if r.resource.IsOperator() && !operatorLogsSet {
			log_meta.SetOperatorLogPath(path)

			operatorLogsSet = true
		}

		_ = b.WriteFile(path, logs.String())
	}

	return nil
}
