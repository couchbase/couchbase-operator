package resource

import (
	ctx "context"

	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/util"

	"github.com/ghodss/yaml"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ServiceAccountResource represents a collection of ServiceAccounts.
type ServiceAccountResource struct {
	context *context.Context
	// ServiceAccounts is the raw output from listing ServiceAccounts
	ServiceAccounts *v1.ServiceAccountList
}

// NewServiceAccountResource initializes a new ServiceAccount resource.
func NewServiceAccountResource(context *context.Context) Resource {
	return &ServiceAccountResource{
		context: context,
	}
}

func (r *ServiceAccountResource) Kind() string {
	return "ServiceAccount"
}

// Fetch collects all ServiceAccounts as defined by the configuration.
func (r *ServiceAccountResource) Fetch() error {
	var err error

	r.ServiceAccounts, err = r.context.KubeClient.CoreV1().ServiceAccounts(r.context.Namespace()).List(ctx.Background(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	return nil
}

func (r *ServiceAccountResource) Write(b backend.Backend) error {
	for _, ServiceAccount := range r.ServiceAccounts.Items {
		data, err := yaml.Marshal(ServiceAccount)
		if err != nil {
			return err
		}

		_ = b.WriteFile(util.ArchivePath(r.context.Namespace(), r.Kind(), ServiceAccount.Name, ServiceAccount.Name+".yaml"), string(data))
	}

	return nil
}

func (r *ServiceAccountResource) References() []Reference {
	return []Reference{}
}
