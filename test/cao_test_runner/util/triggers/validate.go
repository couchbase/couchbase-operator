package triggers

import (
	"errors"
	"fmt"
)

var (
	ErrCBSecretNotProvided = errors.New("cb secret not provided")
)

func ValidateTriggerConfig(tc *TriggerConfig) error {
	err := validateTriggerName(tc.TriggerName)
	if err != nil {
		return err
	}

	if tc.CBSecretName == "" {
		return fmt.Errorf("validate trigger: %w", ErrCBSecretNotProvided)
	}

	return nil
}

func validateTriggerName(triggerName TriggerName) error {
	switch triggerName {
	case TriggerWait, TriggerInstant, TriggerRebalance, TriggerDeltaRecovery, TriggerRebalanceEnd:
		return nil
	case TriggerDeltaRecoveryUpgradeWarmup, TriggerDeltaRecoveryUpgrade, TriggerSwapRebalanceInUpgrade, TriggerSwapRebalanceOutUpgrade:
		return nil
	case TriggerScaling:
		return nil
	default:
		return fmt.Errorf("validate trigger name `%s`: %w", triggerName, ErrInvalidTrigger)
	}
}
