package chaosbaremetal

import (
	"errors"
	"fmt"

	cbbaremetalfilter "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cb_bare_metal_filter"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/triggers"
)

var (
	ErrInsufficientTriggers     = errors.New("insufficient Triggers provided, doesn't match CBBareMetalFilter.Count")
	ErrInsufficientChaosActions = errors.New("insufficient ChaosActions provided, doesn't match CBBareMetalFilter.Count")
	ErrInsufficientCBSvcChaos   = errors.New("insufficient CBServiceChaos' provided, doesn't match CBBareMetalFilter.Count")
	ErrChaosActionNotFound      = errors.New("provided chaos action not found")
)

// validateChaosList validates the fields of ChaosList.
func validateChaosList(chaosList *ChaosList) error {
	err := cbbaremetalfilter.ValidateCBNodeFilter(&chaosList.CBBareMetalFilter)
	if err != nil {
		return fmt.Errorf("validate chaos list: %w", err)
	}

	cbNodesCount := chaosList.CBBareMetalFilter.Count

	if chaosList.ChaosIterations <= 0 {
		chaosList.ChaosIterations = 1
	}

	// Validating ChaosList.Trigger.
	if len(chaosList.Trigger) != cbNodesCount {
		return fmt.Errorf("validate chaos list`: %w", ErrInsufficientTriggers)
	}

	for i := range chaosList.Trigger {
		if err := triggers.ValidateTriggerConfig(&chaosList.Trigger[i]); err != nil {
			return fmt.Errorf("validate chaos list: %w", err)
		}
	}

	// Validating ChaosList.ChaosActions.
	if len(chaosList.ChaosActions) != cbNodesCount {
		return fmt.Errorf("validate chaos list: %w", ErrInsufficientChaosActions)
	}

	for _, action := range chaosList.ChaosActions {
		if err := validateBareMetalChaosAction(action); err != nil {
			return fmt.Errorf("validate chaos list: %w", err)
		}
	}

	// TODO
	// Validating CBServiceChaos. If not provided, we just create a slice to avoid panic for accessing nil slice.
	// if chaosList.CBServiceChaos != nil {
	// 	if len(chaosList.CBServiceChaos) != cbNodesCount {
	// 		return fmt.Errorf("validate chaos list: %w", ErrInsufficientCBSvcChaos)
	// 	}
	//
	// 	for i := range chaosList.CBServiceChaos {
	// 		err := chaos.ValidateCBServiceChaos(&chaosList.CBServiceChaos[i])
	// 		if err != nil {
	// 			return fmt.Errorf("validate chaos list: %w", err)
	// 		}
	// 	}
	// } else {
	// 	for range cbNodesCount {
	// 		chaosList.CBServiceChaos = append(chaosList.CBServiceChaos, chaos.CBService{})
	// 	}
	// }

	return nil
}

func validateBareMetalChaosAction(action ActionName) error {
	switch action {
	case RestartNodes, CBServiceChaos:
		return nil
	default:
		return fmt.Errorf("validate bare metal chaos action `%s`: %w", action, ErrChaosActionNotFound)
	}
}
