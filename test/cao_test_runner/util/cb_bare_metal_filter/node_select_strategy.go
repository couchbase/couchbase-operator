package cbbaremetalfilter

import (
	"fmt"
	"math/rand"
	"slices"
)

type CBNodeSelectStrategy string

const (
	// SelectSorted sorts the list of cb nodes based on ascending order of names.
	SelectSorted CBNodeSelectStrategy = "sorted"

	// SelectRandom randomizes the list of cb nodes.
	SelectRandom CBNodeSelectStrategy = "random"
)

func SelectCBNodesUsingStrategy(strategy CBNodeSelectStrategy, count int, cbNodeNames []string) ([]string, error) {
	if len(cbNodeNames) < count {
		return nil, fmt.Errorf("select cb nodes using strategy `%s`: %w", strategy, ErrLessCBNodes)
	}

	switch strategy {
	case SelectSorted:
		return selectSortedCBNodes(count, cbNodeNames)
	case SelectRandom:
		return selectRandomCBNodes(count, cbNodeNames)
	default:
		return nil, fmt.Errorf("select cb nodes using strategy `%s`: %w", strategy, ErrSelectStrategyInvalid)
	}
}

// selectSortedCBNodes sorts the cb nodes in ascending order of names and then selects required number of cb nodes.
func selectSortedCBNodes(count int, cbNodeNames []string) ([]string, error) {
	slices.Sort(cbNodeNames)
	return cbNodeNames[0:count], nil
}

// selectRandomCBNodes randomizes the list of cb nodes and then selects required number of cb nodes.
func selectRandomCBNodes(count int, cbNodeNames []string) ([]string, error) {
	rand.Shuffle(len(cbNodeNames), func(i, j int) {
		cbNodeNames[i], cbNodeNames[j] = cbNodeNames[j], cbNodeNames[i]
	})

	return cbNodeNames[0:count], nil
}
