package context

import (
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbase/couchbase-operator/pkg/info/config"

	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Context is a container which holds things needed across the application
type Context struct {
	// Config holds immutable configuration data loaded at startup
	Config config.Configuration
	// KubeConfig is the parsed kubernetes configuration
	KubeConfig *rest.Config
	// KubeClient is an initialized Kubernetes client for all the core APIs
	KubeClient *kubernetes.Clientset
	// KubeExtClient is an initialized Kubernetes extensions client for CRDs
	KubeExtClient clientset.Interface
	// CouchbaseClusterClient is an initialized Kubernetes client for CouchbaseCluster CRDs
	CouchbaseClusterClient versioned.Interface
}

// Copy makes a shallow copy of a Context object
func (c *Context) Copy() *Context {
	cpy := *c
	return &cpy
}
