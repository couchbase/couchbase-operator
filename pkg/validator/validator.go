package validator

import (
	"fmt"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbase/couchbase-operator/pkg/util/annotations"
	"github.com/couchbase/couchbase-operator/pkg/validator/types"
	validationv2 "github.com/couchbase/couchbase-operator/pkg/validator/v2"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
)

func GetAnnotationPopulationWarnings(resource runtime.Object) ([]string, error) {
	switch t := resource.(type) {
	case *couchbasev2.CouchbaseCluster:
		return annotations.PopulateWithWarnings(&t.Spec, t.Annotations)
	case *couchbasev2.CouchbaseBucket:
		return annotations.PopulateWithWarnings(&t.Spec, t.Annotations)
	}

	return nil, nil
}
func New(client kubernetes.Interface, couchbaseClient versioned.Interface, options *types.ValidatorOptions) *types.Validator {
	return types.New(client, couchbaseClient, options)
}

func CheckConstraints(v *types.Validator, resource runtime.Object) ([]string, error) {
	switch t := resource.(type) {
	case *couchbasev2.CouchbaseCluster:
		return validationv2.CheckConstraints(v, t)
	case *couchbasev2.CouchbaseBucket:
		return validationv2.CheckConstraintsBucket(v, t, nil)
	case *couchbasev2.CouchbaseEphemeralBucket:
		return []string{}, validationv2.CheckConstraintsEphemeralBucket(v, t, nil)
	case *couchbasev2.CouchbaseMemcachedBucket:
		return validationv2.CheckConstraintsMemcachedBucket(v, t, nil)
	case *couchbasev2.CouchbaseReplication:
		return []string{}, validationv2.CheckConstraintsReplication(v, t)
	case *couchbasev2.CouchbaseUser:
		return validationv2.CheckConstraintsCouchbaseUser(v, t)
	case *couchbasev2.CouchbaseGroup:
		return []string{}, validationv2.CheckConstraintsCouchbaseGroup(v, t)
	case *couchbasev2.CouchbaseBackup:
		return []string{}, validationv2.CheckConstraintsBackup(v, t)
	case *couchbasev2.CouchbaseBackupRestore:
		return []string{}, validationv2.CheckConstraintsBackupRestore(v, t)
	case *couchbasev2.CouchbaseCollection:
		return []string{}, validationv2.CheckConstraintsCollection(v, t)
	case *couchbasev2.CouchbaseCollectionGroup:
		return []string{}, validationv2.CheckConstraintsCollectionGroup(v, t)
	case *couchbasev2.CouchbaseScope:
		return []string{}, validationv2.CheckConstraintsScope(v, t)
	case *couchbasev2.CouchbaseScopeGroup:
		return []string{}, validationv2.CheckConstraintsScopeGroup(v, t)
	case *couchbasev2.CouchbaseEncryptionKey:
		return []string{}, validationv2.CheckConstraintsEncryptionKey(v, t)
	}

	return nil, nil
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
	case *couchbasev2.CouchbaseCollection:
		if _, ok := updated.(*couchbasev2.CouchbaseCollection); !ok {
			return fmt.Errorf("failed to update couchbase collection")
		}
	case *couchbasev2.CouchbaseCollectionGroup:
		if _, ok := updated.(*couchbasev2.CouchbaseCollectionGroup); !ok {
			return fmt.Errorf("failed to update couchbase collection group")
		}
	case *couchbasev2.CouchbaseEncryptionKey:
		if t2, ok := updated.(*couchbasev2.CouchbaseEncryptionKey); ok {
			return validationv2.CheckImmutableFieldsEncryptionKey(t, t2)
		}
	case *couchbasev2.CouchbaseUser:
		if t2, ok := updated.(*couchbasev2.CouchbaseUser); ok {
			return validationv2.CheckImmutableFieldsUser(t, t2)
		}
	}

	return nil
}

func CheckChangeConstraints(v *types.Validator, current, updated runtime.Object) (bool, []string, error) {
	switch t := current.(type) {
	case *couchbasev2.CouchbaseCluster:
		if t2, ok := updated.(*couchbasev2.CouchbaseCluster); ok {
			statusOnly, err := validationv2.CheckChangeConstraintsCluster(v, t, t2)
			return statusOnly, nil, err
		}
	case *couchbasev2.CouchbaseBucket:
		if t2, ok := updated.(*couchbasev2.CouchbaseBucket); ok {
			warnings, err := validationv2.CheckChangeConstraintsBucket(v, t, t2, nil)

			return false, warnings, err
		}
	case *couchbasev2.CouchbaseEphemeralBucket:
		if t2, ok := updated.(*couchbasev2.CouchbaseEphemeralBucket); ok {
			return false, nil, validationv2.CheckChangeConstraintsEphemeralBucket(v, t, t2, nil)
		}
	}

	return false, nil, nil
}

func CheckDeleteConstraints(v *types.Validator, resource runtime.Object) error {
	switch t := resource.(type) {
	case *couchbasev2.CouchbaseEncryptionKey:
		return validationv2.CheckDeleteConstraintsEncryptionKey(v, t)
	default:
		return nil
	}
}

func WarnOnFieldValues(resource runtime.Object) []string {
	switch t := resource.(type) {
	case *couchbasev2.CouchbaseCluster:
		return checkFieldsCouchbaseCluster(*t)
	case *couchbasev2.CouchbaseBucket:
		return checkFieldsCouchbaseBucket(*t)
	}

	return nil
}
