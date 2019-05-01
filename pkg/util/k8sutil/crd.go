package k8sutil

import (
	"context"
	"fmt"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	validationv1 "github.com/couchbase/couchbase-operator/pkg/util/k8sutil/v1"
	validationv2 "github.com/couchbase/couchbase-operator/pkg/util/k8sutil/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"

	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiservervalidation "k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/scheme"
)

// CouchbaseClusterCRUpdateFunc is a function to be used when atomically
// updating a Cluster CR.
type CouchbaseClusterCRUpdateFunc func(*couchbasev2.CouchbaseCluster)

func CreateCRD(clientset apiextensionsclient.Interface, version constants.KubernetesVersion) error {
	crd := createCRD(version)
	_, err := clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Create(crd)
	return err
}

func GetCRD() *apiextensionsv1beta1.CustomResourceDefinition {
	return createCRD(constants.KubernetesVersionMax)
}

func createCRD(version constants.KubernetesVersion) *apiextensionsv1beta1.CustomResourceDefinition {
	return &apiextensionsv1beta1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			APIVersion: apiextensionsv1beta1.SchemeGroupVersion.String(),
			Kind:       "CustomResourceDefinition",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: couchbasev2.ClusterCRDName,
		},
		Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
			Group: couchbasev2.SchemeGroupVersion.Group,
			Scope: apiextensionsv1beta1.NamespaceScoped,
			Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
				Plural:     couchbasev2.ClusterCRDResourcePlural,
				Kind:       couchbasev2.ClusterCRDResourceKind,
				ShortNames: []string{"couchbase", "cbc"},
			},
			Versions: []apiextensionsv1beta1.CustomResourceDefinitionVersion{
				{
					Name:    "v2",
					Served:  true,
					Storage: true,
					Schema:  validationv2.GetCouchbaseClusterSchema(),
				},
				{
					Name:   "v1",
					Served: true,
					Schema: validationv1.GetCouchbaseClusterSchema(),
				},
			},
		},
	}
}

func WaitCRDReady(clientset apiextensionsclient.Interface) error {
	err := retryutil.Retry(context.Background(), 5*time.Second, 20, func() (bool, error) {
		crd, err := clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Get(couchbasev2.ClusterCRDName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, cond := range crd.Status.Conditions {
			switch cond.Type {
			case apiextensionsv1beta1.Established:
				if cond.Status == apiextensionsv1beta1.ConditionTrue {
					return true, nil
				}
			case apiextensionsv1beta1.NamesAccepted:
				if cond.Status == apiextensionsv1beta1.ConditionFalse {
					return false, fmt.Errorf("name conflict: %v", cond.Reason)
				}
			}
		}
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("wait CRD created failed: %v", err)
	}
	return nil
}

func MustNewKubeExtClient() apiextensionsclient.Interface {
	cfg, err := InClusterConfig()
	if err != nil {
		panic(err)
	}
	return apiextensionsclient.NewForConfigOrDie(cfg)
}

func ValidateCRD(customResource *unstructured.Unstructured) error {
	validation := apiextensions.CustomResourceValidation{}
	err := scheme.Scheme.Convert(validationv2.GetCouchbaseClusterSchema(), &validation, nil)
	if err != nil {
		return err
	}

	validator, _, err := apiservervalidation.NewSchemaValidator(&validation)
	if err != nil {
		return fmt.Errorf("error creating schema validator : %v", err)
	}

	result := validator.Validate(customResource)

	if !result.IsValid() {
		return result.AsError()
	}

	return nil
}
