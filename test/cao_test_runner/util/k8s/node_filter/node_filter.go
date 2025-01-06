package nodefilter

import (
	"errors"
	"fmt"

	managedsvc "github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
	caoinstallutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/install_utils/cao_install_utils"
)

type NodeFilter struct {
	ManagedSvcName managedsvc.ManagedServiceProvider `yaml:"managedServiceProvider" caoCli:"required"`

	// Number of nodes to filter.
	Count int `yaml:"count" caoCli:"required"`

	SelectStrategy NodeSelectionStrategy `yaml:"selectStrategy"`
	SortByStrategy NodeSortByStrategy    `yaml:"sortByStrategy"`

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
	ErrInvalidSortByStrategy = errors.New("sort strategy used is invalid")
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
	// SortNodesUsingSortByStrategy sorts the nodes based on NodeSortByStrategy.
	SortNodesUsingSortByStrategy(nodeFilter *NodeFilter) ([]string, error)

	// FilterOutNodes first runs SortNodesUsingSortByStrategy() and then filters out nodes based on
	// NodeFilter.SkipOperator, NodeFilter.SkipAdmission and NodeFilter.AvoidLabels.
	FilterOutNodes(nodeFilter *NodeFilter) ([]string, error)

	// FilterNodesUsingStrategy first runs FilterOutNodes() and then selects nodes based on NodeSelectionStrategy strategy.
	FilterNodesUsingStrategy(nodeFilter *NodeFilter) ([]string, error)
}

// NewFilterNodesInterface returns the FilterNodesInterface for the provided managedk8sservices.ManagedServiceProvider.
func NewFilterNodesInterface(managedSvcName *managedsvc.ManagedServiceProvider) (FilterNodesInterface, error) {
	switch managedSvcName.Platform {
	case caoinstallutils.Openshift:
		return nil, fmt.Errorf("filter nodes: %w", managedsvc.ErrManagedServiceNotFound)
	case caoinstallutils.Kubernetes:
		switch managedSvcName.Environment {
		case managedsvc.Kind:
			return ConfigNodeFilterKind(), nil
		case managedsvc.Cloud:
			switch managedSvcName.Provider {
			case managedsvc.AWS:
				return ConfigNodeFilterEKS(), nil
			case managedsvc.Azure:
				return nil, fmt.Errorf("filter nodes: %w", managedsvc.ErrManagedServiceNotFound)
			case managedsvc.GCP:
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
