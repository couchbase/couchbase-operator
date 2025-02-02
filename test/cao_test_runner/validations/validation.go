package validations

import (
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
	admissioncontrollervalidator "github.com/couchbase/couchbase-operator/test/cao_test_runner/validations/admission_controller"
	caocrdvalidator "github.com/couchbase/couchbase-operator/test/cao_test_runner/validations/cao_crd"
	destroyadmissioncontrollervalidator "github.com/couchbase/couchbase-operator/test/cao_test_runner/validations/destroy/admission_controller"
	destroyk8sclustervalidator "github.com/couchbase/couchbase-operator/test/cao_test_runner/validations/destroy/k8s_cluster"
	destroyoperatorvalidator "github.com/couchbase/couchbase-operator/test/cao_test_runner/validations/destroy/operator"
	k8sclustervalidator "github.com/couchbase/couchbase-operator/test/cao_test_runner/validations/k8s_cluster"
	namespacevalidator "github.com/couchbase/couchbase-operator/test/cao_test_runner/validations/namespace"
	oldvalidators "github.com/couchbase/couchbase-operator/test/cao_test_runner/validations/old_validators"
	operatorvalidator "github.com/couchbase/couchbase-operator/test/cao_test_runner/validations/operator"
)

const (
	Pre  = "PRE"
	Post = "POST"
)

type Validator interface {
	GetState() string
	Run(ctx *context.Context, testAssets assets.TestAssetGetterSetter) error
}

func RegisterValidators() map[string]Validator {
	return map[string]Validator{
		"couchbaseReadiness":                &oldvalidators.CouchbaseReadiness{},
		"collectLogs":                       &oldvalidators.CollectLogs{},
		"preFlight":                         &oldvalidators.PreFlight{},
		"couchbaseVersion":                  &oldvalidators.CBVersion{},
		"couchbaseClusterSize":              &oldvalidators.CouchbaseClusterSize{},
		"kubeconfigContext":                 &oldvalidators.KubeConfigValidator{},
		"KubernetesClusterValidator":        &k8sclustervalidator.KubernetesClusterValidator{},
		"DestroyKubernetesClusterValidator": &destroyk8sclustervalidator.KubernetesClusterValidator{},
		"NamespaceValidator":                &namespacevalidator.NamespaceValidator{},
		"CAOCRDValidator":                   &caocrdvalidator.CAOCRDValidator{},
		"OperatorValidator":                 &operatorvalidator.OperatorValidator{},
		"AdmissionControllerValidator":      &admissioncontrollervalidator.AdmissionControllerValidator{},
		"DestroyOperatorValidator":          &destroyoperatorvalidator.DestroyOperatorValidator{},
		"DestroyAdmissionValidator":         &destroyadmissioncontrollervalidator.DestroyAdmissionControllerValidator{},
	}
}
