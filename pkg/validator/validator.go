package validator

import (
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/validator/types"
	validationv2 "github.com/couchbase/couchbase-operator/pkg/validator/v2"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
)

func New(client kubernetes.Interface, couchbaseClient versioned.Interface) *types.Validator {
	return types.New(client, couchbaseClient)
}

func ApplyDefaults(v *types.Validator, object *unstructured.Unstructured) jsonpatch.PatchList {
	switch object.GetAPIVersion() {
	case couchbasev2.GroupName + "/v2":
		switch object.GetKind() {
		case couchbasev2.ClusterCRDResourceKind:
			return validationv2.ApplyDefaults(v, object)
		case couchbasev2.BucketCRDResourceKind:
			return validationv2.ApplyBucketDefaults(v, object)
		case couchbasev2.EphemeralBucketCRDResourceKind:
			return validationv2.ApplyEphemeralBucketDefaults(v, object)
		case couchbasev2.MemcachedBucketCRDResourceKind:
			return validationv2.ApplyMemcachedBucketDefaults(v, object)
		case couchbasev2.ReplicationCRDResourceKind:
			return validationv2.ApplyReplicationDefaults(v, object)
		case couchbasev2.RoleCRDResourceKind:
			return validationv2.ApplyRoleDefaults(v, object)
		}
	}
	return nil
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
	case *couchbasev2.CouchbaseRole:
		return validationv2.CheckConstraintsCouchbaseRole(v, t)
	}
	return nil
}

func CheckImmutableFields(current, updated runtime.Object) error {
	switch t := current.(type) {
	case *couchbasev2.CouchbaseCluster:
		switch t2 := updated.(type) {
		case *couchbasev2.CouchbaseCluster:
			return validationv2.CheckImmutableFields(t, t2)
		}
	case *couchbasev2.CouchbaseBucket:
		switch t2 := updated.(type) {
		case *couchbasev2.CouchbaseBucket:
			return validationv2.CheckImmutableFieldsBucket(t, t2)
		}
	case *couchbasev2.CouchbaseEphemeralBucket:
		switch t2 := updated.(type) {
		case *couchbasev2.CouchbaseEphemeralBucket:
			return validationv2.CheckImmutableFieldsEphemeralBucket(t, t2)
		}
	case *couchbasev2.CouchbaseMemcachedBucket:
		switch t2 := updated.(type) {
		case *couchbasev2.CouchbaseMemcachedBucket:
			return validationv2.CheckImmutableFieldsMemcachedBucket(t, t2)
		}
	case *couchbasev2.CouchbaseReplication:
		switch t2 := updated.(type) {
		case *couchbasev2.CouchbaseReplication:
			return validationv2.CheckImmutableFieldsReplication(t, t2)
		}
	}
	return nil
}
