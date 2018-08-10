package cluster

import (
	"fmt"
	"sort"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
)

// Are we performing an upgrade
func (c *Cluster) upgrading() bool {
	return c.status.Upgrading()
}

// Determine whether to trigger an upgrade, only ever called outside of an upgrade
// * If the spec and status versions are the same, ignore
// * Deny upgrades from different releases
// * Deny downgrades
// * Deny upgrades across multiple major versions
func (c *Cluster) triggerUpgrade() (bool, error) {
	// Both versions the same, nothing to do
	if c.cluster.Spec.Version == c.status.CurrentVersion {
		return false, nil
	}

	// Parse the requested version in the spec and the current version in the status
	requestedVersion, err := couchbaseutil.NewVersion(c.cluster.Spec.Version)
	if err != nil {
		return false, err
	}
	currentVersion, err := couchbaseutil.NewVersion(c.status.CurrentVersion)
	if err != nil {
		return false, err
	}

	// Different prefixes, deny moving between say "community" and "enterprise" for now
	if requestedVersion.Prefix() != currentVersion.Prefix() {
		return false, fmt.Errorf("upgrading edition from '%s' to '%s' unsupported",
			currentVersion.Prefix(), requestedVersion.Prefix())
	}

	// Support only upgrades
	if currentVersion.Compare(requestedVersion) > 0 {
		return false, fmt.Errorf("downgrade from '%s' to '%s' unsupported",
			currentVersion.Version(), requestedVersion.Version())
	}

	// Support upgrades within the same major or to the next major release only
	if requestedVersion.Major()-currentVersion.Major() > 1 {
		return false, fmt.Errorf("upgrade across multiple major versions from '%s' to '%s' unsupported",
			currentVersion.Version(), requestedVersion.Version())
	}

	// All pre-flight checks passed!
	return true, nil
}

// Atomically flag the start of an upgrade
func (c *Cluster) startUpgrade() error {
	c.logger.Infof("starting upgrade from '%s' to '%s'", c.status.CurrentVersion, c.cluster.Spec.Version)
	c.raiseEvent(k8sutil.UpgradeStartedEvent(c.status.CurrentVersion, c.cluster.Spec.Version, c.cluster))

	// Make the upgrade path deterministic
	members := make([]string, len(c.status.Members.Ready))
	copy(members, c.status.Members.Ready.Names())
	sort.Strings(members)

	// Start the upgrade procedure
	status := &api.UpgradeStatus{
		TargetVersion: c.cluster.Spec.Version,
		ReadyNodes:    members[1:],
		UpgradingNode: members[0],
		UpgradedNode:  c.nextMemberName(),
		DoneNodes:     []string{},
	}
	if err := c.status.StartUpgrade(status); err != nil {
		return err
	}

	// Persist the status
	if err := c.updateCRStatus(); err != nil {
		return err
	}

	return nil
}

// Atomically update the upgrade, popping from the ready list into the
// upgrading node
func (c *Cluster) updateUpgrade() error {
	// No nodes left to upgrade, atomically complete the operation
	if len(c.status.UpgradeStatus.ReadyNodes) == 0 {
		c.logger.Infof("completed upgrade from '%s' to '%s'", c.status.CurrentVersion, c.status.UpgradeStatus.TargetVersion)
		c.raiseEvent(k8sutil.UpgradeFinishedEvent(c.status.CurrentVersion, c.cluster.Spec.Version, c.cluster))

		return c.status.CompleteUpgrade()
	}

	c.logger.Infof("updating upgrade state. %d nodes left to upgrade", len(c.status.UpgradeStatus.ReadyNodes))
	status := &api.UpgradeStatus{
		TargetVersion: c.status.UpgradeStatus.TargetVersion,
		ReadyNodes:    c.status.UpgradeStatus.ReadyNodes[1:],
		UpgradingNode: c.status.UpgradeStatus.ReadyNodes[0],
		UpgradedNode:  c.nextMemberName(),
		DoneNodes:     append(c.status.UpgradeStatus.DoneNodes, c.status.UpgradeStatus.UpgradedNode),
	}
	if err := c.status.UpdateUpgrade(status); err != nil {
		return err
	}

	// Persist the status
	if err := c.updateCRStatus(); err != nil {
		return err
	}

	return nil
}

// Atomically retry the upgrade, essentially allocates a new upgraded node name
func (c *Cluster) retryUpgrade() error {
	c.logger.Infof("updating upgrade state")
	status := &api.UpgradeStatus{
		TargetVersion: c.status.UpgradeStatus.TargetVersion,
		ReadyNodes:    c.status.UpgradeStatus.ReadyNodes,
		UpgradingNode: c.status.UpgradeStatus.UpgradingNode,
		UpgradedNode:  c.nextMemberName(),
	}
	if err := c.status.UpdateUpgrade(status); err != nil {
		return err
	}

	// Persist the status
	if err := c.updateCRStatus(); err != nil {
		return err
	}

	return nil
}

// Entering into the main upgrade body we need to perform some sanity checking.
// Each time though we expect all nodes to be active, however in the case of
// an error or operator restart we could halfway through an upgrade cycle which
// needs to be taken account of, or we need to recover from an error condition
// and retry
func (c *Cluster) checkAndFixUpgradeState(status *couchbaseutil.ClusterStatus) error {
	upgradingNodeName := c.status.UpgradeStatus.UpgradingNode
	upgradedNodeName := c.status.UpgradeStatus.UpgradedNode

	// If it exists (may have already been deleted), then the upgrading node
	// should be either active (pre-rebalance) or unclustered (post-rebalance)
	if status.ContainsNode(upgradingNodeName) {
		if !status.NodeInState(upgradingNodeName, couchbaseutil.NodeStateActive, couchbaseutil.NodeStateUnclustered) {
			return fmt.Errorf("upgrading node in unexpected state")
		}
	}

	// If it exists (may not have been created yet), then the upgraded node
	// should either be pending add (pre-rebalance) or active (post rebalance).
	// If member addition failed then we can cancel the operation, if the node is
	// unclustered then simply delete it.
	if status.ContainsNode(upgradedNodeName) {
		if status.NodeInState(upgradedNodeName, couchbaseutil.NodeStateFailedAdd) {
			// Node in the status so it must be a member
			member := c.members[upgradedNodeName]
			if err := c.cancelAddMember(nil, member); err != nil {
				return err
			}
			if err := c.client.UpdateClusterStatus(c.members, status); err != nil {
				return err
			}
		}

		if status.NodeInState(upgradedNodeName, couchbaseutil.NodeStateUnclustered) {
			if err := c.destroyMember(upgradedNodeName); err != nil {
				return err
			}
			if err := c.client.UpdateClusterStatus(c.members, status); err != nil {
				return err
			}
		}

		// If the node still exists (post possible deletion) check it's in a valid state
		if status.ContainsNode(upgradedNodeName) {
			if !status.NodeInState(upgradedNodeName, couchbaseutil.NodeStateActive, couchbaseutil.NodeStatePendingAdd) {
				return fmt.Errorf("upgraded node in unexpected state")
			}
		}
	}

	// All other nodes except the upgrading and upgraded nodes should be active
	if !status.NodeStateMap.Exclude(upgradingNodeName, upgradedNodeName).AllActive() {
		return fmt.Errorf("cluster in unexpected state")
	}

	// If a previous attempt to add a node failed, when we next create a new
	// node its name will be different to what is expected. We are expecting
	// cb-0003 to either exist or be allocated next.  If it already has been
	// created and deleted by recovery code above, the next node allocated
	// will actually be cb-0004, so we need update the status accordingly.
	if !status.ContainsNode(upgradedNodeName) && upgradedNodeName != c.nextMemberName() {
		if err := c.retryUpgrade(); err != nil {
			return err
		}
	}

	return nil
}

// Given the current cluster status, ensure the upgrading and upgraded nodes
// are in their expected states, and all other nodes are healthy
func (c *Cluster) checkUpgradeState(status *couchbaseutil.ClusterStatus, upgrading, upgraded couchbaseutil.NodeState) error {
	upgradingNodeName := c.status.UpgradeStatus.UpgradingNode
	upgradedNodeName := c.status.UpgradeStatus.UpgradedNode

	if !status.ContainsNode(upgradingNodeName) {
		return fmt.Errorf("upgrading node not present")
	}
	if !status.NodeInState(upgradingNodeName, upgrading) {
		return fmt.Errorf("upgrading node in unexpected state")
	}

	if !status.ContainsNode(upgradedNodeName) {
		return fmt.Errorf("upgraded node not present")
	}
	if !status.NodeInState(upgradedNodeName, upgraded) {
		return fmt.Errorf("upgraded node in unexpected state")
	}

	if !status.NodeStateMap.Exclude(upgradingNodeName, upgradedNodeName).AllActive() {
		return fmt.Errorf("cluster in unexpected state")
	}

	return nil
}

// Top level entry point for online-upgrades
func (c *Cluster) upgrade(status *couchbaseutil.ClusterStatus) error {
	// Check the status, if we aren't already upgrading we need to check
	// whether to start an upgrade.
	if !c.status.Upgrading() {
		upgrade, err := c.triggerUpgrade()
		// We know that the spec version has altered and is invalid,
		// deny normal reconciliation as that may add upgraded or
		// downgraded nodes to the cluster in an unmanaged fashion.
		if err != nil {
			return err
		}
		// No upgrades or the cluster is unhealthy, in the latter case
		// let reconciliation fix the status up first.
		if !upgrade || !status.ClusterHealthy() {
			return nil
		}
		if err := c.startUpgrade(); err != nil {
			return err
		}
	}

	// Let any rebalancing finish before performing further operations
	if status.IsRebalancing {
		return nil
	}

	// Check the cluster status is in an expected condition, performing any
	// fixes we deem safe and update it.  This call may change the upgrade
	// status in the CRD.
	if err := c.checkAndFixUpgradeState(status); err != nil {
		c.logStatus(status)
		return err
	}

	upgradingNodeName := c.status.UpgradeStatus.UpgradingNode
	upgradedNodeName := c.status.UpgradeStatus.UpgradedNode
	c.logger.Infof("upgrading node '%s'", upgradingNodeName)

	// Upgrading node still exists, check where the upgrade is up to
	// and take action to proceed
	if upgradingNode, ok := c.members[upgradingNodeName]; ok {
		// Check if the upgraded node exists, add it if it doesn't
		if !status.ContainsNode(upgradedNodeName) {
			// Look up the server spec based on the existing configuration
			// Warning: the spec could have been updated...
			serverSpecName := upgradingNode.ServerConfig
			serverSpec := c.cluster.Spec.GetServerConfigByName(serverSpecName)
			if serverSpec == nil {
				return fmt.Errorf("server config '%s' doesn't exist", serverSpecName)
			}

			// Add in the new node
			if _, err := c.addMember(*serverSpec); err != nil {
				return err
			}

			// Cluster status changed, update it
			if err := c.client.UpdateClusterStatus(c.members, status); err != nil {
				return err
			}

			// Before continuing ensure the upgrading node is still active,
			// the upgraded node is now pending add, and the rest of the cluster
			// is still active
			if err := c.checkUpgradeState(status, couchbaseutil.NodeStateActive, couchbaseutil.NodeStatePendingAdd); err != nil {
				c.logStatus(status)
				return err
			}
		}

		// Check if the upgrading node is unclustered, if not we need to
		// rebalance and eject it
		if !status.NodeInState(upgradingNodeName, couchbaseutil.NodeStateUnclustered) {
			// Perform the swap rebalance
			ejectedNodes := couchbaseutil.NewMemberSet(upgradingNode)
			if err := c.rebalance(ejectedNodes, nil); err != nil {
				return err
			}
		}

		// Finally delete the upgrading node now the upgraded node is added,
		// the cluster rebalanced and the upgrading node ejected
		if err := c.destroyMember(upgradingNodeName); err != nil {
			return err
		}
	}

	// Update the upgrade status
	if err := c.updateUpgrade(); err != nil {
		return err
	}

	return nil
}
