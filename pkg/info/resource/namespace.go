package resource

import (
	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/util"

	"github.com/ghodss/yaml"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// namespaceResource represents a collection of namespaces
type namespaceResource struct {
	context *context.Context
	// namespaces is the raw output from listing namespaces
	namespace *corev1.Namespace
}

// NewNamespaceResource initializes a new namespace resource
func NewNamespaceResource(context *context.Context) Resource {
	return &namespaceResource{
		context: context,
	}
}

func (r *namespaceResource) Kind() string {
	return "Namespace"
}

// Fetch collects all namespaces as defined by the configuration
func (r *namespaceResource) Fetch() error {
	var err error
	if r.namespace, err = r.context.KubeClient.CoreV1().Namespaces().Get(r.context.Namespace(), metav1.GetOptions{}); err != nil {
		return err
	}
	return nil
}

func (r *namespaceResource) Write(b backend.Backend) error {
	data, err := yaml.Marshal(r.namespace)
	if err != nil {
		return err
	}

	_ = b.WriteFile(util.ArchivePathUnscoped(r.Kind(), r.namespace.Name, r.namespace.Name+".yaml"), string(data))
	return nil
}

func (r *namespaceResource) References() []Reference {
	return []Reference{}
}
