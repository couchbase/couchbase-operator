package cbpodfilter

import (
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/pods"
)

// GetPodsWithTimestamp returns the names of pods with their creation timestamp.
// []{[]{"pod-name", "creation-timestamp"}, ...}.
func GetPodsWithTimestamp(podNames []string, namespace string) ([][]string, error) {
	var sortPods [][]string

	podList, err := pods.GetPods(podNames, namespace)
	if err != nil {
		return nil, fmt.Errorf("get pods with timestamp: %w", err)
	}

	for _, pod := range podList.Pods {
		sortPods = append(sortPods, []string{pod.Metadata.Name, pod.Metadata.CreationTimestamp.String()})
	}

	return sortPods, nil
}
