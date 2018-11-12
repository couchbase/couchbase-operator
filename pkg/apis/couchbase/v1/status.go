package v1

import (
	"fmt"
	"sort"
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

type ClusterStatusMap map[ClusterConditionType]*ClusterCondition

// If the status contains an UpgradeStatus object then an upgrade is in progress.
//
// Initially when we detect a modification to the cluster version the struct is
// populated with the target version to upgrade to and the set of all member
// nodes, less the first node to upgrade.  The first node to upgrade is recorded
// along with the name of the new upgraded node to replace it with. Nodes are ordered
// and processed in lexical order to allow determinism.  All this is atomically
// added to the status.
type UpgradeStatus struct {
	// Version to upgrade to
	TargetVersion string `json:"targetVersion"`
	// Nodes ready to be upgraded
	ReadyNodes []string `json:"readyNodes,omitempty"`
	// Upgrading node
	UpgradingNode string `json:"upgradingNode"`
	// Upgraded node
	UpgradedNode string `json:"upgradedNode"`
	// Nodes who have been upgraded
	DoneNodes []string `json:"doneNodes,omitempty"`
}

// PortStatus contains the K8S port mappings for various services
type PortStatus struct {
	AdminServicePort        int32 `json:"adminServicePort,omitempty"`
	AdminServicePortTLS     int32 `json:"adminServicePortTLS,omitempty"`
	IndexServicePort        int32 `json:"indexServicePort,omitempty"`
	IndexServicePortTLS     int32 `json:"indexServicePortTLS,omitempty"`
	QueryServicePort        int32 `json:"queryServicePort,omitempty"`
	QueryServicePortTLS     int32 `json:"queryServicePortTLS,omitempty"`
	SearchServicePort       int32 `json:"searchServicePort,omitempty"`
	SearchServicePortTLS    int32 `json:"searchServicePortTLS,omitempty"`
	AnalyticsServicePort    int32 `json:"analyticsServicePort,omitempty"`
	AnalyticsServicePortTLS int32 `json:"analyticsServicePortTLS,omitempty"`
	EventingServicePort     int32 `json:"eventingServicePort,omitempty"`
	EventingServicePortTLS  int32 `json:"eventingServicePortTLS,omitempty"`
	DataServicePort         int32 `json:"dataServicePort,omitempty"`
	DataServicePortTLS      int32 `json:"dataServicePortTLS,omitempty"`
}

// PortStatusMap maps a node name to port status information
type PortStatusMap map[string]*PortStatus

type ClusterStatus struct {
	// Phase is the cluster running phase
	Phase  ClusterPhase `json:"phase"`
	Reason string       `json:"reason"`

	// ControlPuased indicates the operator pauses the control of the cluster.
	ControlPaused bool `json:"controlPaused"`

	// Condition keeps ten most recent cluster conditions
	Conditions ClusterStatusMap `json:"conditions,omitempty"`

	// A unique cluster identifier
	ClusterID string `json:"clusterId"`
	// Size is the current size of the cluster
	Size int `json:"size"`
	// Members are the couchbase members in the cluster
	Members MembersStatus `json:"members"`
	// CurrentVersion is the current cluster version
	CurrentVersion string `json:"currentVersion"`

	// Name of buckets active within cluster
	Buckets map[string]*BucketConfig `json:"buckets"`

	// port exposing couchbase cluster
	AdminConsolePort    string `json:"adminConsolePort,omitempty"`
	AdminConsolePortSSL string `json:"adminConsolePortSSL,omitempty"`

	// ExposedFeatures keeps tabs on what features are currently
	// exposed as node ports
	ExposedFeatures ExposedFeatureList `json:"exposedFeatures,omitempty"`

	// ports exposing couchbase cluster on the K8S node network
	ExposedPorts PortStatusMap `json:"nodePorts,omitempty"`

	// upgrade status
	UpgradeStatus *UpgradeStatus `json:"upgrade,omitempty"`
}

type MemberTimestamp struct {
	Name      string
	timestamp int64
}

func (m MemberTimestamp) Ts() time.Time {
	return time.Unix(m.timestamp, 0)
}

func NewMemberTimestamp(name string) MemberTimestamp {
	return MemberTimestamp{name, time.Now().Unix()}
}

type MemberStatusList []MemberTimestamp

func (l MemberStatusList) Contains(name string) bool {
	return l.GetMember(name) != nil
}

func (l MemberStatusList) GetMember(name string) *MemberTimestamp {
	for _, m := range l {
		if m.Name == name {
			return &m
		}
	}
	return nil
}

func (l MemberStatusList) Names() []string {
	names := []string{}
	for _, m := range l {
		names = append(names, m.Name)
	}
	return names
}

func (l *MemberStatusList) Add(name string) {
	if !l.Contains(name) {
		*l = append(*l, NewMemberTimestamp(name))
	}
}

type MembersStatus struct {
	// Ready are the couchbase members that are ready to serve requests
	// The member names are the same as the couchbase pod names
	Ready MemberStatusList `json:"ready,omitempty"`
	// Unready are the couchbase members not ready to serve requests
	Unready MemberStatusList `json:"unready,omitempty"`
	// Current Index of the members
	Index int `json:"index"`
}

// Set ready members from list.
func (ms *MembersStatus) SetReady(ready []string) {
	sort.Strings(ready)
	ms.Ready = MemberStatusList{}
	for _, m := range ready {
		ms.Ready.Add(m)
	}
}

// Set Unready members from list.
// If the member is already in the list
// then it's old timestamp is retained
func (ms *MembersStatus) SetUnready(unready []string) {
	sort.Strings(unready)
	unreadyList := MemberStatusList{}
	for _, m := range unready {
		if oldMember := ms.Unready.GetMember(m); oldMember != nil {
			unreadyList = append(unreadyList, *oldMember)
		} else {
			unreadyList.Add(m)
		}
	}
	ms.Unready = unreadyList
}

func (cs *ClusterStatus) SetVersion(v string) {
	cs.CurrentVersion = v
}

// If the upgrade status is set then we are upgrading
func (cs *ClusterStatus) Upgrading() bool {
	return cs.UpgradeStatus != nil
}

// Flag upgrade start by setting the upgrade status, only if not set
func (cs *ClusterStatus) StartUpgrade(status *UpgradeStatus) error {
	if cs.Upgrading() {
		return fmt.Errorf("unable to start upgrade upgrade state when upgrading")
	}
	cs.UpgradeStatus = status
	return nil
}

// Update upgrade status if we are already in an upgrade
func (cs *ClusterStatus) UpdateUpgrade(status *UpgradeStatus) error {
	if !cs.Upgrading() {
		return fmt.Errorf("unable to update upgrade status when not upgrading")
	}
	cs.UpgradeStatus = status
	return nil
}

// Complete an upgrade by making the current version the target and unsetting
// the upgrade status, but only if we are already upgrading
func (cs *ClusterStatus) CompleteUpgrade() error {
	if !cs.Upgrading() {
		return fmt.Errorf("unable to complete upgrade when not upgrading")
	}
	cs.CurrentVersion = cs.UpgradeStatus.TargetVersion
	cs.UpgradeStatus = nil
	return nil
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

func (cs *ClusterStatus) IsFailed() bool {
	if cs == nil {
		return false
	}
	return cs.Phase == ClusterPhaseFailed
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

func (cs *ClusterStatus) SetBucketManagementFailedCondition(reason, message string) {
	c := newClusterCondition(v1.ConditionFalse, reason, message)
	cs.setClusterCondition(ClusterConditionManageBuckets, c)
}

func (cs *ClusterStatus) SetScalingUpCondition(from, to int) {
	c := newClusterCondition(v1.ConditionTrue, "Scaling up", scalingMsg(from, to))
	cs.setClusterCondition(ClusterConditionScaling, c)
}

func (cs *ClusterStatus) SetScalingDownCondition(from, to int) {
	c := newClusterCondition(v1.ConditionTrue, "Scaling down", scalingMsg(from, to))
	cs.setClusterCondition(ClusterConditionScaling, c)
}

func (cs *ClusterStatus) SetBalancedCondition() {
	c := newClusterCondition(v1.ConditionTrue, "Cluster is balanced",
		"Data is equally distributed across all nodes in the cluster")
	cs.setClusterCondition(ClusterConditionBalanced, c)
}

func (cs *ClusterStatus) SetUnbalancedCondition() {
	c := newClusterCondition(v1.ConditionFalse, "Cluster is unbalanced",
		"The operator is attempting to rebalance the data to correct this issue")
	cs.setClusterCondition(ClusterConditionBalanced, c)
}

func (cs *ClusterStatus) SetUnknownBalancedCondition() {
	c := newClusterCondition(v1.ConditionUnknown,
		"Unable to check balanced state", "Unable to determine if cluster is balanced")
	cs.setClusterCondition(ClusterConditionBalanced, c)
}

func (cs *ClusterStatus) SetUnavailableCondition(down []string) {
	c := newClusterCondition(v1.ConditionFalse, "Cluster partially available",
		fmt.Sprintf("The following nodes are down and not serving requests: %s", strings.Join(down, ", ")))
	cs.setClusterCondition(ClusterConditionAvailable, c)
}

func (cs *ClusterStatus) SetReadyCondition() {
	c := newClusterCondition(v1.ConditionTrue, "Cluster available", "")
	cs.setClusterCondition(ClusterConditionAvailable, c)
}

func (cs *ClusterStatus) SetConfigRejectedCondition(message string) {
	c := newClusterCondition(v1.ConditionFalse, "Cluster config is rejected", message)
	cs.setClusterCondition(ClusterConditionManageConfig, c)
}

func (cs *ClusterStatus) ClearCondition(t ClusterConditionType) {
	if cs.Conditions == nil {
		cs.Conditions = ClusterStatusMap{}
	}
	delete(cs.Conditions, t)
}

func (cs *ClusterStatus) setClusterCondition(t ClusterConditionType, c *ClusterCondition) {
	if cs.Conditions == nil {
		cs.Conditions = ClusterStatusMap{}
	}
	if cp, ok := cs.Conditions[t]; ok {
		if cp.Status == c.Status && cp.Reason == c.Reason && cp.Message == c.Message {
			return
		}
	}
	cs.Conditions[t] = c
}

func newClusterCondition(status v1.ConditionStatus, reason, message string) *ClusterCondition {
	now := time.Now().Format(time.RFC3339)
	return &ClusterCondition{
		Status:             status,
		LastUpdateTime:     now,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	}
}

func scalingMsg(from, to int) string {
	return fmt.Sprintf("Current cluster size: %d, desired cluster size: %d", from, to)
}
