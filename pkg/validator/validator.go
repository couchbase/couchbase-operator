package validator

import (
	couchbasev1 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbase/couchbase-operator/pkg/validator/types"
	validationv1 "github.com/couchbase/couchbase-operator/pkg/validator/v1"
	validationv2 "github.com/couchbase/couchbase-operator/pkg/validator/v2"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
)

func New(client kubernetes.Interface, couchbaseClient versioned.Interface) *types.Validator {
	return types.New(client, couchbaseClient)
}

func ApplyDefaults(resource runtime.Object) {
	switch t := resource.(type) {
	case *couchbasev1.CouchbaseCluster:
		validationv1.ApplyDefaults(t)
	case *couchbasev2.CouchbaseCluster:
		validationv2.ApplyDefaults(t)
	case *couchbasev2.CouchbaseBucket:
		validationv2.ApplyBucketDefaults(t)
	case *couchbasev2.CouchbaseEphemeralBucket:
		validationv2.ApplyEphemeralBucketDefaults(t)
	case *couchbasev2.CouchbaseMemcachedBucket:
		validationv2.ApplyMemcachedBucketDefaults(t)
	}
}

func CheckConstraints(v *types.Validator, resource runtime.Object) error {
	switch t := resource.(type) {
	case *couchbasev1.CouchbaseCluster:
		return validationv1.CheckConstraints(v, t)
	case *couchbasev2.CouchbaseCluster:
		return validationv2.CheckConstraints(v, t)
	case *couchbasev2.CouchbaseBucket:
		return validationv2.CheckConstraintsBucket(v, t)
	case *couchbasev2.CouchbaseEphemeralBucket:
		return validationv2.CheckConstraintsEphemeralBucket(v, t)
	case *couchbasev2.CouchbaseMemcachedBucket:
		return validationv2.CheckConstraintsMemcachedBucket(v, t)
	}
	return nil
}

func CheckImmutableFields(current, updated runtime.Object) error {
	switch t := current.(type) {
	case *couchbasev1.CouchbaseCluster:
		switch t2 := updated.(type) {
		case *couchbasev1.CouchbaseCluster:
			return validationv1.CheckImmutableFields(t, t2)
		}
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
	}
	return nil
}
