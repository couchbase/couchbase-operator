package e2eutil

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// Assert retries a callback until the timeout, returning an error if the callback fails.
func Assert(ctx context.Context, interval time.Duration, callback func() error) error {
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

// AssertFor is a nice human readable version of Assert.
func AssertFor(timeout time.Duration, callback func() error) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return Assert(ctx, time.Second, callback)
}

func MustAssertFor(t *testing.T, timeout time.Duration, callback func() error) {
	if err := AssertFor(timeout, callback); err != nil {
		Die(t, err)
	}
}
