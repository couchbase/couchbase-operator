package resource

import (
	ctx "context"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/util"

	"github.com/ghodss/yaml"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type couchbaseBackupRestoreResource struct {
	context *context.Context

	couchbaseBackupRestores []couchbasev2.CouchbaseBackupRestore
}

func NewCouchbaseBackupRestoreResource(context *context.Context) Resource {
	return &couchbaseBackupRestoreResource{
		context: context,
	}
}

func (r *couchbaseBackupRestoreResource) Kind() string {
	return "CouchbaseBackupRestore"
}

func (r *couchbaseBackupRestoreResource) Fetch() error {
	couchbaseBackupRestores, err := r.context.CouchbaseClient.CouchbaseV2().CouchbaseBackupRestores(r.context.Namespace()).List(ctx.Background(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	r.couchbaseBackupRestores = couchbaseBackupRestores.Items

	return nil
}

func (r *couchbaseBackupRestoreResource) Write(b backend.Backend) error {
	for _, couchbaseBackupRestore := range r.couchbaseBackupRestores {
		data, err := yaml.Marshal(couchbaseBackupRestore)
		if err != nil {
			return err
		}

		_ = b.WriteFile(util.ArchivePath(r.context.Namespace(), r.Kind(), couchbaseBackupRestore.Name, couchbaseBackupRestore.Name+".yaml"), string(data))
	}

	return nil
}

func (r *couchbaseBackupRestoreResource) References() []Reference {
	references := []Reference{}

	for _, couchbaseBackupRestore := range r.couchbaseBackupRestores {
		references = append(references, newReference(r.Kind(), couchbaseBackupRestore.Name))
	}

	return references
}
