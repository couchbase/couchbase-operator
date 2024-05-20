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
