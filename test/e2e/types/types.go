// package types are types that are not dependant on any other part of the e2e framework.
package types

import (
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
}

// ClusterMap maps a cluster name to its configuration.
type ClusterMap map[string]*Cluster
