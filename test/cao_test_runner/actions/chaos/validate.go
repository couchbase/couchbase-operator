package chaos

import (
	"errors"
	"fmt"

	cbpodfilter "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/cb_pod_filter"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/triggers"
)

var (
	ErrInsufficientTriggers       = errors.New("insufficient Triggers provided, doesn't match CBPodFilter.Count")
	ErrInsufficientChaosActions   = errors.New("insufficient ChaosActions provided, doesn't match CBPodFilter.Count")
	ErrInsufficientCBSvcChaos     = errors.New("insufficient CBServiceChaos' provided, doesn't match CBPodFilter.Count")
	ErrChaosActionNotFound        = errors.New("provided chaos action not found")
	ErrInsufficientCBClusterChaos = errors.New("insufficient CBClusterChaos' provided, doesn't match CBPodFilter.Count")
)

// validateChaosList validates the fields of ChaosList.
func validateChaosList(chaosList *ChaosList) error {
	err := cbpodfilter.ValidateCBPodFilter(&chaosList.CBPodFilter)
	if err != nil {
		return fmt.Errorf("validate chaos list: %w", err)
	}

	cbPodsCount := chaosList.CBPodFilter.Count

	if chaosList.ChaosIterations <= 0 {
		chaosList.ChaosIterations = 1
	}

	// Validating ChaosList.Trigger.
	if len(chaosList.Trigger) != cbPodsCount {
		return fmt.Errorf("validate chaos list`: %w", ErrInsufficientTriggers)
	}

	for i := range chaosList.Trigger {
		if err := triggers.ValidateTriggerConfig(&chaosList.Trigger[i]); err != nil {
			return fmt.Errorf("validate chaos list: %w", err)
		}
	}

	// Validating ChaosList.ChaosActions.
	if len(chaosList.ChaosActions) != cbPodsCount {
		return fmt.Errorf("validate chaos list: %w", ErrInsufficientChaosActions)
	}

	for _, action := range chaosList.ChaosActions {
		if err := validateChaosAction(action); err != nil {
			return fmt.Errorf("validate chaos list: %w", err)
		}
	}

	// Validating CBServiceChaos. If not provided, we just create a slice to avoid panic for accessing nil slice.
	if chaosList.CBServiceChaos != nil {
		if len(chaosList.CBServiceChaos) != cbPodsCount {
			return fmt.Errorf("validate chaos list: %w", ErrInsufficientCBSvcChaos)
		}

		for i := range chaosList.CBServiceChaos {
			err := validateCBServiceChaos(&chaosList.CBServiceChaos[i])
			if err != nil {
				return fmt.Errorf("validate chaos list: %w", err)
			}
		}
	} else {
		for range cbPodsCount {
			chaosList.CBServiceChaos = append(chaosList.CBServiceChaos, CBService{})
		}
	}

	// Validating CBClusterChaos. If not provided, we just create a slice to avoid panic for accessing nil slice.
	if chaosList.CBClusterChaos != nil {
		if len(chaosList.CBClusterChaos) != cbPodsCount {
			return fmt.Errorf("validate chaos list: %w", ErrInsufficientCBClusterChaos)
		}

		for i := range chaosList.CBClusterChaos {
			err := validateCBClusterChaos(&chaosList.CBClusterChaos[i])
			if err != nil {
				return fmt.Errorf("validate chaos list: %w", err)
			}
		}
	} else {
		for range cbPodsCount {
			chaosList.CBClusterChaos = append(chaosList.CBClusterChaos, CBClusterChaosConfig{})
		}
	}

	return nil
}

func validateChaosAction(action ActionName) error {
	switch action {
	case RestartNodes, DeletePods, DeleteNodes, CBServiceChaos:
		return nil
	default:
		return fmt.Errorf("validate chaos action `%s`: %w", action, ErrChaosActionNotFound)
	}
}

func validateCBServiceChaos(cbServiceChaos *CBService) error {
	switch cbServiceChaos.ServiceChaosAction {
	case ServiceKill, ServiceKillAll, ServiceStop, ServiceRestart:
		// no-op.
	default:
		return fmt.Errorf("validate cb service chaos: %w", ErrCBServiceChaosInvalid)
	}

	for _, cbService := range cbServiceChaos.CBServices {
		_, err := MapCBServiceName(cbService)
		if err != nil {
			return fmt.Errorf("validate cb service chaos: %w", err)
		}
	}

	return nil
}

func validateCBClusterChaos(cbClusterChaos *CBClusterChaosConfig) error {
	switch cbClusterChaos.ClusterChaosAction {
	case StopRebalance:
		return nil
	default:
		return fmt.Errorf("validate cb service chaos: %w", ErrCBClusterChaosInvalid)
	}
}
