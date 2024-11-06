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
var errTimeout = errors.New("timeout: error")

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

func TestRetryWithBackoff(t *testing.T) {
	testcases := []struct {
		name            string
		timeout         time.Duration
		interval        time.Duration
		expectedRetries int
		maxAttempts     int
		expectedError   error
	}{
		{
			name:            "retry succeeds on third attempt",
			timeout:         5 * time.Second,
			interval:        30 * time.Millisecond,
			maxAttempts:     3,
			expectedRetries: 3,
			expectedError:   nil,
		},
		{
			name:            "retries timeout",
			timeout:         5 * time.Second,
			interval:        1 * time.Second,
			maxAttempts:     4,
			expectedRetries: 3,
			expectedError:   errTimeout,
		},
	}

	for _, testcase := range testcases {
		attempts := 0
		callback := func() error {
			attempts++
			if attempts < testcase.maxAttempts {
				return errTest
			}

			return nil
		}

		err := RetryWithBackoff(testcase.interval, testcase.timeout, callback)

		if testcase.expectedRetries != attempts {
			t.Errorf("test %v expected %d retries, but there were %d", testcase.name, testcase.expectedRetries, attempts)
		}

		if err != nil && testcase.expectedError != nil && err.Error() != testcase.expectedError.Error() {
			t.Errorf("test %v expected error %v, got %v", testcase.name, testcase.expectedError, err)
		}
	}
}
