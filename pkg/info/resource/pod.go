/*
Copyright 2018-Present Couchbase, Inc.

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

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// podResource represents a collection of pods
type podResource struct {
	context *context.Context
	// pods is the raw output from listing pods
	pods *v1.PodList
}

// NewPodResource initializes a new pod resource
func NewPodResource(context *context.Context) Resource {
	return &podResource{
		context: context,
	}
}

func (r *podResource) Kind() string {
	return "Pod"
}

// Fetch collects all pods as defined by the configuration
func (r *podResource) Fetch() error {
	selector, err := GetResourceSelector(&r.context.Config)
	if err != nil {
		return err
	}
	r.pods, err = r.context.KubeClient.CoreV1().Pods(r.context.Namespace()).List(metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return err
	}
	return nil
}

func (r *podResource) Write(b backend.Backend) error {
	for _, pod := range r.pods.Items {
		data, err := yaml.Marshal(pod)
		if err != nil {
			return err
		}

		_ = b.WriteFile(util.ArchivePath(r.context.Namespace(), r.Kind(), pod.Name, pod.Name+".yaml"), string(data))
	}
	return nil
}

func (r *podResource) References() []ResourceReference {
	references := []ResourceReference{}
	for _, pod := range r.pods.Items {
		references = append(references, newResourceReference(r.Kind(), pod.Name))
	}
	return references
}
