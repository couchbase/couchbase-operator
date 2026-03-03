/*
Copyright 2019-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

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
	couchbaseBackupRestores, err := r.context.CouchbaseClient.CouchbaseV2().CouchbaseBackupRestores(r.context.Namespace()).List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	r.couchbaseBackupRestores = couchbaseBackupRestores.Items

	return nil
}

func (r *couchbaseBackupRestoreResource) Write(b backend.Backend) error {
	for _, couchbaseBackup := range r.couchbaseBackupRestores {
		data, err := yaml.Marshal(couchbaseBackup)
		if err != nil {
			return err
		}

		_ = b.WriteFile(util.ArchivePath(r.context.Namespace(), r.Kind(), couchbaseBackup.Name, couchbaseBackup.Name+".yaml"), string(data))
	}

	return nil
}

func (r *couchbaseBackupRestoreResource) References() []ResourceReference {
	references := []ResourceReference{}

	for _, couchbaseBackupRestore := range r.couchbaseBackupRestores {
		references = append(references, newResourceReference(r.Kind(), couchbaseBackupRestore.Name))
	}

	return references
}
