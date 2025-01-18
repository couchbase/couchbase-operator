package nodefilter

import (
	"errors"
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
)

var (
	ErrNodeFilterIsNil  = errors.New("node filter is nil")
	ErrInvalidNodeCount = errors.New("invalid node count")
)

func ValidateNodeFilter(nf *NodeFilter) error {
	if nf == nil {
		return fmt.Errorf("validate node filter: %w", ErrNodeFilterIsNil)
	}

	if nf.Count < 0 {
		return fmt.Errorf("validate node filter: %w", ErrInvalidNodeCount)
	}

	if err := validateSortByStrategy(nf.SortByStrategy); err != nil {
		return fmt.Errorf("validate node filter: %w", err)
	}

	if err := validateSelectionStrategy(nf); err != nil {
		return fmt.Errorf("validate node filter: %w", err)
	}

	return nil
}

func validateSortByStrategy(strategy NodeSortByStrategy) error {
	switch strategy {
	case SortByDefault, SortBySorted, SortByLatest, SortByOldest, SortByRandom:
		return nil
	default:
		return fmt.Errorf("validate sort by strategy `%s`: %w", strategy, ErrInvalidSortByStrategy)
	}
}

func validateSelectionStrategy(nf *NodeFilter) error {
	switch nf.ManagedSvcProvider.GetPlatform() {
	case assets.Kubernetes:
		switch nf.ManagedSvcProvider.GetEnvironment() {
		case assets.Kind:
			switch nf.SelectStrategy {
			case SelectAny:
				return nil
			}
		case assets.Cloud:
			switch nf.ManagedSvcProvider.GetProvider() {
			case assets.AWS:
				switch nf.SelectStrategy {
				case SelectAny, SelectRoundRobinAZ:
					return nil
				}
			}
		}
	}

	return fmt.Errorf("validate selection strategy %s for %s %s: %w", nf.SelectStrategy,
		nf.ManagedSvcProvider.GetPlatform(), nf.ManagedSvcProvider.GetEnvironment(), ErrInvalidSelectStrategy)
}
