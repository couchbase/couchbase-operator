package cbpodfilter

import (
	"errors"
	"fmt"
)

var (
	ErrCountInvalid = errors.New("count is invalid")
)

// ValidateCBPodFilter validates the basic requirements of CBPodFilter.
// Conditional validation for CBPodFilter is handled in the implementation of its methods.
func ValidateCBPodFilter(cbPodFilter *CBPodFilter) error {
	if cbPodFilter.Count <= 0 {
		return fmt.Errorf("validate cb pod filter: %w", ErrCountInvalid)
	}

	err := validateFilterType(cbPodFilter.FilterType)
	if err != nil {
		return fmt.Errorf("validate cb pod filter: %w", err)
	}

	err = validatePodSelectStrategy(cbPodFilter.SelectionStrategy)
	if err != nil {
		return fmt.Errorf("validate cb pod filter: %w", err)
	}

	return nil
}

func validateFilterType(filterType PodFilterType) error {
	switch filterType {
	case FilterAll, FilterByServices, FilterByServerNames, FilterBySvcAndServerNames:
		return nil
	default:
		return fmt.Errorf("validate filter type `%s`: %w", filterType, ErrFilterTypeInvalid)
	}
}

func validatePodSelectStrategy(strategy PodSelectStrategy) error {
	switch strategy {
	case SelectSorted, SelectOldest, SelectLatest, SelectRandom:
		return nil
	default:
		return fmt.Errorf("validate pod select strategy `%s`: %w", strategy, ErrSelectStrategyInvalid)
	}
}
