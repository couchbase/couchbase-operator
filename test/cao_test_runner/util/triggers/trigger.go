package triggers

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"

	"golang.org/x/sync/errgroup"
)

type TriggerName string

// Contains all the trigger names. Whenever adding a new trigger update switchTrigger().
const (
	TriggerInstant TriggerName = "instant"
	TriggerWait    TriggerName = "wait"

	// Rebalance Triggers.

	TriggerRebalance    TriggerName = "rebalance"
	TriggerRebalanceEnd TriggerName = "rebalanceEnd"

	TriggerDeltaRecovery TriggerName = "deltaRecoveryRebalance"

	// Upgrade Triggers.

	TriggerDeltaRecoveryUpgradeWarmup TriggerName = "deltaRecoveryWarmup"
	TriggerDeltaRecoveryUpgrade       TriggerName = "deltaRecoveryUpgrade"
	TriggerSwapRebalanceOutUpgrade    TriggerName = "swapRebalanceOut"
	TriggerSwapRebalanceInUpgrade     TriggerName = "swapRebalanceIn"

	// Scaling Triggers.

	TriggerScaling TriggerName = "scaling"
)

const (
	defaultTriggerDuration = 10 * time.Minute
	defaultTriggerInterval = 2 * time.Second
	defaultRequestTimeout  = 2 * time.Second
)

var (
	ErrInvalidTrigger        = errors.New("trigger name provided is invalid")
	ErrCBPodNameNotProvided  = errors.New("cb pod name not provided")
	ErrCBVersionNotProvided  = errors.New("cb pod upgrade version not provided")
	ErrNumCBPodsAfterScaling = errors.New("number of cb pods after scaling not provided")
)

// Trigger uses errgroup and cancel context to schedule go routines which will retrieve information from couchbase
// cluster in a time bounded period or else fail.
type Trigger struct {
	g   *errgroup.Group
	ctx context.Context
}

// ApplyTrigger starts the trigger check and waits for its completion.
func ApplyTrigger(triggerConfig *TriggerConfig) error {
	trigger := NewTrigger()

	trigger.WaitForTrigger(triggerConfig)

	err := trigger.Wait()
	if err != nil {
		return fmt.Errorf("apply trigger: %w", err)
	}

	return nil
}

// ApplyMultipleTriggers starts the trigger check for multiple TriggerConfig and waits for its completion.
func ApplyMultipleTriggers(triggerConfigs []*TriggerConfig) error {
	trigger := NewTrigger()

	for _, triggerConfig := range triggerConfigs {
		trigger.WaitForTrigger(triggerConfig)
	}

	err := trigger.Wait()
	if err != nil {
		return fmt.Errorf("apply trigger: %w", err)
	}

	return nil
}

// NewTrigger initialises a Trigger.
func NewTrigger() *Trigger {
	g, ctx := errgroup.WithContext(context.Background())

	return &Trigger{
		g:   g,
		ctx: ctx,
	}
}

// Wait function will wait for all go routines to finish.
// It returns an error of catching event caught or lost.
func (c *Trigger) Wait() error {
	err := c.g.Wait()
	<-c.ctx.Done()

	return err
}

// WaitForTrigger initiates a go routine which constantly listens for particular cluster events.
func (c *Trigger) WaitForTrigger(t *TriggerConfig) {
	c.g.Go(func() error {
		if t.TriggerName == TriggerInstant {
			return nil
		}

		// Add the default duration and interval to trigger
		if t.TriggerDuration == 0 {
			t.TriggerDuration = defaultTriggerDuration
		}

		if t.TriggerInterval == 0 {
			t.TriggerInterval = defaultTriggerInterval
		}

		// Waiting before starting the trigger check
		if t.PreTriggerWait != 0 {
			logrus.Infof("Waiting for %s before starting trigger `%s` check", t.PreTriggerWait.String(), t.TriggerName)
			time.Sleep(t.PreTriggerWait)
		}

		logrus.Infof("Starting trigger `%s` check", t.TriggerName)

		err := switchTrigger(t)
		if err != nil {
			return fmt.Errorf("wait for trigger: %w", err)
		}

		logrus.Infof("Trigger `%s` check successful", t.TriggerName)

		// Waiting after successful trigger check
		if t.PostTriggerWait != 0 {
			logrus.Infof("Waiting for %s after successful trigger `%s` check", t.PostTriggerWait.String(), t.TriggerName)
			time.Sleep(t.PostTriggerWait)
		}

		return nil
	})
}

func switchTrigger(t *TriggerConfig) error {
	switch t.TriggerName {
	case TriggerWait:
		{
			return WaitTime(t)
		}
	case TriggerRebalance:
		{
			return WaitForRebalance(t)
		}
	case TriggerRebalanceEnd:
		{
			return WaitForRebalanceEnd(t)
		}
	case TriggerDeltaRecovery:
		{
			return WaitForDeltaRecoveryRebalance(t)
		}
	case TriggerDeltaRecoveryUpgradeWarmup:
		{
			if t.CBInfo.cbPodName == "" {
				return fmt.Errorf("switch trigger: %w", ErrCBPodNameNotProvided)
			}

			if t.CBInfo.cbPodVersion == "" {
				return fmt.Errorf("switch trigger: %w", ErrCBVersionNotProvided)
			}

			return WaitForDeltaRecoveryUpgradeWarmup(t)
		}
	case TriggerDeltaRecoveryUpgrade:
		{
			if t.CBInfo.cbPodName == "" {
				return fmt.Errorf("switch trigger: %w", ErrCBPodNameNotProvided)
			}

			if t.CBInfo.cbPodVersion == "" {
				return fmt.Errorf("switch trigger: %w", ErrCBVersionNotProvided)
			}

			return WaitForDeltaRecoveryUpgrade(t)
		}
	case TriggerSwapRebalanceInUpgrade:
		{
			if t.CBInfo.cbPodName == "" {
				return fmt.Errorf("switch trigger: %w", ErrCBPodNameNotProvided)
			}

			if t.CBInfo.cbPodVersion == "" {
				return fmt.Errorf("switch trigger: %w", ErrCBVersionNotProvided)
			}

			return WaitForSwapRebalanceIn(t)
		}
	case TriggerSwapRebalanceOutUpgrade:
		{
			if t.CBInfo.cbPodName == "" {
				return fmt.Errorf("switch trigger: %w", ErrCBPodNameNotProvided)
			}

			return WaitForSwapRebalanceOut(t)
		}
	case TriggerScaling:
		{
			if t.CBInfo.cbPodsAfterScaling == 0 {
				return fmt.Errorf("switch trigger: %w", ErrNumCBPodsAfterScaling)
			}

			return WaitForScaling(t)
		}
	default:
		return ErrInvalidTrigger
	}
}

// WaitTime waits for TriggerConfig.TriggerDuration then triggers.
func WaitTime(trigger *TriggerConfig) error {
	logrus.Infof("Trigger `%s` waiting for %s", trigger.TriggerName, trigger.TriggerDuration.String())

	time.Sleep(trigger.TriggerDuration)

	return nil
}
