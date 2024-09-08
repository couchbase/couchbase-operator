package triggers

import (
	"time"
)

// TriggerConfig stores all the information required to check for the trigger.
type TriggerConfig struct {
	TriggerName     TriggerName   `yaml:"triggerName" caoCli:"required"`
	TriggerDuration time.Duration `yaml:"triggerDuration"` // Total time for which we check for trigger.
	TriggerInterval time.Duration `yaml:"triggerInterval"` // Interval time between trigger checks.
	PreTriggerWait  time.Duration `yaml:"preTriggerWait"`  // Time to wait before starting trigger check.
	PostTriggerWait time.Duration `yaml:"postTriggerWait"` // Time to wait after successful trigger check.
	CBSecretName    string        `yaml:"cbSecret"`        // K8S CB Secret to use to communicate with CB cluster.

	CBInfo CBInfo
}

// CBInfo stores all the information about the CB cluster and pods required during trigger checks.
type CBInfo struct {
	// cbPodVersion stores the CB pod name which is required for trigger check e.g. delta recovery rebalance trigger.
	cbPodName string
	// cbPodVersion stores the version of CB pod which is required for trigger check e.g. any upgrade trigger.
	cbPodVersion string
	// CBSwapRebPodName stores the CB pod name which has been added to the CB cluster during a swap rebalance of CBInfo.cbPodName.
	CBSwapRebPodName string

	cbPodsAfterScaling int
}

func SetCBPodName(tc *TriggerConfig, cbPodName string) {
	tc.CBInfo.cbPodName = cbPodName
}

func SetCBPodVersion(tc *TriggerConfig, cbVersion string) {
	tc.CBInfo.cbPodVersion = cbVersion
}

func SetCBSwapRebPodName(tc *TriggerConfig, cbSwapRebPodName string) {
	tc.CBInfo.CBSwapRebPodName = cbSwapRebPodName
}

func SetCBNodesAfterScaling(tc *TriggerConfig, cbNodes int) {
	tc.CBInfo.cbPodsAfterScaling = cbNodes
}
