/*
Copyright 2020-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

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
			return fmt.Errorf("failed to wait for condition: %v", err)
		}
	}

	return nil
}

// MustWaitFor waits until a condition is nil.
func MustWaitFor(t *testing.T, f WaitFunc, timeout time.Duration) {
	if err := WaitFor(f, timeout); err != nil {
		t.Fatal(err)
	}
}
