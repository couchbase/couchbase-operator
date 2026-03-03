/*
Copyright 2019-Present Couchbase, Inc.

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

// configMapResource represents a collection of configMaps
type configMapResource struct {
	context *context.Context
	// configMaps is the raw output from listing configMaps
	configMaps *v1.ConfigMapList
}

// NewConfigMapResource initializes a new configMap resource
func NewConfigMapResource(context *context.Context) Resource {
	return &configMapResource{
		context: context,
	}
}

func (r *configMapResource) Kind() string {
	return "ConfigMap"
}

// Fetch collects all configMaps as defined by the configuration
func (r *configMapResource) Fetch() error {
	selector, err := GetResourceSelector(&r.context.Config)
	if err != nil {
		return err
	}
	r.configMaps, err = r.context.KubeClient.CoreV1().ConfigMaps(r.context.Namespace()).List(metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return err
	}
	return nil
}

func (r *configMapResource) Write(b backend.Backend) error {
	for _, configMap := range r.configMaps.Items {
		data, err := yaml.Marshal(configMap)
		if err != nil {
			return err
		}

		_ = b.WriteFile(util.ArchivePath(r.context.Namespace(), r.Kind(), configMap.Name, configMap.Name+".yaml"), string(data))
	}
	return nil
}

func (r *configMapResource) References() []ResourceReference {
	return []ResourceReference{}
}
