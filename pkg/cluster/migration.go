package cluster

import (
	"fmt"
	"slices"
	"sort"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/scheduler"
	v1 "k8s.io/api/core/v1"
)

func (c *Cluster) getMigratingReadyTarget() interface{} {
	if c.cluster.IsMigrationCluster() && c.readyMembers().Empty() {
		return c.cluster.Spec.Migration.GetUnmanagedHostURL()
	}

	return c.readyMembers()
}

// checkTargetClusterVersion checks that the target cluster version is one that can be migrated to. A migration can occur to the source cluster version or when an upgrade is taking place
// as new pods are being created with the new version, however the migration will fail if the target cluster version would constitute a rollback.
func (c *Cluster) checkTargetClusterVersion() error {
	var clusterInfo couchbaseutil.ClusterInfo

	target := c.getMigratingReadyTarget()

	if err := couchbaseutil.GetPoolsDefault(&clusterInfo).On(c.api, target); err != nil {
		return err
	}

	targetClusterVersion, err := couchbaseutil.NewVersionFromImage(c.cluster.Spec.Image)

	if err != nil {
		return err
	}

	hasEqualVersion := false

	for _, node := range clusterInfo.Nodes {
		nodeVersion, err := couchbaseutil.NewVersion(node.Version)
		if err != nil {
			return err
		}

		// If the target cluster version is less than any node in the source cluster, we should error out of the migration.
		if targetClusterVersion.Less(nodeVersion) {
			return errors.ErrRollbackDuringMigration
		} else if targetClusterVersion.Equal(nodeVersion) {
			hasEqualVersion = true
		}
	}

	// If no nodes have the same version as the target cluster version, we should error out of the migration.
	if !hasEqualVersion {
		return errors.ErrClusterVersionMismatch
	}

	return nil
}

func (c *Cluster) checkIndexerStorageMode() error {
	if !c.cluster.IsIndexerEnabled() {
		return nil
	}

	indexSettings := couchbaseutil.IndexSettings{}

	if err := couchbaseutil.GetIndexSettings(&indexSettings).On(c.api, c.getMigratingReadyTarget()); err != nil {
		return err
	}

	if c.cluster.Spec.ClusterSettings.Indexer != nil && indexSettings.StorageMode != couchbaseutil.IndexStorageMode(c.cluster.Spec.ClusterSettings.Indexer.StorageMode) {
		return errors.ErrIndexStorageModeMismatch
	}

	return nil
}

func (c *Cluster) reconcileMigrationCluster() error {
	log.Info("Reconciling migration cluster", "cluster", c.namespacedName())

	pods := c.getClusterPods()

	for _, pod := range pods {
		if c.podInitialized(pod) {
			continue
		}

		log.Info("Killing uninitialized pod", "cluster", c.namespacedName(), "pod", pod.Name)

		if err := k8sutil.DeletePod(c.k8s, c.cluster.Namespace, pod.Name, c.config.GetDeleteOptions()); err != nil {
			log.Error(err, "Failed to delete uninitialized pod", "cluster", c.namespacedName(), "pod", pod.Name)
			continue
		}
	}

	scheduler, err := scheduler.New(pods, c.cluster)
	if err != nil {
		return err
	}

	c.scheduler = scheduler

	if c.members.Empty() {
		if err := c.initializeClusterState(); err != nil {
			log.Error(err, "Failed to initialize cluster state", "cluster", c.namespacedName())
			return err
		}

		lowestImage, err := c.cluster.Spec.LowestInUseCouchbaseVersionImage()
		if err != nil {
			return err
		}

		version, err := k8sutil.CouchbaseVersion(lowestImage)
		if err != nil {
			return err
		}

		c.cluster.Status.CurrentVersion = version

		target := c.getMigratingReadyTarget()

		if err := c.checkUnmanagedClusterReadiness(target); err != nil {
			return err
		}

		if err := c.initalizeClusterKubernetesResources(target); err != nil {
			log.Error(err, "Failed to initialize cluster kubernetes resources", "cluster", c.namespacedName())
			return err
		}
	} else {
		// Update the status and ready list to reflect any new members added.
		if err := c.updateMembers(); err != nil {
			log.Error(err, "Failed to update members", "cluster", c.namespacedName())
			return err
		}
	}

	if err := c.checkTargetClusterVersion(); err != nil {
		return err
	}

	if err := c.checkIndexerStorageMode(); err != nil {
		return err
	}

	// Run pre-creation reconcilers.  These are all the things we need to correctly
	// provision a Couchbase pod such as requisite services or any secrets/configmaps
	// that may be mounted in the container.
	preCreationReconcilers := reconcileFuncList{
		(*Cluster).reconcileStatus,
		(*Cluster).reconcileCompletedPods,
		(*Cluster).reconcilePeerServices,
		(*Cluster).reconcileAdminService,
		(*Cluster).initTLSCache,
		(*Cluster).refreshTLSShadowCASecret,
		(*Cluster).refreshTLSShadowSecret,
		(*Cluster).refreshTLSClientSecret,
		(*Cluster).refreshTLSPassphraseResources,
		(*Cluster).reconcileLogConfig,
		(*Cluster).reconcileCloudNativeGatewayConfig,
	}

	if err := preCreationReconcilers.run(c); err != nil {
		return err
	}

	mrm, err := NewMigrationReconcileMachine(c)

	if err != nil {
		return err
	}

	if !mrm.externalMembers.Empty() {
		c.cluster.Status.SetMigratingCondition()
	}

	if err != nil {
		return err
	}

	if aborted, err := mrm.exec(c); err != nil {
		return err
	} else if aborted {
		return nil
	}

	c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionScaling)
	c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionScalingDown)
	c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionScalingUp)

	c.cluster.Status.Size = c.members.Size()
	log.Info("Migration cluster reconciled", "cluster", c.namespacedName(), "members", c.members.Names())
	c.cluster.Status.SetBalancedCondition()

	return nil
}

func (c *Cluster) checkUnmanagedClusterReadiness(target interface{}) error {
	var clusterInfo couchbaseutil.ClusterInfo

	if err := couchbaseutil.GetPoolsDefault(&clusterInfo).On(c.api, target); err != nil {
		return fmt.Errorf("failed to get cluster info: %w", err)
	}

	for _, node := range clusterInfo.Nodes {
		if node.Status != string(NodeStatusHealthy) {
			c.cluster.Status.SetErrorCondition(fmt.Sprintf("node is not healthy: %s", node.HostName))
			log.Error(errors.ErrNodeNotActive, "Node is not healthy", "cluster", c.namespacedName(), "node", node.HostName)

			return errors.ErrNodeNotActive
		}
	}

	if clusterInfo.Balanced {
		c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionError)
		return nil
	}

	c.cluster.Status.SetErrorCondition("remote cluster needs rebalance")

	return errors.ErrRemoteClusterUnbalanced
}

type MigrationReconcileMachine struct {
	ReconcileMachine

	// externalMembers are the members that are part of the cluster and need to be migrated.
	externalMembers couchbaseutil.MemberSet

	// migratedMembers are the external members that have been created on the target cluster.
	migratedMembers couchbaseutil.MemberSet
}

func NewMigrationReconcileMachine(c *Cluster) (*MigrationReconcileMachine, error) {
	pd := couchbaseutil.ClusterInfo{}

	target := c.getMigratingReadyTarget()

	if err := couchbaseutil.GetPoolsDefault(&pd).On(c.api, target); err != nil {
		log.Error(err, "Failed to get cluster info", "cluster", c.namespacedName())
		return nil, err
	}

	k8sMembers := podsToMemberSet(c.getClusterPods())

	allNodes, err := addMissingNodesToSet(pd.Nodes, c.cluster.Spec.Servers, k8sMembers)
	if err != nil {
		log.Error(err, "Failed to get all nodes", "cluster", c.namespacedName())
		return nil, err
	}

	external := allNodes.Diff(k8sMembers)
	clusterK8sMembers := k8sMembers.Intersect(allNodes)

	log.V(1).Info("Cluster members", "cluster", c.namespacedName(), "k8s_members", k8sMembers.Names(), "external_nodes", external.Names(), "all_nodes", allNodes.Names())

	status, err := c.getStatusFromClusterInfo(&pd, allNodes)
	if err != nil {
		log.Error(err, "Failed to get status from cluster info", "cluster", c.namespacedName())
		return nil, err
	}

	allNodes.MergeWithOverwrite(clusterK8sMembers)

	state := &MemberState{
		NodeStateMap:           status.NodeStates,
		managedNodes:           allNodes,
		ActiveNodes:            couchbaseutil.NewMemberSet(),
		PendingAddNodes:        couchbaseutil.NewMemberSet(),
		AddBackNodes:           couchbaseutil.NewMemberSet(),
		FailedAddNodes:         couchbaseutil.NewMemberSet(),
		WarmupNodes:            couchbaseutil.NewMemberSet(),
		DownNodes:              couchbaseutil.NewMemberSet(),
		FailedNodes:            couchbaseutil.NewMemberSet(),
		UnclusteredNodes:       couchbaseutil.NewMemberSet(),
		IsRebalancing:          status.Balancing,
		NeedsRebalance:         !status.Balanced,
		ServerRebalanceReasons: status.RebalanceReasons,
	}

	log.V(1).Info("Node States", "cluster", c.namespacedName(), "states", status.NodeStates)

	for name, nodeState := range status.NodeStates {
		switch nodeState {
		case NodeStateActive:
			state.ActiveNodes.Add(allNodes[name])
		case NodeStatePendingAdd:
			state.PendingAddNodes.Add(allNodes[name])
		case NodeStateFailedAdd:
			state.FailedAddNodes.Add(allNodes[name])
		case NodeStateWarmup:
			state.WarmupNodes.Add(allNodes[name])
		case NodeStateDown:
			state.DownNodes.Add(allNodes[name])
		case NodeStateFailed:
			state.FailedNodes.Add(allNodes[name])
		case NodeStateAddBack:
			state.AddBackNodes.Add(allNodes[name])
		}
	}

	for name, member := range allNodes {
		if _, ok := status.NodeStates[name]; !ok {
			state.UnclusteredNodes.Add(member)
		}
	}

	return &MigrationReconcileMachine{
		ReconcileMachine: ReconcileMachine{
			clusteredMembers:   allNodes,
			runningMembers:     allNodes,
			ejectMembers:       couchbaseutil.NewMemberSet(),
			unclusteredMembers: state.UnclusteredNodes.Copy(),
			needsRebalance:     state.NeedsRebalance,
			couchbase:          state,
			c:                  c,
			rebalanceRetries:   3,
		},
		externalMembers: external,
		migratedMembers: couchbaseutil.NewMemberSet(),
	}, nil
}

func (r *MigrationReconcileMachine) getManagedK8sMembers() couchbaseutil.MemberSet {
	return r.clusteredMembers.Diff(r.externalMembers)
}

func (r *MigrationReconcileMachine) exec(c *Cluster) (bool, error) {
	reconcileFunctions := []func(*MigrationReconcileMachine, *Cluster) error{
		(*MigrationReconcileMachine).handleRebalanceCheck,
		(*MigrationReconcileMachine).handleWarmupNodes,
		(*MigrationReconcileMachine).handleDownNodes,
		(*MigrationReconcileMachine).handleUnclusteredNodes,
		(*MigrationReconcileMachine).handleFailedAddNodes,
		(*MigrationReconcileMachine).handleAddBackNodes,
		(*MigrationReconcileMachine).handleFailedNodes,
		(*MigrationReconcileMachine).handleUnknownServerConfigs,
		(*MigrationReconcileMachine).handleRemoveNode,
		(*MigrationReconcileMachine).handleAddNode,
		(*MigrationReconcileMachine).handleMigrateNodes,
		(*MigrationReconcileMachine).handleServerGroups,
		(*MigrationReconcileMachine).handleNodeServices,
		(*MigrationReconcileMachine).handleServerGroups,
		(*MigrationReconcileMachine).handleRebalance,
		(*MigrationReconcileMachine).handleMarkReady,
		(*MigrationReconcileMachine).handleMigrateCondition,
		(*MigrationReconcileMachine).handleNotifyFinished,
	}

	for i := 0; i < len(reconcileFunctions); i++ {
		if err := reconcileFunctions[i](r, c); err != nil {
			return false, err
		}

		if r.abortReason != "" {
			log.Info("Aborting Migration reconcile", "cluster", c.namespacedName(), "reason", r.abortReason)
			return true, nil
		}

		if err := c.updateCRStatus(); err != nil {
			log.Error(err, "Cluster status update failed", "cluster", c.namespacedName())
		}
	}

	return false, nil
}

func (r *MigrationReconcileMachine) handleUnknownServerConfigs(c *Cluster) error {
	// If a server configuration was deleted in a spec update then we will clean
	// up all the nodes from that server config here.
	managedClusteredMembers := r.clusteredMembers.Intersect(r.getManagedK8sMembers())
	for name, member := range managedClusteredMembers {
		r.removeMemberIfUnknownServerConfig(c, name, member)
	}

	return nil
}

func (r *MigrationReconcileMachine) handleRemoveNode(c *Cluster) error {
	var deletions []couchbasev2.ServerConfig

	var scheduledScaling couchbasev2.ScalingMessageList

	allManagedMembers := r.getManagedK8sMembers()
	allManagedClustedMembers := r.clusteredMembers.Intersect(allManagedMembers)

	allUnmanagedMembers := r.externalMembers
	allUnmanagedClusteredMembers := r.clusteredMembers.Intersect(allUnmanagedMembers)

	for _, serverSpec := range c.cluster.Spec.Servers {
		// Check to see if we need to remove anything
		members := r.clusteredMembers.GroupByServerConfig(serverSpec.Name)

		existingNodes := members.Size()
		nodesToRemove := existingNodes - serverSpec.Size

		if nodesToRemove <= 0 {
			continue
		}

		managedMembers := allManagedClustedMembers.GroupByServerConfig(serverSpec.Name)
		unmanagedMembers := allUnmanagedClusteredMembers.GroupByServerConfig(serverSpec.Name)

		if (managedMembers.Size() + unmanagedMembers.Size()) < nodesToRemove {
			nodesToRemove = managedMembers.Size()
		}

		for i := 0; i < nodesToRemove; i++ {
			deletions = append(deletions, serverSpec)
		}

		scheduledScaling = append(scheduledScaling, couchbasev2.ScalingMessage{Server: serverSpec.Name, From: existingNodes, To: serverSpec.Size})
	}

	if len(deletions) == 0 {
		return nil
	}

	r.log()

	c.cluster.Status.SetScalingDownCondition(scheduledScaling.BuildMessage())

	for _, serverSpec := range deletions {
		// Remove unmanaged first as they need to get ejected anyway
		unmanagedMembers := allUnmanagedClusteredMembers.GroupByServerConfig(serverSpec.Name)

		var server string

		if unmanagedMembers.Size() > 0 {
			server = unmanagedMembers.Names()[0]
		}

		if server == "" {
			var err error

			server, err = c.scheduler.Delete(serverSpec.Name)
			if err != nil {
				return fmt.Errorf("failed to schedule removal of member '%s': %w", serverSpec.Name, err)
			}
		}

		if m := c.members[server]; m != nil {
			r.removeMemberUser(m)
			allUnmanagedClusteredMembers.Remove(m.Name())
		}
	}

	return nil
}

func (r *MigrationReconcileMachine) handleNotifyFinished(c *Cluster) error {
	if len(r.migratedMembers) > 0 {
		remainingMigrationNodes := r.externalMembers.Size() - r.c.cluster.Spec.Migration.NumUnmanagedNodes
		log.Info("Node migration completed", "cluster", c.namespacedName(), "nodes", r.migratedMembers.Names(), "remainingMigrationNodes", remainingMigrationNodes)
	}

	log.V(1).Info("Migration reconcile completed", "cluster", c.namespacedName())

	return nil
}

func (r *MigrationReconcileMachine) handleMigrateCondition(c *Cluster) error {
	// null checks for migration
	if c.cluster.Spec.Migration == nil {
		return nil
	}

	waitingCond := r.c.cluster.Status.GetCondition(couchbasev2.ClusterConditionWaitingBetweenMigrations)

	// If we have no more nodes to migrate and we are not waiting for the stabilization period to end, then we are done migrating.
	if r.externalMembers.Empty() && (waitingCond == nil || waitingCond.Status == v1.ConditionFalse) {
		c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionMigrating)
		c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionWaitingBetweenMigrations)
	}

	if c.cluster.Spec.Migration.UnmanagedClusterHost == "" {
		return nil
	}

	if c.cluster.Spec.Migration.StabilizationPeriod == nil {
		return nil
	}

	// If we've created a migration node, we start the stabilization period.
	if len(r.migratedMembers) > 0 {
		c.cluster.Status.SetWaitingBetweenMigrations()
		return nil
	}

	// If we are waiting then check if the stabilization period has passed.
	if waitingCond != nil && waitingCond.Status == v1.ConditionTrue {
		lastTransitionTime, err := time.Parse(time.RFC3339, waitingCond.LastTransitionTime)

		if err != nil {
			// This can happen if someone has messed with the status fields.
			// We'll just assume that we don't need to wait.
			log.Info("[WARN]]: failed to parse last update time for node migration condition", "error", err)
			c.cluster.Status.SetNotWaitingBetweenMigrations()

			return nil
		}

		stabilizationPeriodFinished := time.Since(lastTransitionTime) > r.c.cluster.Spec.Migration.StabilizationPeriod.Duration

		if stabilizationPeriodFinished {
			c.cluster.Status.SetNotWaitingBetweenMigrations()
		}
	}

	return nil
}

// handleMigrateNodes handles migration of nodes from the externalMembers set to the cluster.
func (r *MigrationReconcileMachine) handleMigrateNodes(c *Cluster) error {
	if r.needsRebalance {
		log.Info("Rebalance required, skipping node migration", "cluster", c.namespacedName())
		return nil
	}

	if !c.cluster.IsReadyToAttemptMigration() {
		log.Info("Cluster not ready to start migration", "cluster", c.namespacedName())
		return nil
	}

	r.log()

	maxNodes := c.cluster.Spec.Migration.MaxConcurrentMigrations

	if maxNodes <= 0 {
		maxNodes = 1
	}

	migrationCandidates := couchbaseutil.NewMemberSet()

	numDataNodes := c.cluster.GetNumberOfDataServiceNodes()
	performDataNodesCheck := (numDataNodes > 1 && maxNodes >= numDataNodes)
	numDataNodeCandidates := 0

	allCandidates, err := r.getMigrationCandidates()
	if err != nil {
		return err
	}

	for _, member := range allCandidates {
		// Prevent migration of all data nodes at once if we have more than one data node.
		if performDataNodesCheck && r.isDataNode(member) {
			if numDataNodeCandidates >= (numDataNodes - 1) {
				continue
			}

			numDataNodeCandidates++
		}

		migrationCandidates.Add(member)

		if migrationCandidates.Size() >= maxNodes {
			break
		}
	}

	for _, member := range migrationCandidates {
		if err := r.migrateNode(member); err != nil {
			return err
		}

		r.migratedMembers.Add(member)
	}

	if len(r.migratedMembers) > 0 {
		log.Info("Migration nodes added to cluster", "cluster", c.namespacedName(), "nodes", r.migratedMembers.Names())
	}

	return nil
}

func (r *MigrationReconcileMachine) isDataNode(m couchbaseutil.Member) bool {
	config := r.c.cluster.Spec.GetServerConfigByName(m.Config())
	if config == nil {
		return false
	}

	return couchbasev2.ServiceList(config.Services).Contains(couchbasev2.DataService)
}

func (r *MigrationReconcileMachine) getMigrationCandidates() (couchbaseutil.MemberSet, error) {
	if r.externalMembers.Empty() || len(r.externalMembers) <= r.c.cluster.Spec.Migration.NumUnmanagedNodes {
		return couchbaseutil.NewMemberSet(), nil
	}

	// Default behavior if no strategy is set
	allNextCandidates := r.externalMembers

	numToMigrate := r.externalMembers.Size() - r.c.cluster.Spec.Migration.NumUnmanagedNodes

	// Check if a migration order override strategy is set
	if r.c.cluster.Spec.Migration.MigrationOrderOverride != nil {
		switch r.c.cluster.Spec.Migration.MigrationOrderOverride.MigrationOrderOverrideStrategy {
		case couchbasev2.ByServerGroup:
			if !r.c.cluster.Spec.ServerGroupsEnabled() {
				break
			}

			var err error

			allNextCandidates, err = r.getMigrationCandidatesByServerGroup()

			if err != nil {
				return couchbaseutil.NewMemberSet(), err
			}
		case couchbasev2.ByServerClass:
			allNextCandidates = r.getMigrationCandidatesByServerClass()
		case couchbasev2.ByNode:
			allNextCandidates = r.getMigrationCandidatesByNode(numToMigrate)
		}
	}

	migrationCandidates := couchbaseutil.NewMemberSet()

	for _, member := range allNextCandidates {
		if migrationCandidates.Size() >= numToMigrate {
			break
		}

		migrationCandidates.Add(member)
	}

	return migrationCandidates, nil
}

func (r *MigrationReconcileMachine) getMigrationCandidatesByServerClass() couchbaseutil.MemberSet {
	serverClassOrder := r.c.cluster.Spec.Migration.MigrationOrderOverride.ServerClassOrder

	if len(serverClassOrder) == 0 {
		for _, sc := range r.c.cluster.Spec.Servers {
			serverClassOrder = append(serverClassOrder, sc.Name)
		}
	} else if len(serverClassOrder) < len(r.c.cluster.Spec.Servers) {
		for _, sc := range r.c.cluster.Spec.Servers {
			if !slices.Contains(serverClassOrder, sc.Name) {
				serverClassOrder = append(serverClassOrder, sc.Name)
			}
		}
	}

	groupedExtMembers := r.externalMembers.GroupByServerConfigs()
	for _, sc := range serverClassOrder {
		if members, ok := groupedExtMembers[sc]; ok {
			if members.Size() > 0 {
				return members
			}
		}
	}

	return couchbaseutil.NewMemberSet()
}

func (r *MigrationReconcileMachine) getMigrationCandidatesByServerGroup() (couchbaseutil.MemberSet, error) {
	serverGroupOrder := r.c.cluster.Spec.Migration.MigrationOrderOverride.ServerGroupOrder

	// Poll the server for existing information
	existingGroups := &couchbaseutil.ServerGroups{}
	if err := couchbaseutil.ListServerGroups(existingGroups).On(r.c.api, r.c.getMigratingReadyTarget()); err != nil {
		return couchbaseutil.NewMemberSet(), err
	}

	// Get all the existing server groups and sort them alphabetically
	alphabeticalGroups := make([]string, 0, len(existingGroups.Groups))

	for _, group := range existingGroups.Groups {
		alphabeticalGroups = append(alphabeticalGroups, group.Name)
	}

	sort.Strings(alphabeticalGroups)

	// Group the external members by server group
	groupedExtNodes := map[string]couchbaseutil.MemberSet{}

	for _, group := range existingGroups.Groups {
		for _, node := range group.Nodes {
			nodeName := node.HostName.GetMemberName()
			m, ok := r.externalMembers[nodeName]

			if !ok {
				continue
			}

			if _, ok := groupedExtNodes[group.Name]; !ok {
				groupedExtNodes[group.Name] = couchbaseutil.NewMemberSet(m)
			} else {
				groupedExtNodes[group.Name].Add(m)
			}
		}
	}

	// Make sure all server groups are in the order, and if not, add them
	if len(serverGroupOrder) == 0 {
		serverGroupOrder = alphabeticalGroups
	} else if len(serverGroupOrder) < len(alphabeticalGroups) {
		for _, sg := range alphabeticalGroups {
			if !slices.Contains(serverGroupOrder, sg) {
				serverGroupOrder = append(serverGroupOrder, sg)
			}
		}
	}

	// Go through the server group order and return the first group that has external members
	for _, sc := range serverGroupOrder {
		if members, ok := groupedExtNodes[sc]; ok {
			if members.Size() > 0 {
				return members, nil
			}
		}
	}

	return couchbaseutil.NewMemberSet(), nil
}

func (r *MigrationReconcileMachine) getMigrationCandidatesByNode(numCandidates int) couchbaseutil.MemberSet {
	if r.c.cluster.Spec.Migration.MaxConcurrentMigrations < numCandidates {
		numCandidates = r.c.cluster.Spec.Migration.MaxConcurrentMigrations
	}

	candidates := couchbaseutil.NewMemberSet()

	// Go through the provided names and return the first ones that needs to be migrated.
	for _, hostName := range r.c.cluster.Spec.Migration.MigrationOrderOverride.NodeOrder {
		for _, extMember := range r.externalMembers {
			// Confusingly, we don't use GetHostName as that includes the port
			if extMember.GetDNSName() == hostName {
				candidates.Add(extMember)
				break
			}
		}

		if candidates.Size() >= numCandidates {
			return candidates
		}
	}

	// Go through the rest in alphabetical order
	sortedNodes := r.externalMembers.Names()
	sort.Strings(sortedNodes)

	for _, nodeName := range sortedNodes {
		m := r.externalMembers[nodeName]
		if !candidates.Contains(m.Name()) {
			candidates.Add(m)
		}

		if candidates.Size() >= numCandidates {
			return candidates
		}
	}

	return candidates
}

func (r *MigrationReconcileMachine) migrateNode(m couchbaseutil.Member) error {
	log.Info("Migrating node", "cluster", r.c.namespacedName(), "node", m.Name())

	serverClass := r.c.cluster.Spec.GetServerConfigByName(m.Config())

	if serverClass == nil {
		return errors.ErrNoMatchingServerClass
	}

	log.Info("Creating pod in server class", "serverClass", serverClass, "scheduler", r.c.scheduler)

	target := r.c.getMigratingReadyTarget()

	memberResults, err := r.c.addMembersToTarget(target, *serverClass)
	if err != nil {
		log.Error(err, "Failed to add members to cluster", "cluster", r.c.namespacedName())
		return err
	}

	numErrors := 0

	for _, result := range memberResults {
		if result.Err != nil {
			numErrors++
		}
	}

	errs := make([]error, 0, numErrors)

	for _, result := range memberResults {
		if result.Err != nil {
			errs = append(errs, fmt.Errorf("failed to create new node for cluster: %w", result.Err))
			log.Error(result.Err, "Pod addition to cluster failed", "cluster", r.c.namespacedName(), "pod", result.Member.Name())
		} else {
			r.addMember(result.Member)
			r.removeMemberUser(m)
		}
	}

	if len(errs) > 0 {
		log.Error(err, "Failed to migrate node", "cluster", r.c.namespacedName(), "node", m.Name(), "errors", len(errs))

		return errors.Join(errs...)
	}

	log.Info("Migration node created", "cluster", r.c.namespacedName(), "node", m.Name())

	return nil
}

// removeMemberUser simulates removing a current member.  This is called as the result
// of a user initiated action, e.g. scale down.  In this case we want to purge log volumes.
func (r *MigrationReconcileMachine) removeMemberUser(m couchbaseutil.Member) {
	if r.externalMembers.Contains(m.Name()) {
		r.externalMembers.Remove(m.Name())
	}

	r.ReconcileMachine.removeMemberUser(m)
}

func (r *MigrationReconcileMachine) handleMarkReady(c *Cluster) error {
	if candidates, err := r.getMigrationCandidates(); err != nil {
		return err
	} else if candidates.Empty() {
		c.cluster.Status.SetReadyCondition()
	}

	return nil
}
