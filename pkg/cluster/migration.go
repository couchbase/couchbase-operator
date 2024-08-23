package cluster

import (
	"fmt"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/scheduler"
)

func (c *Cluster) getMigratingReadyTarget() interface{} {
	if c.readyMembers().Empty() {
		return c.cluster.Spec.Migration.GetUnmanagedHostURL()
	}

	return c.readyMembers()
}

func (c *Cluster) reconcileMigrationCluster() error {
	log.Info("Reconciling migration cluster", "cluster", c.namespacedName())

	pods := c.getClusterPods()

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

		target := c.getMigratingReadyTarget()
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

	if aborted, err := mrm.exec(c); err != nil {
		return err
	} else if aborted {
		return nil
	}

	c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionScaling)
	c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionScalingDown)
	c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionScalingUp)

	c.cluster.Status.Size = c.members.Size()
	log.Info("Migration cluster reconciled", "cluster", c.namespacedName(), "members", c.members)
	c.cluster.Status.SetBalancedCondition()

	return nil
}

type MigrationReconcileMachine struct {
	ReconcileMachine

	// externalMembers are the members that are part of the cluster and need to be migrated.
	externalMembers couchbaseutil.MemberSet
}

func NewMigrationReconcileMachine(c *Cluster) (*MigrationReconcileMachine, error) {
	pd := couchbaseutil.ClusterInfo{}

	target := c.getMigratingReadyTarget()

	if err := couchbaseutil.GetPoolsDefault(&pd).On(c.api, target); err != nil {
		log.Error(err, "Failed to get cluster info", "cluster", c.namespacedName())
		return nil, err
	}

	k8sMembers := podsToMemberSet(c.getClusterPods())

	allNodes, err := nodeInfosToMemberSet(pd.Nodes, c.cluster.Spec.Servers)
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

	log.V(1).Info("Node States", "states", status.NodeStates)

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
		},
		externalMembers: external,
	}, nil
}

func (r *MigrationReconcileMachine) exec(c *Cluster) (bool, error) {
	reconcileFunctions := []func(*MigrationReconcileMachine, *Cluster) error{
		(*MigrationReconcileMachine).handleRebalanceCheck,
		(*MigrationReconcileMachine).handleWarmupNodes,
		(*MigrationReconcileMachine).handleDownNodes,
		(*MigrationReconcileMachine).handleFailedAddNodes,
		(*MigrationReconcileMachine).handleAddBackNodes,
		(*MigrationReconcileMachine).handleFailedNodes,
		(*MigrationReconcileMachine).handleMigrateNodes,
		(*MigrationReconcileMachine).handleRemoveNode,
		(*MigrationReconcileMachine).handleAddNode,
		(*MigrationReconcileMachine).handleNodeServices,
		(*MigrationReconcileMachine).handleRebalance,
		(*MigrationReconcileMachine).handleMarkReady,
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

// handleMigrateNodes handles migration of nodes from the externalMembers set to the cluster.
func (r *MigrationReconcileMachine) handleMigrateNodes(c *Cluster) error {
	if r.needsRebalance {
		log.Info("Rebalance required, skipping node migration", "cluster", c.namespacedName())
		return nil
	}

	maxNodes := 1

	migrationCandidates := couchbaseutil.NewMemberSet()

	for _, member := range r.getMigrationCandidates() {
		migrationCandidates.Add(member)

		if migrationCandidates.Size() >= maxNodes {
			break
		}
	}

	for _, member := range migrationCandidates {
		if err := r.migrateNode(member); err != nil {
			log.Error(err, "Failed to migrate node", "cluster", c.namespacedName(), "node", member.Name())
			return err
		}

		log.Info("Node migrated", "cluster", c.namespacedName(), "node", member.Name())
	}

	log.Info("Node migration complete", "cluster", c.namespacedName())

	return nil
}

func (r *MigrationReconcileMachine) getMigrationCandidates() couchbaseutil.MemberSet {
	return r.externalMembers
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

	log.Info("Migration complete", "cluster", r.c.namespacedName(), "errors", len(errs))

	if len(errs) == 0 {
		log.Info("Returning nil", "cluster", r.c.namespacedName(), "errors", len(errs))
		return nil
	}

	return errors.Join(errs...)
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
	if r.getMigrationCandidates().Empty() {
		c.cluster.Status.SetReadyCondition()
	}

	return nil
}
