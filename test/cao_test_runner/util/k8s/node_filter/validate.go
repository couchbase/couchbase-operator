package nodefilter

import (
	"errors"
	"fmt"

	managedsvc "github.com/couchbase/couchbase-operator/test/cao_test_runner/managedk8sservices"
	caoinstallutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/install_utils/cao_install_utils"
)

var (
	ErrNodeFilterIsNil  = errors.New("node filter is nil")
	ErrInvalidNodeCount = errors.New("invalid node count")
)

func ValidateNodeFilter(nf *NodeFilter) error {
	if nf == nil {
		return fmt.Errorf("validate node filter: %w", ErrNodeFilterIsNil)
	}

	if err := managedsvc.ValidateManagedServices(&nf.ManagedSvcProvider); err != nil {
		return fmt.Errorf("validate node filter: %w", err)
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
	switch nf.ManagedSvcProvider.Platform {
	case caoinstallutils.Kubernetes:
		switch nf.ManagedSvcProvider.Environment {
		case managedsvc.Kind:
			switch nf.SelectStrategy {
			case SelectAny:
				return nil
			}
		case managedsvc.Cloud:
			switch nf.ManagedSvcProvider.Provider {
			case managedsvc.AWS:
				switch nf.SelectStrategy {
				case SelectAny, SelectRoundRobinAZ:
					return nil
				}
			}
		}
	}

	return fmt.Errorf("validate selection strategy %s for %s %s: %w", nf.SelectStrategy, nf.ManagedSvcProvider.Platform, nf.ManagedSvcProvider.Environment, ErrInvalidSelectStrategy)
}
