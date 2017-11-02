package v1beta1

import (
	"fmt"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
)

type ClusterPhase string

const (
	ClusterPhaseNone     ClusterPhase = ""
	ClusterPhaseCreating              = "Creating"
	ClusterPhaseRunning               = "Running"
	ClusterPhaseFailed                = "Failed"
)

type ClusterCondition struct {
	// Type of cluster condition.
	Type ClusterConditionType `json:"type"`
	// Status of the condition, one of True, False, Unknown.
	Status v1.ConditionStatus `json:"status"`
	// The last time this condition was updated.
	LastUpdateTime string `json:"lastUpdateTime,omitempty"`
	// Last time the condition transitioned from one status to another.
	LastTransitionTime string `json:"lastTransitionTime,omitempty"`
	// The reason for the condition's last transition.
	Reason string `json:"reason,omitempty"`
	// A human readable message indicating details about the transition.
	Message string `json:"message,omitempty"`
}

type ClusterConditionType string

const (
	ClusterConditionAvailable     ClusterConditionType = "Available"
	ClusterConditionBalanced                           = "Balanced"
	ClusterConditionManageBuckets                      = "ManageBuckets"
	ClusterConditionManageConfig                       = "ManageConfig"
	ClusterConditionScaling                            = "Scaling"
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

func (cs *ClusterStatus) SetScalingUpCondition(from, to int) {
	c := newClusterCondition(ClusterConditionScaling, v1.ConditionTrue, "Scaling up", scalingMsg(from, to))
	cs.setClusterCondition(*c)
}

func (cs *ClusterStatus) SetScalingDownCondition(from, to int) {
	c := newClusterCondition(ClusterConditionScaling, v1.ConditionTrue, "Scaling down", scalingMsg(from, to))
	cs.setClusterCondition(*c)
}

func (cs *ClusterStatus) SetBalancedCondition() {
	c := newClusterCondition(ClusterConditionBalanced, v1.ConditionTrue, "Cluster is balanced",
		"Data is equally distributed across all nodes in the cluster")
	cs.setClusterCondition(*c)
}

func (cs *ClusterStatus) SetUnbalancedCondition() {
	c := newClusterCondition(ClusterConditionBalanced, v1.ConditionFalse, "Cluster is unbalanced",
		"The operator is attempting to rebalance the data to correct this issue")
	cs.setClusterCondition(*c)
}

func (cs *ClusterStatus) SetUnknownBalancedCondition() {
	c := newClusterCondition(ClusterConditionBalanced, v1.ConditionUnknown,
		"Unable to check balanced state", "Unable to determine if cluster is balanced")
	cs.setClusterCondition(*c)
}

func (cs *ClusterStatus) SetUnavailableCondition(down []string) {
	c := newClusterCondition(ClusterConditionAvailable, v1.ConditionFalse, "Cluster partially available",
		fmt.Sprintf("The following nodes are down and not serving requests: %s", strings.Join(down, ", ")))
	cs.setClusterCondition(*c)
}

func (cs *ClusterStatus) SetReadyCondition() {
	c := newClusterCondition(ClusterConditionAvailable, v1.ConditionTrue, "Cluster available", "")
	cs.setClusterCondition(*c)
}

func (cs *ClusterStatus) ClearCondition(t ClusterConditionType) {
	pos, _ := cs.getClusterCondition(t)
	if pos == -1 {
		return
	}
	cs.Conditions = append(cs.Conditions[:pos], cs.Conditions[pos+1:]...)
}

func (cs *ClusterStatus) setClusterCondition(c ClusterCondition) {
	pos, cp := cs.getClusterCondition(c.Type)
	if cp != nil &&
		cp.Status == c.Status && cp.Reason == c.Reason && cp.Message == c.Message {
		return
	}

	if cp != nil {
		cs.Conditions[pos] = c
	} else {
		cs.Conditions = append(cs.Conditions, c)
	}
}

func (cs *ClusterStatus) getClusterCondition(t ClusterConditionType) (int, *ClusterCondition) {
	for i, c := range cs.Conditions {
		if t == c.Type {
			return i, &c
		}
	}
	return -1, nil
}

func newClusterCondition(condType ClusterConditionType, status v1.ConditionStatus, reason, message string) *ClusterCondition {
	now := time.Now().Format(time.RFC3339)
	return &ClusterCondition{
		Type:               condType,
		Status:             status,
		LastUpdateTime:     now,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	}
}

func (cs *ClusterStatus) appendCondition(c ClusterCondition) {
	cs.Conditions = append(cs.Conditions, c)
	if len(cs.Conditions) > 10 {
		cs.Conditions = cs.Conditions[1:]
	}
}

func scalingMsg(from, to int) string {
	return fmt.Sprintf("Current cluster size: %d, desired cluster size: %d", from, to)
}
