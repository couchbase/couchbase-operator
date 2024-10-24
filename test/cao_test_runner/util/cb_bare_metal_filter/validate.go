package cbbaremetalfilter

import (
	"errors"
	"fmt"
)

var (
	ErrCountInvalid       = errors.New("count is invalid")
	ErrCBHostnameNotFound = errors.New("cb hostname not found")
)

// ValidateCBNodeFilter validates the basic requirements of CBBareMetalFilter.
// Conditional validation for CBBareMetalFilter is handled in the implementation of its methods.
func ValidateCBNodeFilter(cbNodeFilter *CBBareMetalFilter) error {
	if cbNodeFilter.Count <= 0 {
		return fmt.Errorf("validate cb node filter: %w", ErrCountInvalid)
	}

	if cbNodeFilter.CBHostname == "" {
		return fmt.Errorf("validate cb node filter: %w", ErrCBHostnameNotFound)
	}

	err := validateFilterType(cbNodeFilter)
	if err != nil {
		return fmt.Errorf("validate cb node filter: %w", err)
	}

	err = validateCBNodeSelectStrategy(cbNodeFilter)
	if err != nil {
		return fmt.Errorf("validate cb node filter: %w", err)
	}

	return nil
}

func validateFilterType(cbNodeFilter *CBBareMetalFilter) error {
	switch cbNodeFilter.FilterType {
	case FilterAll, FilterByServices, FilterByServerGroups:
		return nil
	default:
		return fmt.Errorf("validate filter type `%s`: %w", cbNodeFilter.FilterType, ErrFilterTypeInvalid)
	}
}

func validateCBNodeSelectStrategy(cbNodeFilter *CBBareMetalFilter) error {
	switch cbNodeFilter.SelectionStrategy {
	case SelectSorted, SelectRandom:
		{
			return nil
		}
	default:
		return fmt.Errorf("validate cb node select strategy `%s`: %w", cbNodeFilter.SelectionStrategy, ErrSelectStrategyInvalid)
	}
}
