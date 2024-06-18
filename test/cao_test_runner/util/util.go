package util

import (
	"fmt"
	"math/rand"
	"time"
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
