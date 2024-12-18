package jobs

import (
	"errors"
	"fmt"
	"time"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util"
	"github.com/sirupsen/logrus"
)

var (
	ErrJobNotStarted = errors.New("job is not started")
	ErrJobCompleted  = errors.New("job is completed")
	ErrJobFailed     = errors.New("job failed")
	ErrJobRunning    = errors.New("job is running")
	ErrUnexpected    = errors.New("unexpected error")
)

// CheckJobCompletion checks if the job has completed successfully.
/*
 * Returns nil if the job has completed successfully within the given duration.
 * Returns error if the job has not started, failed, or is still running (after duration time).
 * numPods determines the number of pods that should be running for the job.
 */
func CheckJobCompletion(jobName, namespace string, duration, interval time.Duration, numPods int) error {
	checkJobCompletion := func() error {
		job, err := UnmarshalJob(jobName, namespace)
		if err != nil {
			if errors.Is(err, ErrJobNotFound) {
				return ErrJobNotStarted
			} else {
				return err
			}
		}

		if job.Status.Active == numPods && !job.Status.StartTime.IsZero() {
			// Job is currently running.
			return ErrJobRunning
		} else if job.Status.Succeeded == numPods && !job.Status.CompletionTime.IsZero() {
			return nil
		} else if job.Status.Failed > 0 {
			// If during the job start check, the job fails.
			return ErrJobFailed
		}

		return ErrUnexpected
	}

	// Checking if the job completes successfully.
	err := util.RetryFunctionTillTimeout(checkJobCompletion, duration, interval)
	if err != nil {
		switch {
		case errors.Is(err, ErrJobNotFound):
			logrus.Warnf("Job completion check: job %s not found.", jobName)
			return fmt.Errorf("check job completion %s: %w", jobName, err)
		case errors.Is(err, ErrJobFailed):
			logrus.Warnf("Backup job success check: backup job %s has failed.", jobName)
			return fmt.Errorf("check job completion %s: %w", jobName, err)
		default:
			return fmt.Errorf("check job completion %s: %w", jobName, err)
		}
	}

	return nil
}

// CheckJobStart checks if the job has started.
/*
 * Returns nil if the job has started within the given duration.
 * Returns error if the job has not started (after duration time), failed, or completed.
 * numPods determines the number of pods that should be running for the job.
 */
func CheckJobStart(jobName, namespace string, duration, interval time.Duration, numPods int) error {
	checkJobStart := func() error {
		job, err := UnmarshalJob(jobName, namespace)
		if err != nil {
			if errors.Is(err, ErrJobNotFound) {
				return ErrJobNotStarted
			} else {
				return err
			}
		}

		if job.Status.Active == numPods && !job.Status.StartTime.IsZero() {
			// Job has started.
			return nil
		} else if job.Status.Succeeded == numPods && !job.Status.CompletionTime.IsZero() {
			// If during the job start check, the job successfully completes.
			return ErrJobCompleted
		} else if job.Status.Failed > 0 {
			// If during the job start check, any of the job fails.
			return ErrJobFailed
		}

		return ErrUnexpected
	}

	// Checking if the job has started within 20s
	err := util.RetryFunctionTillTimeout(checkJobStart, duration, interval)
	if err != nil {
		switch {
		case errors.Is(err, ErrJobCompleted):
			logrus.Warnf("Job start check: job %s has been completed.", jobName)
		case errors.Is(err, ErrJobFailed):
			logrus.Warnf("Job start check: job %s has failed.", jobName)
			return fmt.Errorf("check job start %s: %w", jobName, err)
		default:
			return fmt.Errorf("check job start %s: %w", jobName, err)
		}
	}

	return nil
}
