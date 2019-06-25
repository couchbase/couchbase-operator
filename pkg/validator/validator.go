package validator

import (
	"fmt"

	couchbasev1 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	schemav1 "github.com/couchbase/couchbase-operator/pkg/util/k8sutil/v1"
	schemav2 "github.com/couchbase/couchbase-operator/pkg/util/k8sutil/v2"
	"github.com/couchbase/couchbase-operator/pkg/validator/types"
	validationv1 "github.com/couchbase/couchbase-operator/pkg/validator/v1"
	validationv2 "github.com/couchbase/couchbase-operator/pkg/validator/v2"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiservervalidation "k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
)

func New(client kubernetes.Interface, couchbaseClient versioned.Interface) *types.Validator {
	return types.New(client, couchbaseClient)
}

// SchemaValidate performs any schema based validation.  At present versioned
// CRD validations are only performed if CRD conversion is enabled.  As we
// don't want to cause more hassle for users we just perform it manually.
func SchemaValidate(scheme *runtime.Scheme, raw runtime.RawExtension) error {
	cr := &unstructured.Unstructured{}
	if err := cr.UnmarshalJSON(raw.Raw); err != nil {
		return err
	}

	if cr.GetKind() != "CouchbaseCluster" {
		return nil
	}

	var schema *apiextensionsv1beta1.CustomResourceValidation
	switch cr.GetAPIVersion() {
	case "couchbase.com/v1":
		schema = schemav1.GetCouchbaseClusterSchema()
	case "couchbase.com/v2":
		schema = schemav2.GetCouchbaseClusterSchema()
	default:
		return fmt.Errorf("unknown version: %s", cr.GetAPIVersion())
	}

	validation := &apiextensions.CustomResourceValidation{}
	if err := scheme.Convert(schema, validation, nil); err != nil {
		return err
	}
	validator, _, err := apiservervalidation.NewSchemaValidator(validation)
	if err != nil {
		return err
	}
	result := validator.Validate(cr)
	if !result.IsValid() {
		return result.AsError()
	}
	return nil
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
