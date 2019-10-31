package k8sutil

import (
	"reflect"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/client"

	policyv1beta1 "k8s.io/api/policy/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// ReconcilePDB takes a cluster and creates a PodDisruptionBudget based on cluster size.
func ReconcilePDB(client *client.Client, cluster *couchbasev2.CouchbaseCluster) error {
	name := cluster.Name + "-pdb"

	required := &policyv1beta1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: LabelsForCluster(cluster.Name),
		},
		Spec: policyv1beta1.PodDisruptionBudgetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: LabelsForCluster(cluster.Name),
			},
			MinAvailable: &intstr.IntOrString{
				IntVal: int32(cluster.Spec.TotalSize() - 1),
			},
		},
	}
	applyBaseAnnotations(required.GetObjectMeta())
	addOwnerRefToObject(required.GetObjectMeta(), cluster.AsOwner())

	// Get any existing budgets, creating one if it doesn't exist.
	actual, found := client.PodDisruptionBudgets.Get(name)
	if !found {
		_, err := client.KubeClient.PolicyV1beta1().PodDisruptionBudgets(cluster.Namespace).Create(required)
		return err
	}

	// If the requested and actual specifications are out of sync, patch the requested
	// version into the existing one and update it.
	if reflect.DeepEqual(required.Spec, actual.Spec) {
		return nil
	}

	// Delete and recreate as the spec is immutable.
	if err := client.KubeClient.PolicyV1beta1().PodDisruptionBudgets(cluster.Namespace).Delete(name, nil); err != nil {
		return err
	}
	_, err := client.KubeClient.PolicyV1beta1().PodDisruptionBudgets(cluster.Namespace).Create(required)
	return err
}
