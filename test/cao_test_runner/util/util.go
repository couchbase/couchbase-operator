package util

import (
	"errors"
	"fmt"
	"math/rand"
	"time"

	"golang.org/x/sync/errgroup"
)

const (
	Start = 97
	End   = 25
)

func CreateNameSpaceName(length int) string {
	r := rand.New(rand.NewSource(time.Now().Unix()))
	ranStr := make([]byte, length)

	for i := 0; i < length; i++ {
		ranStr[i] = byte(Start + r.Intn(End))
	}

	return string(ranStr)
}

// RetryFunctionTillTimeout retries a given function until the total duration is reached, with a specified interval between attempts.
func RetryFunctionTillTimeout(functionToExec func() error, totalDuration time.Duration, pollInterval time.Duration) error {
	var err error

	startTime := time.Now()
	for time.Since(startTime) < totalDuration {
		err = functionToExec()
		if err == nil {
			return nil
		}

		time.Sleep(pollInterval)
	}

	return fmt.Errorf("timeout reached without success: %w", err)
}

// RetryFunctionWithErrCatch retries a given function until the total duration is reached, with a specified interval between attempts.
// If an error is returned which is present in the errList then execution is immediately returned.
func RetryFunctionWithErrCatch(functionToExec func() error, totalDuration time.Duration, pollInterval time.Duration, errList []error) error {
	if errList == nil {
		errList = make([]error, 0)
	}

	startTime := time.Now()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	var eg errgroup.Group

	eg.Go(func() error {
		var err error

		for {
			err = functionToExec()
			if err == nil {
				return nil
			}

			for _, targetErr := range errList {
				if errors.Is(err, targetErr) {
					return fmt.Errorf("retry function: encountered error to be caught: %w", err)
				}
			}

			if time.Since(startTime) >= totalDuration {
				return fmt.Errorf("retry function: timeout reached without success: %w", err)
			}

			select {
			case <-ticker.C:
				continue
			}
		}
	})

	err := eg.Wait()
	if err != nil {
		return err
	}

	return nil
}
