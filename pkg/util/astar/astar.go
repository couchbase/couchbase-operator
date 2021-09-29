// package astar implements the A* shortest path algorithm.
// This is useful for exhaustive graph searches that pick the optimal solution
// for going from an initial to a final state.
// This doesn't implement the full algorithm at present, it makes assumptions
// that each transition in the graph is of equal cost.
package astar

import (
	"errors"
	"fmt"

	cberrors "github.com/couchbase/couchbase-operator/pkg/errors"
)

// ErrUnsolvable is raised when the problem submitted was unsolvable for some reason.
var ErrUnsolvable = errors.New("problem is unsolvable")

// AStarable is an interface a problem must implment to perform an A* algorthm.
type AStarable interface {
	// Done returns true when the problem is solved.
	Done() bool

	// Hash returns a unique scalar representation of the problem state.
	Hash() string

	// Move returns a list of all valid states that can be reached from the
	// current one.
	Move() []AStarable
}

// queue is a simple FIFO queue.
type queue struct {
	queue []AStarable
}

// newQueue creates a new queue with an initial member.
func newQueue(init AStarable) *queue {
	return &queue{
		queue: []AStarable{
			init,
		},
	}
}

// push adds a state to the tail of the queue.
func (q *queue) push(next AStarable) {
	q.queue = append(q.queue, next)
}

// pop pops a state from the head of the queue.
func (q *queue) pop() AStarable {
	next := q.queue[0]
	q.queue = q.queue[1:]

	return next
}

// empty returns whether the queue is empty of not.
func (q *queue) empty() bool {
	return len(q.queue) == 0
}

// witnessCache is used to cache states that we have already seen.
type witnessCache struct {
	// seen is a map of states we have encountered.  The key is the unique state
	// hash, the value is insignificant.
	seen map[string]interface{}
}

// newWitnessCache creates a witness cache with an initial state in it.
func newWitnessCache(init AStarable) *witnessCache {
	return &witnessCache{
		seen: map[string]interface{}{
			init.Hash(): nil,
		},
	}
}

// witness takes a new state and either adds it to the cache if it hasn't been
// seen before, or reports that it has been seen and can be ignored.
func (w *witnessCache) witness(move AStarable) bool {
	hash := move.Hash()

	if _, ok := w.seen[hash]; ok {
		return true
	}

	w.seen[hash] = nil

	return false
}

// AStar runs the A* BFS algorithm against a conforming AStarable object.
func AStar(init AStarable) (AStarable, error) {
	// Is the problem already solved?
	if init.Done() {
		return init, nil
	}

	// Keep a queue of moves, this gives the BFS, and a cache of states we have
	// already seen, which avoids repetition in an O(1) manner.
	queue := newQueue(init)
	cache := newWitnessCache(init)

	for !queue.empty() {
		for _, next := range queue.pop().Move() {
			// First time we have seen a done state, that's our solution.
			if next.Done() {
				return next, nil
			}

			// Reject any states that we have seen before, and enqueue any
			// that we haven't.
			if seen := cache.witness(next); seen {
				continue
			}

			queue.push(next)
		}
	}

	return nil, fmt.Errorf("%w: no more moves", cberrors.NewStackTracedError(ErrUnsolvable))
}
