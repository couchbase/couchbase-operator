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
	// NodeMap stores the information of the nodes. Maps node names to nodes.Node.
	NodeMap map[string]*nodes.Node
	// NodeNames stores the node names in sorted order (sorted using NodeSortByStrategy).
	// NodeNames slice is consistent with the NodeMap map.
	NodeNames []string
}

func ConfigNodeFilterKind() *NodeFilterKind {
	return &NodeFilterKind{
		NodeMap:   make(map[string]*nodes.Node),
		NodeNames: make([]string, 0),
	}
}

// FilterNodesUsingStrategy filters and selects the nodes and returns the node names in sorted order.
/*
 * First we filter out the nodes based on the conditional parameters using filterNodesOnConditionalParameters().
 * Next, we sort the nodes using NodeSortByStrategy.
 * Finally, we select the nodes based on NodeSelectionStrategy strategy.
 */
func (n *NodeFilterKind) FilterNodesUsingStrategy(nodeFilter *NodeFilter) ([]string, error) {
	var err error

	n.NodeMap, err = nodes.GetNodesMap(nil)
	if err != nil {
		return nil, fmt.Errorf("filter nodes `eks`: %w", err)
	}

	err = filterNodesOnConditionalParameters(nodeFilter, n.NodeMap)
	if err != nil {
		return nil, fmt.Errorf("filter nodes `kind`: %w", err)
	}

	n.NodeNames, err = sortNodesUsingSortByStrategy(nodeFilter.SortByStrategy, n.NodeMap)
	if err != nil {
		return nil, fmt.Errorf("filter nodes `kind: %w", err)
	}

	if len(n.NodeNames) < nodeFilter.Count {
		return nil, fmt.Errorf("filter nodes `kind` with count=%d and filtered nodes=%d: %w", nodeFilter.Count, len(n.NodeNames), ErrInsufficientNodes)
	}

	switch nodeFilter.SelectStrategy {
	case SelectAny:
		return n.kindFilterAny(nodeFilter)
	default:
		return nil, fmt.Errorf("filter nodes `kind` using `%s`: %w", nodeFilter.SelectStrategy, ErrInvalidSelectStrategy)
	}
}

// filterAny filters the first NodeFilter.Count number of nodes from the slice of nodeNames.
func (n *NodeFilterKind) kindFilterAny(nodeFilter *NodeFilter) ([]string, error) {
	return n.NodeNames[0:nodeFilter.Count], nil
}
