package task

import (
	"context"
	"sync"
	"time"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/validations"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions"
	icontext "github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
)

// Tree defines the structure used in a branching cao testrunner scenario.
type Tree struct {
	Trees         []*Tree
	Action        actions.Action
	Iterations    int
	MaxConcurrent int
	Timeout       time.Duration
	ErrorExpected bool
	ErrorType     string
	RetryOnError  bool
	MaxRetries    int
	Blocking      bool
	ScenarioName  string
}

// newTree returns a new tree with sensible defaults set.
func newTree() *Tree {
	return &Tree{
		Iterations:    1,
		MaxConcurrent: 1,
		Timeout:       defaultTimeoutMins * time.Minute,
	}
}

// Execute runs through the tasks in the Tree.
func (t *Tree) Execute(ctx context.Context) []error {
	tCtx := icontext.NewContext(ctx)
	if t.Action != nil {
		actionConfig, err := tCtx.ReconcileConfig(t.Action.Config())
		if err != nil {
			return []error{err}
		}

		err = t.Action.Checks(tCtx, actionConfig, validations.Pre)
		if err != nil {
			return []error{err}
		}

		err = t.Action.Do(tCtx, actionConfig)
		if err != nil {
			return []error{err}
		}

		err = t.Action.Checks(tCtx, actionConfig, validations.Post)
		if err != nil {
			return []error{err}
		}
	}

	// If the scenario has only one action but iterations > 1
	if len(t.Trees) == 0 && t.Iterations > 1 {
		t.Iterations -= 1
		return t.Execute(ctx)
	} else if len(t.Trees) == 0 {
		return []error{}
	}

	return t.do(tCtx.Context())
}

func (t *Tree) do(ctx context.Context) []error {
	work := make(chan struct{})
	done := make(chan struct{})
	errorsChan := make(chan []error)

	workers := sync.WaitGroup{}
	workers.Add(t.MaxConcurrent)

	// spawn up the correct number of workers.
	for i := 0; i < t.MaxConcurrent; i++ {
		go func() {
			defer workers.Done()

			for {
				select {
				// we got a piece of work from the channel
				case <-work:
					wg := sync.WaitGroup{}
					wg.Add(len(t.Trees))

					for _, v := range t.Trees {
						childCtx, childCancel := context.WithTimeout(ctx, t.Timeout)
						if t.Blocking {
							defer childCancel()

							errors := v.Execute(childCtx)
							errorsChan <- errors

							wg.Done()
						} else {
							go func(v *Tree) {
								defer childCancel()

								errors := v.Execute(childCtx)
								errorsChan <- errors

								wg.Done()
							}(v)
						}
					}

					wg.Wait()
				// context cancellation.
				case <-ctx.Done():
					return
				// routine is no longer needed.
				case <-done:
					return
				}
			}
		}()
	}

	go func() {
		// add the work to the work channel, blocking when no routines are ready.
		for i := 0; i < t.Iterations; i++ { //nolint:intrange
			work <- struct{}{}
		}

		// kill everything.
		for i := 0; i < t.MaxConcurrent; i++ { //nolint:intrange
			done <- struct{}{}
		}

		workers.Wait()

		close(errorsChan)
	}()

	var errors []error
	for errs := range errorsChan {
		errors = append(errors, errs...)
	}

	return errors
}
