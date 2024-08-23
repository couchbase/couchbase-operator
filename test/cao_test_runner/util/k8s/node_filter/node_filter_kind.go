package nodefilter

import (
	"errors"
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/nodes"
)

var (
	ErrInsufficientNodes = errors.New("insufficient number of nodes which match the selection strategy")
)

type NodeFilterKind struct {
}

func ConfigNodeFilterKind() *NodeFilterKind {
	return &NodeFilterKind{}
}

// SortNodesUsingSortByStrategy sorts the nodes based on NodeSortByStrategy.
func (n *NodeFilterKind) SortNodesUsingSortByStrategy(nodeFilter *NodeFilter) ([]string, error) {
	nodeNames, err := sortNodesUsingSortByStrategy(nodeFilter.SortByStrategy)
	if err != nil {
		return nil, fmt.Errorf("sort nodes `kind`: %w", err)
	}

	return nodeNames, nil
}

// FilterOutNodes first runs SortNodesUsingSortByStrategy() and then filters out nodes based on
// NodeFilter.SkipOperator, NodeFilter.SkipAdmission and NodeFilter.AvoidLabels.
func (n *NodeFilterKind) FilterOutNodes(nodeFilter *NodeFilter) ([]string, error) {
	nodeNames, err := sortNodesUsingSortByStrategy(nodeFilter.SortByStrategy)
	if err != nil {
		return nil, fmt.Errorf("filter out nodes `kind: %w", err)
	}

	operatorNodeName, admissionNodeName, err := GetOperatorAdmissionNodeNames("default")
	if err != nil {
		return nil, fmt.Errorf("filter out nodes `kind: %w", err)
	}

	for i := range nodeNames {
		if nodeFilter.SkipOperator && nodeNames[i] == operatorNodeName {
			nodeNames[i] = ""
			continue
		}

		if nodeFilter.SkipAdmission && nodeNames[i] == admissionNodeName {
			nodeNames[i] = ""
			continue
		}

		// If one of the labels in AvoidLabels is present on the node then we reject the node.
		for _, avoidLabel := range nodeFilter.AvoidLabels {
			node, err := nodes.GetNode(nodeNames[i])
			if err != nil {
				return nil, fmt.Errorf("filter out nodes `kind: %w", err)
			}

			if LabelExists(node, avoidLabel[0], avoidLabel[1]) {
				nodeNames[i] = ""
				break
			}
		}
	}

	nodeNames = RemoveEmptyFromSlice(nodeNames)

	return nodeNames, nil
}

// FilterNodesUsingStrategy first runs FilterOutNodes() and then selects nodes based on NodeSelectionStrategy strategy.
func (n *NodeFilterKind) FilterNodesUsingStrategy(nodeFilter *NodeFilter) ([]string, error) {
	nodeNames, err := n.FilterOutNodes(nodeFilter)
	if err != nil {
		return nil, fmt.Errorf("filter nodes `kind`: %w", err)
	}

	switch nodeFilter.SelectStrategy {
	case SelectAny:
		return kindFilterAny(nodeFilter, nodeNames)
	default:
		return nil, fmt.Errorf("filter out nodes `kind` using `%s`: %w", nodeFilter.SelectStrategy, ErrInvalidSelectStrategy)
	}
}

// filterAny filters the first NodeFilter.Count number of nodes from the slice of nodeNames.
func kindFilterAny(nodeFilter *NodeFilter, nodeNames []string) ([]string, error) {
	if len(nodeNames) < nodeFilter.Count {
		return nil, fmt.Errorf("filter any with count=%d and filtered nodes=%d: %w", nodeFilter.Count, len(nodeNames), ErrInsufficientNodes)
	}

	return nodeNames[0:nodeFilter.Count], nil
}

func (n *NodeFilterKind) FilterRoundRobinAZ(nodeFilter *NodeFilter) ([]string, error) {
	return nil, fmt.Errorf("filter out nodes `kind` using `%s`: %w", nodeFilter.SelectStrategy, ErrInvalidSelectStrategy)
}
