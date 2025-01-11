package nodefilter

import (
	"errors"
)

// NodeSelectionStrategy determines the way to choose the nodes from a list of nodes.
// Prior to Node selection, the list of nodes is sorted using NodeSortByStrategy.
type NodeSelectionStrategy string

const (
	SelectAny          NodeSelectionStrategy = "any"
	SelectRoundRobinAZ NodeSelectionStrategy = "roundRobinAZ"
)

var (
	ErrInvalidSelectStrategy = errors.New("node selection strategy invalid")
)
