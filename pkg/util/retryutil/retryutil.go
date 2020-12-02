package retryutil

import (
	"context"
	"fmt"
	"time"
)

// RetryOnErr retries a callback until it doesn't return an error.
func RetryOnErr(ctx context.Context, interval time.Duration, f func() error) error {
	tick := time.NewTicker(interval)
	defer tick.Stop()

	for err := f(); err != nil; err = f() {
		select {
		case <-tick.C:
		case <-ctx.Done():
			return fmt.Errorf("timeout: %w", err)
		}
	}

	return nil
}
