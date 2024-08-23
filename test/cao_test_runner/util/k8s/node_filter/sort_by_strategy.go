package nodefilter

import (
	"cmp"
	"fmt"
	"math/rand"
	"slices"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/nodes"
)

type NodeSortByStrategy string

const (
	SortByDefault NodeSortByStrategy = "default"
	SortByLatest  NodeSortByStrategy = "latest"
	SortByOldest  NodeSortByStrategy = "oldest"
	SortByRandom  NodeSortByStrategy = "random"
	SortBySorted  NodeSortByStrategy = "sorted"
)

// sortNodesUsingSortByStrategy finds all the nodes in the k8s cluster and then sorts them as per the NodeSortByStrategy.
func sortNodesUsingSortByStrategy(strategy NodeSortByStrategy) ([]string, error) {
	switch strategy {
	case SortByDefault:
		return sortByDefault()
	case SortBySorted:
		return sortBySorted()
	case SortByLatest:
		return sortByLatest()
	case SortByOldest:
		return sortByOldest()
	case SortByRandom:
		return sortByRandom()
	default:
		return nil, fmt.Errorf("sort nodes `%s`: %w", strategy, ErrInvalidSortByStrategy)
	}
}

// sortByDefault returns the node names as the way kubectl returns them.
func sortByDefault() ([]string, error) {
	return nodes.GetNodeNames()
}

// sortBySorted sorts the node names in ascending order.
func sortBySorted() ([]string, error) {
	nodeNames, err := nodes.GetNodeNames()
	if err != nil {
		return nil, fmt.Errorf("sort nodes by `%s`: %w", SortBySorted, err)
	}

	slices.Sort(nodeNames)

	return nodeNames, nil
}

// sortByLatest sorts the nodes as per their creation time from latest (most recently created) -> oldest.
func sortByLatest() ([]string, error) {
	var latestNodes []string

	nodesWithTimestamp, err := GetNodesWithTimestamp()
	if err != nil {
		return nil, fmt.Errorf("sort nodes by `%s`: %w", SortByLatest, err)
	}

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
func sortByOldest() ([]string, error) {
	var oldestNodes []string

	nodesWithTimestamp, err := GetNodesWithTimestamp()
	if err != nil {
		return nil, fmt.Errorf("sort nodes by `%s`: %w", SortByOldest, err)
	}

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
func sortByRandom() ([]string, error) {
	nodeNames, err := nodes.GetNodeNames()
	if err != nil {
		return nil, fmt.Errorf("sort nodes by `%s`: %w", SortByRandom, err)
	}

	rand.Shuffle(len(nodeNames), func(i, j int) {
		nodeNames[i], nodeNames[j] = nodeNames[j], nodeNames[i]
	})

	return nodeNames, nil
}
