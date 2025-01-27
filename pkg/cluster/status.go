package cluster

import (
	"fmt"
	"strings"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
)

var RetryPeriod = 5 * time.Second

// NodeState is the human readable state a Couchbase cluster
// node is in.
type NodeState string

const (
	// Active nodes are clustered and healthy.
	NodeStateActive NodeState = "Active"

	// Pending add nodes have been clustered but not balanced in.
	NodeStatePendingAdd NodeState = "PendingAdd"

	// Failed add nodes have been clustered and have failed before
	// being balanced in, specifically no vbuckets are active on them.
	NodeStateFailedAdd NodeState = "FailedAdd"

	// Warmup nodes are populating bucket caches from persistent
	// storage.  This is seen during recovery and bucket creation.
	NodeStateWarmup NodeState = "Warmup"

	// Down nodes are clustered, have active vbuckets on them and are
	// not reachable from cluster quorum members.
	NodeStateDown NodeState = "Down"

	// Failed nodes are clustered, have active vbuckets on them and
	// have been either manually or automatically failed out of the
	// cluster.  Replicas are now serving requests for the node's
	// active vbuckets.
	NodeStateFailed NodeState = "Failed"

	// Add back nodes are nodes that were manually failed over and
	// can be added back into the cluster.
	NodeStateAddBack NodeState = "AddBack"
)

// NodeStateMap translates a pod/member name to a node state.
type NodeStateMap map[string]NodeState

// Callable determines whether the node is capable of being called by
// with the Couchbase API.
func (s NodeState) Callable() bool {
	return s == NodeStateWarmup || s == NodeStateActive || s == NodeStatePendingAdd || s == NodeStateAddBack
}

// getNodeState looks up node status based on Couchbase hostname.
func getNodeState(node *couchbaseutil.NodeInfo) (NodeState, error) {
	switch node.Status {
	case "warmup":
		return NodeStateWarmup, nil
	case "healthy":
		switch node.Membership {
		case "active":
			return NodeStateActive, nil
		case "inactiveAdded":
			return NodeStatePendingAdd, nil
		case "inactiveFailed":
			return NodeStateAddBack, nil
		}
	case "unhealthy":
		switch node.Membership {
		case "active":
			return NodeStateDown, nil
		case "inactiveAdded":
			return NodeStateFailedAdd, nil
		case "inactiveFailed":
			return NodeStateFailed, nil
		}
	}

	return "", fmt.Errorf("%w: unknown node state: status=%s membership=%s", errors.NewStackTracedError(errors.ErrInternalError), node.Status, node.Membership)
}

// Status is a curated view of the Couchbase cluster state.
type Status struct {
	// NodeStates is a mapping from pod name to state.
	NodeStates NodeStateMap

	// Balanced reports whether Couchbase is balanced or not.
	Balanced bool

	// Balancing reports whether Couchbase is balancing or not.
	Balancing bool

	// Unknown members are members that Couchbase knows about but
	// we do not.
	UnknownMembers couchbaseutil.MemberSet

	// RebalanceReasons are the reasons for the rebalance being required.
	// This is only rebalance reasons directly from CB Server.
	RebalanceReasons []string
}

// GetStatus returns a new cluster status.
func (c *Cluster) GetStatus() (*Status, error) {
	target := c.getMigratingReadyTarget()

	if _, ok := target.(couchbaseutil.MemberSet); ok {
		return c.getStatus(c.callableMembers)
	}

	return c.getStatusFromTarget(target, c.members)
}

// GetStatusUnsafe returns a new cluster status from an untrusted
// set of members.
func (c *Cluster) GetStatusUnsafe(members couchbaseutil.MemberSet) (*Status, error) {
	return c.getStatus(members)
}

// getStatus parses the Couchbase cluster status and makes it easier
// to use.
func (c *Cluster) getStatus(members couchbaseutil.MemberSet) (*Status, error) {
	return c.getStatusFromTarget(members, members)
}

// getStatusFromTarget parses the Couchbase cluster status and makes it easier
// to use.
func (c *Cluster) getStatusFromTarget(target interface{}, members couchbaseutil.MemberSet) (*Status, error) {
	info := &couchbaseutil.ClusterInfo{}
	if err := couchbaseutil.GetPoolsDefault(info).RetryFor(RetryPeriod).On(c.api, target); err != nil {
		return nil, err
	}

	return c.getStatusFromClusterInfo(info, members)
}

// getStatusFromClusterInfo parses the Couchbase cluster status and makes it easier
// to use.
func (c *Cluster) getStatusFromClusterInfo(info *couchbaseutil.ClusterInfo, members couchbaseutil.MemberSet) (*Status, error) {
	status := &Status{
		NodeStates:     NodeStateMap{},
		UnknownMembers: couchbaseutil.MemberSet{},
	}

	for i := range info.Nodes {
		node := info.Nodes[i]

		name := node.HostName.GetMemberName()

		// See if the member exists, if so the node is managed, otherwise it's come from
		// some source we don't trust.
		if _, ok := members[name]; !ok {
			// Fake it until you make it...
			member := couchbaseutil.NewExternalMember(strings.Split(string(node.HostName), ":")[0], "", false)

			status.UnknownMembers.Add(member)
		}

		// Determine the human-readable node state.
		state, err := getNodeState(&node)
		if err != nil {
			return nil, err
		}

		status.NodeStates[name] = state
	}

	status.RebalanceReasons = []string{}
	for _, reasonCode := range info.ServicesNeedRebalance {
		status.RebalanceReasons = append(status.RebalanceReasons, fmt.Sprintf("%s. Services: %v.", reasonCode.Description, reasonCode.Services))
	}

	for _, reasonCode := range info.BucketsNeedRebalance {
		status.RebalanceReasons = append(status.RebalanceReasons, fmt.Sprintf("%s. Buckets: %v.", reasonCode.Description, reasonCode.Buckets))
	}

	// Couchbase reports itself as balanced correctly only when it has more
	// than one node.
	status.Balanced = len(info.Nodes) == 1 || info.Balanced

	// While upgrading to server version equal or above 7.1,
	// there is an apparent anomaly in returning the cluster balanced status.
	// See issue, https://issues.couchbase.com/browse/MB-45973 for more explanations.
	// As a temp workaround, when encountering "balanced": false during upgrades,
	// we tend to check "rebalanceStatus": "none" now.
	if upgrading, err := c.isUpgrading(); err == nil && upgrading {
		var nodeCurrentVersion string

		var found bool

		var verBelow72 bool

		var err error

		nodes := info.Nodes

		for _, node := range nodes {
			nodeCurrentVersion, _, found = strings.Cut(node.Version, "-")
			if !found {
				return nil, errors.ErrImageVersionUnretrievable
			}

			verBelow72, err = c.VersionBefore(nodeCurrentVersion, "7.2.0")
			if err != nil {
				return nil, err
			}

			if verBelow72 {
				break
			}
		}

		isVersionUpgrade, err := areNodesVersionUpgrading(nodes)
		if err != nil {
			return nil, err
		}

		if verBelow72 && isVersionUpgrade {
			status.Balanced = status.Balanced || (!info.Balanced && info.RebalanceStatus == couchbaseutil.RebalanceStatusNone)
		}
	}

	status.Balancing = info.RebalanceStatus == couchbaseutil.RebalanceStatusRunning

	return status, nil
}

func areNodesVersionUpgrading(nodes []couchbaseutil.NodeInfo) (bool, error) {
	version := ""

	for _, node := range nodes {
		nodeCurrentVersion, _, found := strings.Cut(node.Version, "-")
		if !found {
			return false, errors.ErrImageVersionUnretrievable
		}

		if version == "" {
			version = nodeCurrentVersion
		} else if version != nodeCurrentVersion {
			return true, nil
		}
	}

	return false, nil
}
