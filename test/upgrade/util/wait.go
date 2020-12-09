package util

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// WaitFunc is the function signature we expect all wait operations to present.
type WaitFunc func() error

// WaitFor waits until a condition is nil.
func WaitFor(f WaitFunc, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	tick := time.NewTicker(time.Second)
	defer tick.Stop()

	for err := f(); err != nil; err = f() {
		select {
		case <-tick.C:
		case <-ctx.Done():
			return fmt.Errorf("failed to wait for condition: %w", err)
		}
	}

	return nil
}

// CheckFor retries a function for a time period, never expecting an error.
func CheckFor(f WaitFunc, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	tick := time.NewTicker(time.Second)
	defer tick.Stop()

	var err error

	for err = f(); err == nil; err = f() {
		select {
		case <-tick.C:
		case <-ctx.Done():
			return nil
		}
	}

	return err
}

// MustWaitFor waits until a condition is nil.
func MustWaitFor(t *testing.T, f WaitFunc, timeout time.Duration) {
	if err := WaitFor(f, timeout); err != nil {
		t.Fatal(err)
	}
}

// CheckFor retries a function for a time period, never expecting an error.
func MustCheckFor(t *testing.T, f WaitFunc, timeout time.Duration) {
	if err := CheckFor(f, timeout); err != nil {
		t.Fatal(err)
	}
}
