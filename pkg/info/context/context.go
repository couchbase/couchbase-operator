package context

import (
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbase/couchbase-operator/pkg/info/config"

	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Context is a container which holds things needed across the application.
type Context struct {
	// Config holds immutable configuration data loaded at startup
	Config config.Configuration
	// KubeConfigLoader is the raw client configuration used to generate a REST config
	KubeConfigLoader clientcmd.ClientConfig
	// KubeConfig is the parsed kubernetes configuration
	KubeConfig *rest.Config
	// KubeClient is an initialized Kubernetes client for all the core APIs
	KubeClient *kubernetes.Clientset
	// KubeExtClient is an initialized Kubernetes extensions client for CRDs
	KubeExtClient clientset.Interface
	// CouchbaseClient is an initialized Kubernetes client for Couchbase CRDs
	CouchbaseClient versioned.Interface
	// NamespaceOverride allows you to ignore what kuebconfig or the CLI says
	NamespaceOverride string
}

// Copy makes a shallow copy of a Context object.
func (c *Context) Copy() *Context {
	cpy := *c
	return &cpy
}

// Namespace gets the namespace defined on the CLI or defaulting to what's in the Kubernetes
// configuration file.
func (c *Context) Namespace() string {
	if c.NamespaceOverride != "" {
		return c.NamespaceOverride
	}

	namespace, _, _ := c.KubeConfigLoader.Namespace()

	return namespace
}
