package validator

import (
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbase/couchbase-operator/pkg/validator/types"
	validationv2 "github.com/couchbase/couchbase-operator/pkg/validator/v2"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
)

func New(client kubernetes.Interface, couchbaseClient versioned.Interface, options *types.ValidatorOptions) *types.Validator {
	return types.New(client, couchbaseClient, options)
}

func CheckConstraints(v *types.Validator, resource runtime.Object) error {
	switch t := resource.(type) {
	case *couchbasev2.CouchbaseCluster:
		return validationv2.CheckConstraints(v, t)
	case *couchbasev2.CouchbaseBucket:
		return validationv2.CheckConstraintsBucket(v, t)
	case *couchbasev2.CouchbaseEphemeralBucket:
		return validationv2.CheckConstraintsEphemeralBucket(v, t)
	case *couchbasev2.CouchbaseMemcachedBucket:
		return validationv2.CheckConstraintsMemcachedBucket(v, t)
	case *couchbasev2.CouchbaseReplication:
		return validationv2.CheckConstraintsReplication(v, t)
	case *couchbasev2.CouchbaseUser:
		return validationv2.CheckConstraintsCouchbaseUser(v, t)
	case *couchbasev2.CouchbaseGroup:
		return validationv2.CheckConstraintsCouchbaseGroup(v, t)
	case *couchbasev2.CouchbaseBackup:
		return validationv2.CheckConstraintsBackup(v, t)
	case *couchbasev2.CouchbaseBackupRestore:
		return validationv2.CheckConstraintsBackupRestore(v, t)
	}

	return nil
}

func CheckImmutableFields(current, updated runtime.Object) error {
	switch t := current.(type) {
	case *couchbasev2.CouchbaseCluster:
		if t2, ok := updated.(*couchbasev2.CouchbaseCluster); ok {
			return validationv2.CheckImmutableFields(t, t2)
		}
	case *couchbasev2.CouchbaseBucket:
		if t2, ok := updated.(*couchbasev2.CouchbaseBucket); ok {
			return validationv2.CheckImmutableFieldsBucket(t, t2)
		}
	case *couchbasev2.CouchbaseEphemeralBucket:
		if t2, ok := updated.(*couchbasev2.CouchbaseEphemeralBucket); ok {
			return validationv2.CheckImmutableFieldsEphemeralBucket(t, t2)
		}
	case *couchbasev2.CouchbaseMemcachedBucket:
		if t2, ok := updated.(*couchbasev2.CouchbaseMemcachedBucket); ok {
			return validationv2.CheckImmutableFieldsMemcachedBucket(t, t2)
		}
	case *couchbasev2.CouchbaseReplication:
		if t2, ok := updated.(*couchbasev2.CouchbaseReplication); ok {
			return validationv2.CheckImmutableFieldsReplication(t, t2)
		}
	case *couchbasev2.CouchbaseBackup:
		if t2, ok := updated.(*couchbasev2.CouchbaseBackup); ok {
			return validationv2.CheckImmutableFieldsBackup(t, t2)
		}
	case *couchbasev2.CouchbaseAutoscaler:
		if t2, ok := updated.(*couchbasev2.CouchbaseAutoscaler); ok {
			return validationv2.CheckImmutableFieldsAutoscaler(t, t2)
		}
	}

	return nil
}
