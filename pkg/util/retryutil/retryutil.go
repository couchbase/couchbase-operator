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

// RetryWithBackoff will retry a callback until it doesn't return an error or reaches the time limit, with an exponential backoff doubling the interval each failure.
func RetryWithBackoff(interval, timeout time.Duration, callback func() error) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)

	defer cancel()

	for err := callback(); err != nil; err = callback() {
		select {
		case <-time.After(interval):
			interval *= 2
		case <-ctx.Done():
			return fmt.Errorf("timeout: %w", err)
		}
	}

	return nil
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

// RetryUntilSuccess retries a callback until it succeeds or times out.
func RetryUntilSuccess(timeout, interval time.Duration, callback func() error) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

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

// RetryUntilErrorOrSuccess retries a callback until it returns an error or succeeds (returns true).
// It will return the any errors from te callback and also if the context times out or is cancelled.
func RetryUntilErrorOrSuccess(timeout, interval time.Duration, callback func() (error, bool)) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	tick := time.NewTicker(interval)
	defer tick.Stop()

	var err error

	var success bool

	for err, success = callback(); err == nil; err, success = callback() {
		if success {
			return nil
		}

		select {
		case <-tick.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return err
}
