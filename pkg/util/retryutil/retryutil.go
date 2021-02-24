package retryutil

import (
	"context"
	"fmt"
	"time"
)

// Retry retries a callback until it doesn't return an error.
func Retry(ctx context.Context, interval time.Duration, callback func() error) error {
	tick := time.NewTicker(interval)
	defer tick.Stop()

	for err := callback(); err != nil; err = callback() {
		select {
		case <-tick.C:
		case <-ctx.Done():
			return fmt.Errorf("timeout: %w", err)
		}
	}

	return nil
}

// RetryFor is a nice human readable version of Retry.
func RetryFor(timeout time.Duration, callback func() error) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return Retry(ctx, time.Second, callback)
}

// Assert retries a callback until it returns an error, returning that
// error.  The context timing out or begin cancelled is not an error condition.
func Assert(ctx context.Context, interval time.Duration, callback func() error) error {
	tick := time.NewTicker(interval)
	defer tick.Stop()

	var err error

	for err = callback(); err == nil; err = callback() {
		select {
		case <-tick.C:
		case <-ctx.Done():
			return nil
		}
	}

	return fmt.Errorf("assertion error: %w", err)
}

// AssertFor is a terse version of Assert.
func AssertFor(timeout time.Duration, callback func() error) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return Assert(ctx, time.Second, callback)
}
