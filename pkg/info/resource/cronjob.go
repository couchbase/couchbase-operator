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

	batchv1beta1 "k8s.io/api/batch/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// cronJobResource represents a collection of cronJobs.
type cronJobResource struct {
	context *context.Context
	// cronJobs is the raw output from listing cronJobs
	cronJobs *batchv1beta1.CronJobList
}

// NewCronJobResource initializes a new cronJob resource.
func NewCronJobResource(context *context.Context) Resource {
	return &cronJobResource{
		context: context,
	}
}

func (r *cronJobResource) Kind() string {
	return "CronJob"
}

// Fetch collects all cronJobs as defined by the configuration.
func (r *cronJobResource) Fetch() error {
	selector, err := GetResourceSelector(&r.context.Config)
	if err != nil {
		return err
	}

	r.cronJobs, err = r.context.KubeClient.BatchV1beta1().CronJobs(r.context.Namespace()).List(metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return err
	}

	return nil
}

func (r *cronJobResource) Write(b backend.Backend) error {
	for _, cronJob := range r.cronJobs.Items {
		data, err := yaml.Marshal(cronJob)
		if err != nil {
			return err
		}

		_ = b.WriteFile(util.ArchivePath(r.context.Namespace(), r.Kind(), cronJob.Name, cronJob.Name+".yaml"), string(data))
	}

	return nil
}

func (r *cronJobResource) References() []Reference {
	references := []Reference{}

	for _, cronJob := range r.cronJobs.Items {
		references = append(references, newReference(r.Kind(), cronJob.Name))
	}

	return references
}
