package cbpodfilter

import (
	"cmp"
	"fmt"
	"math/rand"
	"slices"
)

type PodSelectStrategy string

const (
	// SelectSorted sorts the list of pods based on ascending order of names.
	SelectSorted PodSelectStrategy = "sorted"

	// SelectOldest sorts the list of pods from oldest -> latest creation time.
	SelectOldest PodSelectStrategy = "oldest"

	// SelectLatest sorts the list of pods from latest -> oldest creation time.
	SelectLatest PodSelectStrategy = "latest"

	// SelectRandom randomizes the list of pods.
	SelectRandom PodSelectStrategy = "random"
)

func SelectPodsUsingStrategy(strategy PodSelectStrategy, count int, podNames []string, namespace string) ([]string, error) {
	if len(podNames) < count {
		return nil, fmt.Errorf("select pods using strategy `%s`: %w", strategy, ErrLessPods)
	}

	switch strategy {
	case SelectSorted:
		return selectSortedPods(count, podNames, namespace)
	case SelectOldest:
		return selectOldestPods(count, podNames, namespace)
	case SelectLatest:
		return selectLatestPods(count, podNames, namespace)
	case SelectRandom:
		return selectRandomPods(count, podNames, namespace)
	default:
		return nil, fmt.Errorf("select pods using strategy `%s`: %w", strategy, ErrSelectStrategyInvalid)
	}
}

// selectSortedPods sorts the pods in ascending order of names and then selects required number of pods.
func selectSortedPods(count int, podNames []string, _ string) ([]string, error) {
	slices.Sort(podNames)
	return podNames[0:count], nil
}

// selectOldestPods sorts the pods from oldest -> latest creation time and then selects required number of pods.
func selectOldestPods(count int, podNames []string, namespace string) ([]string, error) {
	podsWithTimestamp, err := GetPodsWithTimestamp(podNames, namespace)
	if err != nil {
		return nil, fmt.Errorf("select oldest pods: %w", err)
	}

	// Sorting in ascending order of the timestamps
	slices.SortFunc(podsWithTimestamp, func(i, j []string) int {
		return cmp.Compare(i[1], j[1])
	})

	var oldestPods []string

	for i := 0; i < count; i++ {
		oldestPods = append(oldestPods, podsWithTimestamp[i][0])
	}

	return oldestPods, nil
}

// selectLatestPods sorts the pods from latest -> oldest creation time and then selects required number of pods.
func selectLatestPods(count int, podNames []string, namespace string) ([]string, error) {
	podsWithTimestamp, err := GetPodsWithTimestamp(podNames, namespace)
	if err != nil {
		return nil, fmt.Errorf("select latest pods: %w", err)
	}

	// Sorting in descending order of the timestamps
	slices.SortFunc(podsWithTimestamp, func(i, j []string) int {
		return cmp.Compare(j[1], i[1])
	})

	var latestPods []string

	for i := 0; i < count; i++ {
		latestPods = append(latestPods, podsWithTimestamp[i][0])
	}

	return latestPods, nil
}

// selectRandomPods randomizes the list of pods and then selects required number of pods.
func selectRandomPods(count int, podNames []string, _ string) ([]string, error) {
	rand.Shuffle(len(podNames), func(i, j int) {
		podNames[i], podNames[j] = podNames[j], podNames[i]
	})

	return podNames[0:count], nil
}
