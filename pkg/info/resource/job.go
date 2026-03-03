/*
Copyright 2020-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package resource

import (
	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/util"

	"github.com/ghodss/yaml"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// jobResource represents a collection of jobs.
type jobResource struct {
	context *context.Context
	// jobs is the raw output from listing jobs
	jobs *batchv1.JobList
}

// NewJobResource initializes a new job resource.
func NewJobResource(context *context.Context) Resource {
	return &jobResource{
		context: context,
	}
}

func (r *jobResource) Kind() string {
	return "Job"
}

// Fetch collects all jobs as defined by the configuration.
func (r *jobResource) Fetch() error {
	selector, err := GetResourceSelector(&r.context.Config)
	if err != nil {
		return err
	}

	r.jobs, err = r.context.KubeClient.BatchV1().Jobs(r.context.Namespace()).List(metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return err
	}

	return nil
}

func (r *jobResource) Write(b backend.Backend) error {
	for _, job := range r.jobs.Items {
		data, err := yaml.Marshal(job)
		if err != nil {
			return err
		}

		_ = b.WriteFile(util.ArchivePath(r.context.Namespace(), r.Kind(), job.Name, job.Name+".yaml"), string(data))
	}

	return nil
}

func (r *jobResource) References() []Reference {
	references := []Reference{}

	for _, job := range r.jobs.Items {
		references = append(references, newReference(r.Kind(), job.Name))
	}

	return references
}
