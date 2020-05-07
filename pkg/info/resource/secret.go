package resource

import (
	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/util"

	"github.com/ghodss/yaml"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// secretResource represents a collection of secrets
type secretResource struct {
	context *context.Context
	// secrets is the raw output from listing secrets
	secrets *v1.SecretList
}

// NewSecretResource initializes a new secret resource.
func NewSecretResource(context *context.Context) Resource {
	return &secretResource{
		context: context,
	}
}

func (r *secretResource) Kind() string {
	return "Secret"
}

// Fetch collects all secrets as defined by the configuration.
func (r *secretResource) Fetch() error {
	var err error

	r.secrets, err = r.context.KubeClient.CoreV1().Secrets(r.context.Namespace()).List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	// Redact ALL secret data.  We could try and be clever and whitelist certain
	// keys, but this is still too dangerous as we collect all secrets in a namespace
	// which may or may not relate to a cluster.
	for index, secret := range r.secrets.Items {
		for key := range secret.Data {
			r.secrets.Items[index].Data[key] = []byte{}
		}

		for key := range secret.StringData {
			r.secrets.Items[index].StringData[key] = ""
		}

		// Plug a gaping hole that 'kubectl apply' creates for us
		delete(r.secrets.Items[index].Annotations, "kubectl.kubernetes.io/last-applied-configuration")
	}

	return nil
}

func (r *secretResource) Write(b backend.Backend) error {
	for _, secret := range r.secrets.Items {
		data, err := yaml.Marshal(secret)
		if err != nil {
			return err
		}

		_ = b.WriteFile(util.ArchivePath(r.context.Namespace(), r.Kind(), secret.Name, secret.Name+".yaml"), string(data))
	}

	return nil
}

func (r *secretResource) References() []Reference {
	return []Reference{}
}
