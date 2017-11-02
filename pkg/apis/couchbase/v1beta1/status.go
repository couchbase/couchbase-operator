package v1beta1

import (
	"fmt"
	"time"
)

type ClusterPhase string

const (
	ClusterPhaseNone     ClusterPhase = ""
	ClusterPhaseCreating              = "Creating"
	ClusterPhaseRunning               = "Running"
	ClusterPhaseFailed                = "Failed"
)

type ClusterCondition struct {
	Type ClusterConditionType `json:"type"`

	Reason string `json:"reason"`

	TransitionTime string `json:"transitionTime"`
}

type ClusterConditionType string

const (
	ClusterConditionReady = "Ready"

	ClusterConditionRemovingDeadMember = "RemovingDeadMember"

	ClusterConditionRecovering = "Recovering"

	ClusterConditionScalingUp   = "ScalingUp"
	ClusterConditionScalingDown = "ScalingDown"

	ClusterConditionUpgrading = "Upgrading"
)

type ClusterStatus struct {
	// Phase is the cluster running phase
	Phase  ClusterPhase `json:"phase"`
	Reason string       `json:"reason"`

	// ControlPuased indicates the operator pauses the control of the cluster.
	ControlPaused bool `json:"controlPaused"`

	// Condition keeps ten most recent cluster conditions
	Conditions []ClusterCondition `json:"conditions"`

	// A unique cluster identifier
	ClusterID string `json:"clusterId"`
	// Size is the current size of the cluster
	Size int `json:"size"`
	// Members are the couchbase members in the cluster
	Members MembersStatus `json:"members"`
	// CurrentVersion is the current cluster version
	CurrentVersion string `json:"currentVersion"`
	// TargetVersion is the version the cluster upgrading to.
	// If the cluster is not upgrading, TargetVersion is empty.
	TargetVersion string `json:"targetVersion"`

	// Name of buckets active within cluster
	Buckets map[string]*BucketConfig `json:"buckets"`
}

type MembersStatus struct {
	// Ready are the couchbase members that are ready to serve requests
	// The member names are the same as the couchbase pod names
	Ready []string `json:"ready,omitempty"`
	// Unready are the couchbase members not ready to serve requests
	Unready []string `json:"unready,omitempty"`
}

func (cs *ClusterStatus) SetVersion(v string) {
	cs.TargetVersion = ""
	cs.CurrentVersion = v
}

func (cs *ClusterStatus) UpdateBuckets(name string, config *BucketConfig) {
	if cs.Buckets == nil {
		cs.Buckets = make(map[string]*BucketConfig)
	}

	cs.Buckets[name] = config
}

// get index of bucket name within status and remove
func (cs *ClusterStatus) RemoveBucket(b string) {
	if cs.Buckets == nil {
		cs.Buckets = make(map[string]*BucketConfig)
	}

	delete(cs.Buckets, b)
}

func (c *ClusterStatus) IsFailed() bool {
	return false
}

func (cs *ClusterStatus) SetPhase(p ClusterPhase) {
	cs.Phase = p
}

func (cs *ClusterStatus) SetClusterID(uuid string) {
	cs.ClusterID = uuid
}

func (cs *ClusterStatus) PauseControl() {
	cs.ControlPaused = true
}

func (cs *ClusterStatus) Control() {
	cs.ControlPaused = false
}

func (cs *ClusterStatus) SetReason(r string) {
	cs.Reason = r
}

func (cs *ClusterStatus) AppendScalingUpCondition(from, to int) {
	c := ClusterCondition{
		Type:           ClusterConditionScalingUp,
		Reason:         scalingReason(from, to),
		TransitionTime: time.Now().Format(time.RFC3339),
	}
	cs.appendCondition(c)
}

func (cs *ClusterStatus) AppendScalingDownCondition(from, to int) {
	c := ClusterCondition{
		Type:           ClusterConditionScalingDown,
		Reason:         scalingReason(from, to),
		TransitionTime: time.Now().Format(time.RFC3339),
	}
	cs.appendCondition(c)
}

func (cs *ClusterStatus) AppendRemovingDeadMember(name string) {
	reason := fmt.Sprintf("removing dead member %s", name)

	c := ClusterCondition{
		Type:           ClusterConditionRemovingDeadMember,
		Reason:         reason,
		TransitionTime: time.Now().Format(time.RFC3339),
	}
	cs.appendCondition(c)
}

func (cs *ClusterStatus) SetReadyCondition() {
	c := ClusterCondition{
		Type:           ClusterConditionReady,
		TransitionTime: time.Now().Format(time.RFC3339),
	}

	if len(cs.Conditions) == 0 {
		cs.appendCondition(c)
		return
	}

	lastc := cs.Conditions[len(cs.Conditions)-1]
	if lastc.Type == ClusterConditionReady {
		return
	}
	cs.appendCondition(c)
}

func (cs *ClusterStatus) appendCondition(c ClusterCondition) {
	cs.Conditions = append(cs.Conditions, c)
	if len(cs.Conditions) > 10 {
		cs.Conditions = cs.Conditions[1:]
	}
}

func scalingReason(from, to int) string {
	return fmt.Sprintf("Current cluster size: %d, desired cluster size: %d", from, to)
}
