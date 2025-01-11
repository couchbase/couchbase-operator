package nodefilter

import (
	"cmp"
	"errors"
	"fmt"
	"math/rand"
	"slices"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/nodes"
)

// NodeSortByStrategy is the strategy used to sort the nodes.
// We sort the nodes after filtering them based on conditional parameters.
// After sorting, we select the nodes based on the NodeSelectionStrategy.
type NodeSortByStrategy string

const (
	SortByDefault NodeSortByStrategy = "default"
	SortByLatest  NodeSortByStrategy = "latest"
	SortByOldest  NodeSortByStrategy = "oldest"
	SortByRandom  NodeSortByStrategy = "random"
	SortBySorted  NodeSortByStrategy = "sorted"
)

var (
	ErrNodesMapIsNil         = errors.New("nodes map is nil")
	ErrInvalidSortByStrategy = errors.New("sort strategy used is invalid")
)

// sortNodesUsingSortByStrategy finds all the nodes in the k8s cluster and then sorts them as per the NodeSortByStrategy.
func sortNodesUsingSortByStrategy(strategy NodeSortByStrategy, nodesMap map[string]*nodes.Node) ([]string, error) {
	if nodesMap == nil {
		return nil, fmt.Errorf("sort nodes `%s`: %w", strategy, ErrNodesMapIsNil)
	}

	switch strategy {
	case SortByDefault:
		return sortByDefault(nodesMap)
	case SortBySorted:
		return sortBySorted(nodesMap)
	case SortByLatest:
		return sortByLatest(nodesMap)
	case SortByOldest:
		return sortByOldest(nodesMap)
	case SortByRandom:
		return sortByRandom(nodesMap)
	default:
		return nil, fmt.Errorf("sort nodes `%s`: %w", strategy, ErrInvalidSortByStrategy)
	}
}

// sortByDefault returns the node names as the way kubectl returns them.
func sortByDefault(nodesMap map[string]*nodes.Node) ([]string, error) {
	var nodeNames []string

	for nodeName := range nodesMap {
		nodeNames = append(nodeNames, nodeName)
	}

	return nodeNames, nil
}

// sortBySorted sorts the node names in ascending order.
func sortBySorted(nodesMap map[string]*nodes.Node) ([]string, error) {
	var nodeNames []string

	for nodeName := range nodesMap {
		nodeNames = append(nodeNames, nodeName)
	}

	slices.Sort(nodeNames)

	return nodeNames, nil
}

// sortByLatest sorts the nodes as per their creation time from latest (most recently created) -> oldest.
func sortByLatest(nodesMap map[string]*nodes.Node) ([]string, error) {
	var nodesWithTimestamp [][]string
	var latestNodes []string

	// Get the nodes with their creation timestamp
	for _, node := range nodesMap {
		temp := []string{node.Metadata.Name, node.Metadata.CreationTimestamp.String()}
		nodesWithTimestamp = append(nodesWithTimestamp, temp)
	}

	// Sorting in ascending order of node names. Ensures consistent order while sorting later if timestamps are same.
	slices.SortFunc(nodesWithTimestamp, func(i, j []string) int {
		return cmp.Compare(i[0], j[0])
	})

	// Sorting in descending order of timestamp
	slices.SortFunc(nodesWithTimestamp, func(i, j []string) int {
		return cmp.Compare(j[1], i[1])
	})

	for _, nodeWithTimestamp := range nodesWithTimestamp {
		latestNodes = append(latestNodes, nodeWithTimestamp[0])
	}

	return latestNodes, nil
}

// sortByOldest sorts the nodes as per their creation time from oldest -> latest.
func sortByOldest(nodesMap map[string]*nodes.Node) ([]string, error) {
	var nodesWithTimestamp [][]string
	var oldestNodes []string

	// Get the nodes with their creation timestamp
	for _, node := range nodesMap {
		temp := []string{node.Metadata.Name, node.Metadata.CreationTimestamp.String()}
		nodesWithTimestamp = append(nodesWithTimestamp, temp)
	}

	// Sorting in ascending order of node names. Ensures consistent order while sorting later if timestamps are same.
	slices.SortFunc(nodesWithTimestamp, func(i, j []string) int {
		return cmp.Compare(i[0], j[0])
	})

	// Sorting in ascending order of timestamp
	slices.SortFunc(nodesWithTimestamp, func(i, j []string) int {
		return cmp.Compare(i[1], j[1])
	})

	for _, nodeWithTimestamp := range nodesWithTimestamp {
		oldestNodes = append(oldestNodes, nodeWithTimestamp[0])
	}

	return oldestNodes, nil
}

// sortByRandom sorts the nodes randomly.
func sortByRandom(nodesMap map[string]*nodes.Node) ([]string, error) {
	var nodeNames []string

	for nodeName := range nodesMap {
		nodeNames = append(nodeNames, nodeName)
	}

	rand.Shuffle(len(nodeNames), func(i, j int) {
		nodeNames[i], nodeNames[j] = nodeNames[j], nodeNames[i]
	})

	return nodeNames, nil
}
