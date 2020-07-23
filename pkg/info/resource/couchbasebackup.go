package resource

import (
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/util"

	"github.com/ghodss/yaml"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type couchbaseBackupResource struct {
	context *context.Context

	couchbaseBackups []couchbasev2.CouchbaseBackup
}

func NewCouchbaseBackupResource(context *context.Context) Resource {
	return &couchbaseBackupResource{
		context: context,
	}
}

func (r *couchbaseBackupResource) Kind() string {
	return "CouchbaseBackup"
}

func (r *couchbaseBackupResource) Fetch() error {
	couchbaseBackups, err := r.context.CouchbaseClient.CouchbaseV2().CouchbaseBackups(r.context.Namespace()).List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	r.couchbaseBackups = couchbaseBackups.Items

	return nil
}

func (r *couchbaseBackupResource) Write(b backend.Backend) error {
	for _, couchbaseBackup := range r.couchbaseBackups {
		data, err := yaml.Marshal(couchbaseBackup)
		if err != nil {
			return err
		}

		_ = b.WriteFile(util.ArchivePath(r.context.Namespace(), r.Kind(), couchbaseBackup.Name, couchbaseBackup.Name+".yaml"), string(data))
	}

	return nil
}

func (r *couchbaseBackupResource) References() []ResourceReference {
	references := []ResourceReference{}

	for _, couchbaseBackup := range r.couchbaseBackups {
		references = append(references, newResourceReference(r.Kind(), couchbaseBackup.Name))
	}

	return references
}
