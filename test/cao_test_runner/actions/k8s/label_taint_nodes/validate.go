package labeltaintnodes

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	nodefilter "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/node_filter"
	corev1 "k8s.io/api/core/v1"
)

var (
	ErrValueNotFound          = errors.New("value is nil or empty")
	ErrNoActionProvided       = errors.New("no apply (or remove) label (or taint) action provided")
	ErrApplyAndRemoveTogether = errors.New("both apply and remove action provided together")
	ErrInvalidLabelKey        = errors.New("invalid label key")
	ErrInvalidLabelValue      = errors.New("invalid label value")
)

func ValidateLabelTaintNodeConfig(ltn *LabelTaintNodeConfig) error {
	if err := nodefilter.ValidateNodeFilter(ltn.NodeFilter); err != nil {
		return fmt.Errorf("validate label taint node config: %w", err)
	}

	if !(ltn.ApplyLabel || ltn.RemoveLabel || ltn.ApplyTaint || ltn.RemoveTaint) {
		return fmt.Errorf("validate label taint node config: %w", ErrNoActionProvided)
	}

	if ltn.ApplyLabel && ltn.RemoveLabel {
		return fmt.Errorf("validate label node config: %w", ErrApplyAndRemoveTogether)
	}

	if ltn.ApplyTaint && ltn.RemoveTaint {
		return fmt.Errorf("validate taint node config: %w", ErrApplyAndRemoveTogether)
	}

	if ltn.ApplyLabel || ltn.RemoveLabel {
		for _, label := range ltn.Labels {
			if err := validateLabel(label.LabelKey, label.LabelValue); err != nil {
				return fmt.Errorf("validate label: %w", err)
			}
		}
	}

	if ltn.ApplyTaint || ltn.RemoveTaint {
		for _, taint := range ltn.Taints {
			if err := validateLabel(taint.TaintKey, taint.TaintValue); err != nil {
				return fmt.Errorf("validate taint: %w", err)
			}

			switch taint.TaintEffect {
			case corev1.TaintEffectNoSchedule, corev1.TaintEffectNoExecute, corev1.TaintEffectPreferNoSchedule:
				break
			default:
				return fmt.Errorf("validate taint effect: %w", ErrValueNotFound)
			}
		}
	}

	return nil
}

// validateLabel validates the label key and value. Can be used for taints as well.
/*
 * https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#syntax-and-character-set.
 */
func validateLabel(labelKey, labelValue string) error {
	if err := validateLabelKey(labelKey); err != nil {
		return fmt.Errorf("validate label: %w", err)
	}
	if err := validateLabelValue(labelValue); err != nil {
		return fmt.Errorf("validate label: %w", err)
	}
	return nil
}

func validateLabelKey(labelKey string) error {
	labelKeyParts := strings.Split(labelKey, "/")
	if len(labelKeyParts) > 2 {
		return ErrInvalidLabelKey
	}

	if len(labelKeyParts) == 2 {
		if len(labelKeyParts[0]) > 253 {
			return ErrInvalidLabelKey
		}
	}

	name := labelKeyParts[len(labelKeyParts)-1]
	labelKeyRegex := regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9\-_.]{0,61}[a-zA-Z0-9])?$`)

	if len(name) > 63 || !labelKeyRegex.MatchString(name) {
		return ErrInvalidLabelKey
	}

	return nil
}

func validateLabelValue(labelValue string) error {
	labelValueRegex := regexp.MustCompile(`^[a-zA-Z0-9]?([a-zA-Z0-9\-_.]{0,61}[a-zA-Z0-9])?$`)

	if len(labelValue) > 63 || !labelValueRegex.MatchString(labelValue) {
		return ErrInvalidLabelValue
	}

	return nil
}
