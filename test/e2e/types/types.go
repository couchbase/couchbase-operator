// package types are types that are not dependant on any other part of the e2e framework.
package types

import (
	"net/url"

	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// ResultType is used to encode the test case result type.
type ResultType string

const (
	// ResultTypePass means the test passed.
	ResultTypePass ResultType = "✔"

	// ResultTypeFail means the test failed.
	ResultTypeFail ResultType = "✗"

	// ResultTypeSkip means the test was skipped, most likely this is due
	// to the test being incompatible with the environment or dynamic
	// configuration parameters.
	ResultTypeSkip ResultType = "?"

	// ResultTypeErr means the test itself errored, at present this means
	// it raise a panic.
	ResultTypeErr ResultType = "!"
)

// Cluster represents a Kubernetes cluster.
type Cluster struct {
	// KubeClient is a Kubernetes client for the cluster.
	KubeClient kubernetes.Interface
	// DynamicClient is an untyped client.
	DynamicClient dynamic.Interface
	// RESTMapper maps from an object to API paths.
	RESTMapper meta.RESTMapper
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
