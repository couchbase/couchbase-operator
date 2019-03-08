package resource

import (
	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/util"
	"k8s.io/api/policy/v1beta1"

	"github.com/ghodss/yaml"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// podDisruptionBudgetResource represents a collection of pod disruption budgets
type podDisruptionBudgetResource struct {
	context *context.Context
	// pdbs is the raw output from listing pod disruption budgets
	pdbs *v1beta1.PodDisruptionBudgetList
}

// NewPodDisruptionBudgetResource initializes a new service resource
func NewPodDisruptionBudgetResource(context *context.Context) Resource {
	return &podDisruptionBudgetResource{
		context: context,
	}
}

func (r *podDisruptionBudgetResource) Kind() string {
	return "PodDisruptionBudget"
}

// Fetch collects all pod disruption budgets as defined by the configuration
func (r *podDisruptionBudgetResource) Fetch() error {
	var err error
	selector, err := GetResourceSelector(&r.context.Config)
	if err != nil {
		return err
	}
	r.pdbs, err = r.context.KubeClient.PolicyV1beta1().PodDisruptionBudgets(r.context.Config.Namespace).List(metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return err
	}
	return nil
}

func (r *podDisruptionBudgetResource) Write(b backend.Backend) error {
	for _, pdb := range r.pdbs.Items {
		data, err := yaml.Marshal(pdb)
		if err != nil {
			return err
		}

		_ = b.WriteFile(util.ArchivePath(r.context.Config.Namespace, r.Kind(), pdb.Name, pdb.Name+".yaml"), string(data))
	}
	return nil
}

func (r *podDisruptionBudgetResource) References() []ResourceReference {
	return []ResourceReference{}
}
