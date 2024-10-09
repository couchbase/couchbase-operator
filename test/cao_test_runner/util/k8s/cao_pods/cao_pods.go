package caopods

import (
	"errors"
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/pods"
)

var (
	ErrAdmissionPodDoesntExist = errors.New("admission controller pod does not exist")
	ErrOperatorPodDoesntExist  = errors.New("operator pod does not exist")
	ErrNamespaceNotProvided    = errors.New("namespace not provided")
	ErrPodsListEmpty           = errors.New("pods list empty")
)

// GetOperatorAdmissionPodNames retrieves the k8s pod names for operator and admission pod respectively.
func GetOperatorAdmissionPodNames(namespace string) (string, []string, error) {
	if namespace == "" {
		return "", nil, fmt.Errorf("get operator admission pod names: %w", ErrNamespaceNotProvided)
	}

	podNames, err := pods.GetPodNames(namespace)
	if err != nil {
		return "", nil, fmt.Errorf("get operator admission pod names: %w", err)
	}

	var operatorPodName string
	var admissionControllerPodNames []string

	for _, podName := range podNames {
		if strings.Contains(podName, "couchbase-operator-admission") {
			admissionControllerPodNames = append(admissionControllerPodNames, podName)
			continue
		}

		if strings.Contains(podName, "couchbase-operator") {
			operatorPodName = podName
		}
	}

	if operatorPodName == "" {
		return "", nil, fmt.Errorf("get operator admission pod names: %w", ErrOperatorPodDoesntExist)
	}

	if len(admissionControllerPodNames) == 0 {
		return "", nil, fmt.Errorf("get operator admission pod names: %w", ErrAdmissionPodDoesntExist)
	}

	return operatorPodName, admissionControllerPodNames, nil
}

// GetOperatorPod retrieves kubectl pod information of the operator pod in a given namespace.
func GetOperatorPod(namespace string) (*pods.Pod, error) {
	var operatorPod *pods.Pod

	if namespace == "" {
		return nil, fmt.Errorf("get operator pod: %w", ErrNamespaceNotProvided)
	}

	operatorPodName, _, err := GetOperatorAdmissionPodNames(namespace)
	if err != nil {
		return nil, fmt.Errorf("get operator pod: %w", err)
	}

	podList, err := pods.GetPods([]string{operatorPodName}, namespace)
	if err != nil {
		return nil, fmt.Errorf("get operator pod: %w", err)
	}

	if len(podList.Pods) == 0 {
		return nil, fmt.Errorf("get operator pod: %w", ErrPodsListEmpty)
	}

	operatorPod = podList.Pods[0]

	return operatorPod, nil
}

// GetAdmissionPod retrieves kubectl pod information of the admission controller pod in a given namespace.
func GetAdmissionPods(namespace string) ([]*pods.Pod, error) {
	var admissionPods []*pods.Pod

	if namespace == "" {
		return nil, fmt.Errorf("get admission pod: %w", ErrNamespaceNotProvided)
	}

	_, admissionPodNames, err := GetOperatorAdmissionPodNames(namespace)
	if err != nil {
		return nil, fmt.Errorf("get admission pod: %w", err)
	}

	podList, err := pods.GetPods(admissionPodNames, namespace)
	if err != nil {
		return nil, fmt.Errorf("get admission pod: %w", err)
	}

	if len(podList.Pods) == 0 {
		return nil, fmt.Errorf("get admission pod: %w", ErrPodsListEmpty)
	}

	admissionPods = podList.Pods

	return admissionPods, nil
}
