package k8sutil

import (
	"fmt"
	"time"

	api "github.com/couchbaselabs/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbaselabs/couchbase-operator/pkg/util/retryutil"

	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CouchbaseClusterCRUpdateFunc is a function to be used when atomically
// updating a Cluster CR.
type CouchbaseClusterCRUpdateFunc func(*api.CouchbaseCluster)

func CreateCRD(clientset apiextensionsclient.Interface, enableValidation bool) error {
	crd := createCRD(enableValidation)
	_, err := clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Create(crd)
	return err
}

func createCRD(enableValidation bool) *apiextensionsv1beta1.CustomResourceDefinition {
	crd := &apiextensionsv1beta1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: api.CRDName,
		},
		Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
			Group:   api.SchemeGroupVersion.Group,
			Version: api.SchemeGroupVersion.Version,
			Scope:   apiextensionsv1beta1.NamespaceScoped,
			Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
				Plural:     api.CRDResourcePlural,
				Kind:       api.CRDResourceKind,
				ShortNames: []string{"couchbase"},
			},
		},
	}

	if enableValidation {
		crd.Spec.Validation = getCustomResourceValidation()
	}

	return crd
}

func WaitCRDReady(clientset apiextensionsclient.Interface) error {
	err := retryutil.Retry(5*time.Second, 20, func() (bool, error) {
		crd, err := clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Get(api.CRDName, metav1.GetOptions{})
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
					return false, fmt.Errorf("Name conflict: %v", cond.Reason)
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
