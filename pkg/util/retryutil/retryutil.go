package retryutil

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

type RetryError struct {
	n int
}

func (e *RetryError) Error() string {
	return fmt.Sprintf("still failing after %d retries", e.n)
}

func IsRetryFailure(err error) bool {
	_, ok := err.(*RetryError)
	return ok
}

type ConditionFunc func() error
type RetryFunc func() (bool, error)

// Retry retries f every interval until after maxRetries.
// The interval won't be affected by how long f takes.
// For example, if interval is 3s, f takes 1s, another f will be called 2s later.
// However, if f takes longer than interval, it will be delayed.
func Retry(ctx context.Context, interval time.Duration, maxRetries int, f RetryFunc) error {
	if maxRetries <= 0 {
		return fmt.Errorf("maxRetries (%d) should be > 0", maxRetries)
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()

	for i := 0; ; i++ {
		ok, err := f()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		if i == maxRetries {
			break
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return &RetryError{maxRetries}
}

// Retry function that can return an error and log instead as warning
// until actual maxRetries occurs.
func RetryOnErr(ctx context.Context, interval time.Duration, maxRetries int, task string, clusterName string, f ConditionFunc) error {
	return Retry(ctx, interval, maxRetries, func() (bool, error) {

		// run f() and check for err
		if err := f(); err != nil {

			// failed, log attempt
			logger := ctx.Value("logger").(*logrus.Entry)
			logger.Warningf("%s: failed with error %v ...retrying", task, err)
			return false, nil
		}

		// ok
		return true, nil
	})
}
