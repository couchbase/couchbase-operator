/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

// package types are types that are not dependant on any other part of the e2e framework.
package types

import (
	"net/url"

	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"

	"k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Cluster represents a Kubernetes cluster.
type Cluster struct {
	// KubeClient is a Kubernetes client for the cluster.
	KubeClient kubernetes.Interface
	// CRClient is a Kubernetes client for CouchbaseCluster resources.
	CRClient versioned.Interface
	// DefaultSecret is the secret to use defining admin credentials.
	DefaultSecret *v1.Secret
	// Config is the REST configuration to use to directly access the Kubernetes API.
	Config *rest.Config
	// KubeConfPath is the path to use to get the Kubernetes client configuration.
	KubeConfPath string
	// Context is the context used in the Kubernetes config
	Context string
	// Namespace is the namespace to use
	Namespace string
	// Name is the cluster name
	Name string
	// PullSecrets is the list of pull secrets defined in this cluster.  These are
	// identified on a per-namespace basis due to the operator running in a different
	// namespace to the DAC.
	PullSecrets map[string][]string

	// Hacks - remove me

	// SupportsMultipleVolumeClaims overrides dynamic checks of storage classes.
	SupportsMultipleVolumeClaims bool
}

// APIHost returns the Kubernetes endpoint.  If you are using this please reconsider why
// it's probably being done wrong.
func (c *Cluster) APIHost() string {
	return c.Config.Host
}

// APIHostname returns the hostname of the Kubernetes endpoint. If you are using this please reconsider why
// it's probably being done wrong.
func (c *Cluster) APIHostname() (string, error) {
	u, err := url.Parse(c.Config.Host)
	if err != nil {
		return "", err
	}
	return u.Hostname(), nil
}

// ClusterMap maps a cluster name to its configuration.
type ClusterMap map[string]*Cluster
