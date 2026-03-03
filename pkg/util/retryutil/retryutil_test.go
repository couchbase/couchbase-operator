/*
Copyright 2024-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package retryutil

import (
	"errors"
	"testing"
	"time"
)

func TestRetryUntilErrorOrSuccessNoErrors(t *testing.T) {
	timeout := 10 * time.Second
	interval := 10 * time.Millisecond
	counter := 0
	err := RetryUntilErrorOrSuccess(timeout, interval, func() (error, bool) {
		if counter < 5 {
			counter++
			return nil, false
		}
		return nil, true
	})

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if counter != 5 {
		t.Fatalf("expected 5 retries, got %d", counter)
	}
}

var errTest = errors.New("error")

func TestRetryUntilErrorOrSuccessWithErrors(t *testing.T) {
	timeout := 10 * time.Second
	interval := 10 * time.Millisecond
	counter := 0
	err := RetryUntilErrorOrSuccess(timeout, interval, func() (error, bool) {
		if counter == 3 {
			return errTest, false
		}

		if counter < 5 {
			counter++
			return nil, false
		}

		return nil, true
	})

	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	if counter != 3 {
		t.Fatalf("expected 3 retries, got %d", counter)
	}
}
