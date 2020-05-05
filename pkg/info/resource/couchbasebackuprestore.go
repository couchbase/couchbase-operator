package resource

import (
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/util"

	"github.com/ghodss/yaml"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type couchbaseBackupRestoreResource struct {
	context *context.Context

	couchbaseBackups []couchbasev2.CouchbaseBackup
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
	couchbaseBackups, err := r.context.CouchbaseClient.CouchbaseV2().CouchbaseBackups(r.context.Namespace()).List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	r.couchbaseBackups = couchbaseBackups.Items

	return nil
}

func (r *couchbaseBackupRestoreResource) Write(b backend.Backend) error {
	for _, couchbaseBackup := range r.couchbaseBackups {
		data, err := yaml.Marshal(couchbaseBackup)
		if err != nil {
			return err
		}

		_ = b.WriteFile(util.ArchivePath(r.context.Namespace(), r.Kind(), couchbaseBackup.Name, couchbaseBackup.Name+".yaml"), string(data))
	}

	return nil
}

func (r *couchbaseBackupRestoreResource) References() []Reference {
	return []Reference{}
}
