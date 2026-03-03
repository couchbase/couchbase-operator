/*
Copyright 2020-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package util

import (
	"os"

	"k8s.io/apimachinery/pkg/api/meta"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

// Clients allows access to the Kubernetes API.
type Clients struct {
	config     *rest.Config
	kubernetes kubernetes.Interface
	dynamic    dynamic.Interface
	mapper     meta.RESTMapper
}

// NewClients initializes a new clients structure.
func NewClients() (*Clients, error) {
	config, err := clientcmd.BuildConfigFromFlags("", os.Getenv("HOME")+"/.kube/config")
	if err != nil {
		return nil, err
	}

	kubernetes, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	dynamic, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	c := &Clients{
		config:     config,
		kubernetes: kubernetes,
		dynamic:    dynamic,
	}

	if err := c.Refresh(); err != nil {
		return nil, err
	}

	return c, nil
}

// Refresh is called to refresh the dynamic client especially the resource mapping
// that changes as CRDs are added to the system.
func (c *Clients) Refresh() error {
	groupresources, err := restmapper.GetAPIGroupResources(c.kubernetes.Discovery())
	if err != nil {
		return err
	}

	c.mapper = restmapper.NewDiscoveryRESTMapper(groupresources)

	return nil
}
