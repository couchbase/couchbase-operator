package nodefilter

import (
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
	managedsvc "github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
)

type NodeFilter struct {
	ManagedSvcProvider assets.ManagedServiceProvider `yaml:"managedServiceProvider" caoCli:"required"`

	// Namespace is used when filtering based on pods e.g. Operator, Admission.
	Namespace string `yaml:"namespace" caoCli:"context"`

	// Number of nodes to filter. If Count = 0 then all the nodes will be considered.
	Count int `yaml:"count" caoCli:"required"`

	SelectStrategy NodeSelectionStrategy `yaml:"selectStrategy"`
	SortByStrategy NodeSortByStrategy    `yaml:"sortByStrategy"`

	// Conditional Parameters for filtering nodes.

	// NodegroupNames stores the names of the nodegroups / nodepools which will be used to filter the nodes. If nil, all the nodegroups will be considered.
	NodegroupNames []string `yaml:"nodegroupNames"`

	// AZList holds the list of AZs to be used for filtering k8s nodes. If nil, all the AZs will be considered.
	AZList []string `yaml:"azList"`

	// SkipOperator if true, skips the k8s node with the Operator pod.
	SkipOperator bool `yaml:"skipOperator"`

	// SkipAdmission if true, skips the k8s node with the Admission pod.
	SkipAdmission bool `yaml:"skipAdmission"`

	// AvoidLabels holds the list of labels. Nodes having these labels will not be filtered. []{["LabelKey", "LabelValue"], ...}.
	AvoidLabels [][]string `yaml:"avoidLabels"`
}

var (
	NodeZoneLabelKey     = "topology.kubernetes.io/zone"
	EKSNodegroupLabelKey = "eks.amazonaws.com/nodegroup"
)

func NewNodeFilter() *NodeFilter {
	return &NodeFilter{
		SelectStrategy: SelectAny,
		SortByStrategy: SortByDefault,
		SkipOperator:   false,
		SkipAdmission:  false,
		AZList:         nil,
		AvoidLabels:    nil,
	}
}

type FilterNodesInterface interface {
	// FilterNodesUsingStrategy filters and selects the nodes and returns the node names in sorted order.
	// First we filter out the nodes based on the conditional parameters using filterNodesOnConditionalParameters().
	// Next, we sort the nodes using NodeSortByStrategy.
	// Finally, we select the nodes based on NodeSelectionStrategy strategy.
	FilterNodesUsingStrategy(nodeFilter *NodeFilter) ([]string, error)
}

// NewFilterNodesInterface returns the FilterNodesInterface for the provided managedk8sservices.ManagedServiceProvider.
func NewFilterNodesInterface(managedSvcName *assets.ManagedServiceProvider) (FilterNodesInterface, error) {
	switch managedSvcName.GetPlatform() {
	case assets.Openshift:
		return nil, fmt.Errorf("filter nodes: %w", managedsvc.ErrManagedServiceNotFound)
	case assets.Kubernetes:
		switch managedSvcName.GetEnvironment() {
		case assets.Kind:
			return ConfigNodeFilterKind(), nil
		case assets.Cloud:
			switch managedSvcName.GetProvider() {
			case assets.AWS:
				return ConfigNodeFilterEKS(), nil
			case assets.Azure:
				return nil, fmt.Errorf("filter nodes: %w", managedsvc.ErrManagedServiceNotFound)
			case assets.GCP:
				return nil, fmt.Errorf("filter nodes: %w", managedsvc.ErrManagedServiceNotFound)
			default:
				return nil, fmt.Errorf("filter nodes: %w", managedsvc.ErrManagedServiceNotFound)
			}
		default:
			return nil, fmt.Errorf("filter nodes: %w", managedsvc.ErrManagedServiceNotFound)
		}
	default:
		return nil, fmt.Errorf("filter nodes: %w", managedsvc.ErrManagedServiceNotFound)
	}
}
