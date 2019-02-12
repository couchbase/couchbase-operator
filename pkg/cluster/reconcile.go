package cluster

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	cberrors "github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/netutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/pkg/util/scheduler"
	"github.com/couchbase/gocbmgr"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
)

func (c *Cluster) reconcile(pods []*v1.Pod) error {
	c.logger.Debug("Start reconciling")
	defer c.logger.Debug("Finish reconciling")

	// First thing we must do is fix up TLS or we may not be able to talk to the
	// cluster for anything else.
	if err := c.reconcileTLS(); err != nil {
		return err
	}

	// Initialize the scheduler each time around, this saves us having to update
	// internal state in all the cases when a pod fails to be created, deleted,
	// or disappears
	var err error
	if c.scheduler, err = scheduler.New(c.config.KubeCli, c.cluster); err != nil {
		return err
	}

	if len(pods) == 0 {
		err := c.recoverClusterDown()
		if err != nil {
			return err
		}
	}

	// Update the status and ready list before doing anything.
	status, err := c.client.GetClusterStatus(c.members)
	if err != nil {
		c.logger.Warnf("Unable to get cluster state, skiping reconcile loop: %v", err)
		return err
	}
	c.updateMemberStatusWithClusterInfo(status)

	state := &ReconcileMachine{
		runningPods:   podsToMemberSet(pods, c.isSecureClient()),
		knownNodes:    couchbaseutil.NewMemberSet(),
		ejectNodes:    couchbaseutil.NewMemberSet(),
		couchbase:     status,
		state:         ReconcileInit,
		removeVolumes: make(map[string]bool),
	}

	if err := c.reconcileClusterSettings(); err != nil {
		c.logger.Warnf("%s", err.Error())
		return err
	}

	if err := c.reconcileMembers(state); err != nil {
		return err
	}

	// Update the status and ready list to reflect any new members added.
	if status, err = c.client.GetClusterStatus(c.members); err != nil {
		c.logger.Warnf("Unable to get cluster state, skiping reconcile loop: %v", err)
		return err
	}
	c.updateMemberStatusWithClusterInfo(status)

	if err := c.reconcileBuckets(); err != nil {
		return err
	}

	if err := c.verifyClusterVolumes(); err != nil {
		return err
	}

	if err := c.reconcileReadiness(); err != nil {
		return err
	}

	c.reconcileAdminService()

	c.reportUpgradeComplete()
	c.status.ClearCondition(api.ClusterConditionScaling)
	c.status.SetReadyCondition()

	return nil
}

// reconcileMembers reconciles
// - running pods on k8s and cluster membership
// - cluster membership and expected size of couchbase cluster
// Steps:
// 1. Remove running pods that we didn't create explicitly (unknown members)
// 2. If we are currently in a rebalance then we should finish it.
// 3. If any nodes are down then wait for them to be failed over.
// 4. Decide which nodes should be removed and whether we need to add nodes.
//    - Nodes added to the cluster that are failed, but not rebalanced should
//      be removed.
//    - Nodes that have been failed over should be removed from the cluster
//      and rebalanced out.
//    - Nodes that are pending addition, healthy, but not yet rebalanced in
//      should be fully added in.
//    - Healthy active nodes should remain in the cluster.
// 5. We will now know what the current cluster would look like if we handled
//    any issues with the current nodes in the cluster. We now need to either
//    remove healthy nodes if we're scaling down, or add noew nodes if we need
//    to scale up.
// 6. Run a rebalance if neccessary.
// 7. Remove any nodes from the cached member set that are not part actually
//    part of the cluster.
// Returns true if reconciliation completed properly
func (c *Cluster) reconcileMembers(rm *ReconcileMachine) error {
	return rm.exec(c)
}

// logFailedMember outputs any debug information we can about a failed member creation.
func (c *Cluster) logFailedMember(name string) {
	for _, line := range k8sutil.LogPod(c.config.KubeCli, c.cluster.Namespace, name) {
		c.logger.Warn(line)
	}
}

// Create a new Couchbase cluster member
func (c *Cluster) createMember(serverSpec api.ServerConfig) (m *couchbaseutil.Member, err error) {
	// Allocate an index to be used in the name.  Get the current index then increment
	// and commit back to etcd.  That way we are guaranteed to never have conflicting
	// names
	index := c.getPodIndex()
	c.incPodIndex()

	// Create a new member
	newMember := c.newMember(index, serverSpec.Name, c.cluster.Spec.Version)

	// Prepare to delete member Pod if any errors
	// occur during creation or configuration
	defer func() {
		if err != nil {
			c.decPodIndex()
			// Deleting volumes, even log volumes
			// if node doesn't get to start
			c.removePod(newMember.Name, true)
		}
	}()

	if err := c.createPod(newMember, serverSpec); err != nil {
		return nil, fmt.Errorf("fail to create member's pod (%s): %v", newMember.Name, err)
	}

	// Synchronize on pod creation and service availability
	if err := c.waitForCreatePod(newMember); err != nil {
		// We will delete the pod on error, so collect any ephemeral debug we can before
		// discarding it forever.  This will capture errors such as users specifying the
		// wrong image name (pull error), PVCs taking an age to become bound etc.
		c.logger.Warnf("member %s creation failed", newMember.Name)
		c.logFailedMember(newMember.Name)
		c.raiseEventCached(k8sutil.MemberCreationFailedEvent(newMember.Name, c.cluster))
		return nil, err
	}

	// The new node will not be part of the cluster yet  so the API calls will fail
	// when checking the UUID, temporarily disable these checks while installing
	// TLS configuration
	c.client.SetUUID("")
	defer c.client.SetUUID(c.status.ClusterID)

	// Enable TLS if requested
	if err := c.initMemberTLS(newMember, c.cluster.Spec); err != nil {
		c.raiseEventCached(k8sutil.MemberCreationFailedEvent(newMember.Name, c.cluster))
		return nil, err
	}

	// Initialize node paths
	dataPath, indexPath, analyticsPaths := getServiceDataPaths(serverSpec.GetVolumeMounts())
	if err := c.client.NodeInitialize(newMember, c.cluster.Name, dataPath, indexPath, analyticsPaths); err != nil {
		return nil, err
	}

	// Notify that we have created a new member
	c.clusterAddMember(newMember)
	if err := c.updateCRStatus(); err != nil {
		return nil, err
	}

	return newMember, nil
}

// Creates and adds a new Couchbase cluster member
func (c *Cluster) addMember(serverSpec api.ServerConfig) (*couchbaseutil.Member, error) {
	// Save the existing members now, these will be the set we use to add the new node via
	ms := c.members.Copy()

	// Create the new member
	newMember, err := c.createMember(serverSpec)
	if err != nil {
		return nil, err
	}

	// Add to the cluster. Note we have to use the plain text url as
	// /controller/addNode will not work with a https reference
	if err := c.client.AddNode(ms, newMember.ClientURLPlaintext(), serverSpec.Services); err != nil {
		return newMember, err
	}

	c.logger.Infof("added member (%s)", newMember.Name)
	c.raiseEvent(k8sutil.MemberAddEvent(newMember.Name, c.cluster))

	return newMember, nil
}

// Destroys a Couchbase cluster member
func (c *Cluster) destroyMember(name string, removeVolumes bool) error {
	if err := c.removePod(name, removeVolumes); err != nil {
		return err
	}

	// Notify of deletion
	c.clusterRemoveMember(name)
	if err := c.updateCRStatus(); err != nil {
		return err
	}

	return nil
}

// Cancel a node addition
func (c *Cluster) cancelAddMember(ms couchbaseutil.MemberSet, member *couchbaseutil.Member) error {
	if ms == nil {
		m := couchbaseutil.NewMemberSet(member)
		ms = c.members.Diff(m)
	}
	if err := c.client.CancelAddNode(ms, member.HostURLPlaintext()); err != nil {
		return err
	}
	c.raiseEvent(k8sutil.FailedAddNodeEvent(member.Name, c.cluster))
	return nil
}

// Rebalance nodes in the cluster
func (c *Cluster) rebalance(managed couchbaseutil.MemberSet, unmanaged []string) error {
	// Notify that we are starting a rebalance, the actual client operation
	// is blocking so we need to report now or kubernetes will be out of sync
	c.raiseEvent(k8sutil.RebalanceStartedEvent(c.cluster))
	c.status.SetUnbalancedCondition()
	if err := c.updateCRStatus(); err != nil {
		return err
	}

	// Perform the operation
	nodesToRemove := append(managed.HostURLsPlaintext(), unmanaged...)
	if err := c.client.Rebalance(c.members, nodesToRemove, true); err != nil {
		return err
	}

	// Error checking... perfom this a few times as there is a race between when
	// we check and when Server reports rebalance is complete.
	var status *couchbaseutil.ClusterStatus
	retryFunc := func() (bool, error) {
		var err error
		status, err = c.client.GetClusterStatus(c.members)
		if err != nil {
			return false, err
		}
		return !status.NeedsRebalance, nil
	}
	if err := retryutil.Retry(c.ctx, time.Second, 10, retryFunc); err != nil && !retryutil.IsRetryFailure(err) {
		return err
	}

	// Check that Couchbase server is happy with the state
	if status.NeedsRebalance {
		c.raiseEvent(k8sutil.RebalanceIncompleteEvent(c.cluster))
		return fmt.Errorf("cluster reports rebalance incomplete")
	}

	// Notify if we've removed some nodes (deterministically sorted)
	sort.Strings(nodesToRemove)
	for _, nodeToRemove := range nodesToRemove {
		// TODO: this feels dirty, but as this is a mixed bag of members and
		// un-managed nodes we have no other option for now
		memberName := strings.Split(nodeToRemove, ".")[0]
		c.raiseEvent(k8sutil.MemberRemoveEvent(memberName, c.cluster))
	}

	// Report the cluster is balanced
	c.raiseEvent(k8sutil.RebalanceCompletedEvent(c.cluster))
	c.status.SetBalancedCondition()
	if err := c.updateCRStatus(); err != nil {
		return err
	}
	return nil
}

// reconcile buckets by adding or removing
// buckets one at a time based on comparison
// of existing buckets to cluster spec
func (c *Cluster) reconcileBuckets() error {
	if c.cluster.Spec.DisableBucketManagement {
		return nil
	}

	existingBuckets, err := c.client.GetBucketNames(c.readyMembers())
	if err != nil {
		return fmt.Errorf("Unable to get buckets from cluster: %v", err)
	}

	// when reconciling buckets, any buckets in cluster
	// created outside of operator are removed,
	// and any buckets removed by user are recreated
	// if still present in active spec
	spec := c.cluster.Spec
	bucketsToAdd, bucketsToRemove := spec.BucketDiff(existingBuckets)
	bucketsToEdit, err := c.client.GetBucketsToEdit(c.readyMembers(), &spec)
	if err != nil {
		return fmt.Errorf("Unable to get list of buckets to edit: %v", err)
	}

	for _, bucketName := range bucketsToRemove {
		err := c.deleteClusterBucket(bucketName)
		if err != nil {
			msg := fmt.Sprintf("Bucket: %s %s", bucketName, err.Error())
			c.status.SetBucketManagementFailedCondition("Bucket delete failed", msg)
			return fmt.Errorf("Unable to delete bucketName named - %s: %v", bucketName, err)
		}
		c.logger.Infof("Removed bucketName %s", bucketName)
	}

	for _, bucketName := range bucketsToEdit {
		err := c.editClusterBucket(bucketName)
		if err != nil {
			msg := fmt.Sprintf("Bucket: %s %s", bucketName, err.Error())
			c.status.SetBucketManagementFailedCondition("Bucket edit failed", msg)
			return fmt.Errorf("Unable to edit bucketName named - %s: %v", bucketName, err)
		}
		c.logger.Infof("Edited bucketName %s", bucketName)
	}

	for _, bucketName := range bucketsToAdd {
		err := c.createClusterBucket(bucketName)
		if err != nil {
			msg := fmt.Sprintf("Bucket: %s %s", bucketName, err.Error())
			c.status.SetBucketManagementFailedCondition("Bucket add failed", msg)
			return fmt.Errorf("Unable to create bucketName named - %s: %v", bucketName, err)
		}
		c.logger.Infof("Created bucketName %s", bucketName)
	}

	c.status.ClearCondition(api.ClusterConditionManageBuckets)

	return nil
}

// reconcile changes to selected pod labels for
// the nodePort service exposing admin console
func (c *Cluster) reconcileAdminService() {
	status, err := k8sutil.UpdateAdminConsole(c.config.KubeCli, c.cluster, &c.status)
	if err != nil {
		c.logger.Warnf("error reconciling admin console service: %v", err)
		return
	}

	serviceName := k8sutil.ConsoleServiceName(c.cluster.Name)
	switch status {
	case k8sutil.ReconcileStatusCreated:
		c.logger.Infof("Created service %s for admin console", serviceName)
		c.raiseEvent(k8sutil.AdminConsoleSvcCreateEvent(serviceName, c.cluster))
	case k8sutil.ReconcileStatusDeleted:
		c.logger.Infof("Deleted service %s for admin console", serviceName)
		c.raiseEvent(k8sutil.AdminConsoleSvcDeleteEvent(serviceName, c.cluster))
	case k8sutil.ReconcileStatusUpdated:
		c.logger.Infof("Updated service %s for admin console", serviceName)
	}
}

// reconcileExposedFeatures looks at the requested exported feature set in the
// specification and add/removes services as requested, raising events as
// appropriate.
func (c *Cluster) reconcileExposedFeatures() error {
	status, err := k8sutil.UpdateExposedFeatures(c.config.KubeCli, c.members, c.cluster, &c.status)
	if err != nil {
		return err
	}
	for _, service := range status.Added {
		c.raiseEvent(k8sutil.NodeServiceCreateEvent(service, c.cluster))
	}
	for _, service := range status.Removed {
		c.raiseEvent(k8sutil.NodeServiceDeleteEvent(service, c.cluster))
	}
	return nil
}

// create bucket on cluster
func (c *Cluster) createClusterBucket(bucketName string) error {
	config := c.cluster.Spec.GetBucketByName(bucketName)
	err := c.client.CreateBucket(c.readyMembers(), config)
	if err == nil {
		c.status.UpdateBuckets(bucketName, config)
		c.raiseEvent(k8sutil.BucketCreateEvent(bucketName, c.cluster))
	}
	return err
}

func (c *Cluster) deleteClusterBucket(bucketName string) error {
	err := c.client.DeleteBucket(c.readyMembers(), bucketName)
	if err == nil {
		c.status.RemoveBucket(bucketName)
		c.raiseEvent(k8sutil.BucketDeleteEvent(bucketName, c.cluster))
	}
	return err
}

// edit bucket on cluster
func (c *Cluster) editClusterBucket(bucketName string) error {
	config := c.cluster.Spec.GetBucketByName(bucketName)

	if err := c.validateEditBucket(config); err != nil {
		return err
	}
	err := c.client.EditBucket(c.readyMembers(), config)
	if err == nil {
		c.status.UpdateBuckets(bucketName, config)
		c.raiseEvent(k8sutil.BucketEditEvent(bucketName, c.cluster))
	}
	return err
}

// Validate edit bucket returns error on attempts
// to change immutable attributes
func (c *Cluster) validateEditBucket(config *api.BucketConfig) error {

	bucketName := config.BucketName
	if statusBucket, ok := c.status.Buckets[bucketName]; ok {
		if !reflect.DeepEqual(config.ConflictResolution, statusBucket.ConflictResolution) {
			return cberrors.ErrInvalidBucketParamChange{
				Bucket: bucketName,
				Param:  "conflictResolution",
				From:   statusBucket.ConflictResolution,
				To:     config.ConflictResolution}
		}
		if config.BucketType != statusBucket.BucketType {
			return cberrors.ErrInvalidBucketParamChange{
				Bucket: bucketName,
				Param:  "type",
				From:   statusBucket.BucketType,
				To:     config.BucketType}
		}
	}

	return nil
}

// initializes the first member in the cluster
func (c *Cluster) initMember(m *couchbaseutil.Member, serverSpec api.ServerConfig) error {
	c.logger.Infof("Initializing the first node in the cluster")
	settings := c.cluster.Spec.ClusterSettings

	defaults := &cbmgr.PoolsDefaults{
		ClusterName:          c.cluster.Name,
		DataMemoryQuota:      settings.DataServiceMemQuota,
		IndexMemoryQuota:     settings.IndexServiceMemQuota,
		SearchMemoryQuota:    settings.SearchServiceMemQuota,
		EventingMemoryQuota:  settings.EventingServiceMemQuota,
		AnalyticsMemoryQuota: settings.AnalyticsServiceMemQuota,
	}

	// set default volume paths and allow for override of via spec
	dataPath, indexPath, analyticsPaths := getServiceDataPaths(serverSpec.GetVolumeMounts())
	if err := c.client.InitializeCluster(m, c.username, c.password, defaults,
		serverSpec.Services, dataPath, indexPath, analyticsPaths, settings.IndexStorageSetting); err != nil {
		return err
	}

	// enables autofailover by default
	autoFailoverSettings := &cbmgr.AutoFailoverSettings{
		Enabled:  true,
		Timeout:  settings.AutoFailoverTimeout,
		MaxCount: settings.AutoFailoverMaxCount,
		FailoverOnDataDiskIssues: cbmgr.FailoverOnDiskFailureSettings{
			Enabled:    settings.AutoFailoverOnDataDiskIssues,
			TimePeriod: settings.AutoFailoverOnDataDiskIssuesTimePeriod,
		},
		FailoverServerGroup: settings.AutoFailoverServerGroup,
	}
	return c.client.SetAutoFailoverSettings(c.readyMembers(), autoFailoverSettings)
}

// Initialize a member with TLS certificates
func (c *Cluster) initMemberTLS(m *couchbaseutil.Member, cs api.ClusterSpec) error {
	if cs.TLS != nil {
		// Static configuration:
		// * Upload the cluster CA certifcate
		// * Reload the server certifcate/key.  These were injected into
		//   the pod's file system from a secret during creation
		if cs.TLS.Static != nil {
			// Grab the operator secret
			secretName := cs.TLS.Static.OperatorSecret
			secret, err := k8sutil.GetSecret(c.config.KubeCli, secretName, c.cluster.Namespace, nil)
			if err != nil {
				return err
			}

			// Extract the CA's PEM data
			ca, ok := secret.Data[tlsOperatorSecretCACert]
			if !ok {
				return fmt.Errorf("unable to find %s in operator secret", tlsOperatorSecretCACert)
			}

			// Upgrade to a TLS connection.  Do these first few calls unverified as we haven't installed
			// the server cert that corresponds with the cluster CA in the client yet.
			m.SecureClient = true
			tls := c.client.GetTLS()
			c.client.SetTLS(&cbmgr.TLSAuth{CACert: ca, Insecure: true})
			defer c.client.SetTLS(tls)

			// Update Couchbase's TLS configuration
			if err := c.client.UploadClusterCACert(m, ca); err != nil {
				return err
			}
			if err := c.client.ReloadNodeCert(m); err != nil {
				return err
			}

			// Enable TLS verification now the certs are installed for the benefit of the the
			// TLS verification test.
			c.client.SetTLS(tls)

			// TODO: Not available until >=5.5.0, even then does authz which we don't want :(
			//settings := &cbmgr.ClientCertAuth{
			//	State: "mandatory",
			//}
			//if err := c.client.SetClientCertAuth(m, settings); err != nil {
			//	return err
			//}

			// Wait for the port to come backup with the correct certificate chain
			ctx, cancel := context.WithTimeout(c.ctx, 60*time.Second)
			defer cancel()
			if err := netutil.WaitForHostPortTLS(ctx, m.HostURL(), ca); err != nil {
				return err
			}
		}
	}
	return nil
}

// getServerGroups looks over the spec and collects all server groups
// which are defined
func (c *Cluster) getServerGroups() []string {
	// Gather a set of unique server groups
	serverGroups := map[string]interface{}{}
	for _, serverGroup := range c.cluster.Spec.ServerGroups {
		serverGroups[serverGroup] = nil
	}
	for _, serverClass := range c.cluster.Spec.ServerSettings {
		for _, serverGroup := range serverClass.ServerGroups {
			serverGroups[serverGroup] = nil
		}
	}

	// Map into a list
	serverGroupList := []string{}
	for serverGroup, _ := range serverGroups {
		serverGroupList = append(serverGroupList, serverGroup)
	}

	return serverGroupList
}

// createServerGroups creates any server groups defined in the specification
// whuch Couchbase doesn't know about
func (c *Cluster) createServerGroups(existingGroups *cbmgr.ServerGroups) (*cbmgr.ServerGroups, error) {
	serverGroups := c.getServerGroups()

	// Create any that do not exist
	for _, serverGroup := range serverGroups {
		if existingGroups.GetServerGroup(serverGroup) != nil {
			continue
		}

		if err := c.client.CreateServerGroup(c.members, serverGroup); err != nil {
			return nil, err
		}

		// 409s have been seen due to this not being updated quick enough, ensure the
		// new server group exists before continuing
		err := retryutil.Retry(c.ctx, 5*time.Second, couchbaseutil.RetryCount, func() (bool, error) {
			var err error
			existingGroups, err = c.client.GetServerGroups(c.members)
			if err != nil {
				return false, err
			}
			return existingGroups.GetServerGroup(serverGroup) != nil, nil
		})
		if err != nil {
			return nil, err
		}
	}

	return existingGroups, nil
}

// Given a server group update return the index of the named group
// TODO: Move to gocbmgr as a receiver function
func serverGroupIndex(update *cbmgr.ServerGroupsUpdate, name string) (int, error) {
	for index, group := range update.Groups {
		if group.Name == name {
			return index, nil
		}
	}
	return -1, fmt.Errorf("server group %s undefined", name)
}

// reconcileServerGroups looks at the cluster specification, if we have enabled
// server groups, lookup the availability zone the member is in, create the server
// group if it doesn't exist and add the member to the group
func (c *Cluster) reconcileServerGroups() (bool, error) {
	// Cluster not server group aware
	if !c.cluster.Spec.ServerGroupsEnabled() {
		return false, nil
	}

	// Poll the server for exising information
	existingGroups, err := c.client.GetServerGroups(c.members)
	if err != nil {
		return false, err
	}

	// Create any server groups which need defining
	existingGroups, err = c.createServerGroups(existingGroups)
	if err != nil {
		return false, err
	}

	// Create a server group update
	newGroups := cbmgr.ServerGroupsUpdate{
		Groups: []cbmgr.ServerGroupUpdate{},
	}
	for _, existingGroup := range existingGroups.Groups {
		newGroup := cbmgr.ServerGroupUpdate{
			Name:  existingGroup.Name,
			URI:   existingGroup.URI,
			Nodes: []cbmgr.ServerGroupUpdateOTPNode{},
		}
		newGroups.Groups = append(newGroups.Groups, newGroup)
	}

	// Look at each node in each existing group building up the update
	// structure and also checking to see whether we need to dispatch this
	// change to Couchbase server
	update := false
	for _, existingGroup := range existingGroups.Groups {
		for _, existingMember := range existingGroup.Nodes {
			// Extract the scheduled server group for the node
			podName := strings.Split(existingMember.HostName, ".")[0]
			scheduledServerGroup, err := k8sutil.GetServerGroup(c.config.KubeCli, c.cluster.Namespace, podName)

			// A status error from the API probably means a 404, just reuse the old
			// server group location
			if err != nil {
				switch err.(type) {
				case *errors.StatusError:
					scheduledServerGroup = existingGroup.Name
				default:
					return false, err
				}
			}

			// TODO: should we flag this as a warning and leave it where it is?
			if scheduledServerGroup == "" {
				return false, fmt.Errorf("server group unset for pod %s", podName)
			}

			// If the node is in the wrong server group schedule an update
			if scheduledServerGroup != existingGroup.Name {
				update = true
			}

			// Calculate the server group to add the node to
			index, err := serverGroupIndex(&newGroups, scheduledServerGroup)
			if err != nil {
				// You have done something stupid like change the pod label
				return false, fmt.Errorf("server group %s for pod %s undefined", scheduledServerGroup, podName)
			}

			// Insert the node in the correct server group
			otpNode := cbmgr.ServerGroupUpdateOTPNode{
				OTPNode: existingMember.OTPNode,
			}
			newGroups.Groups[index].Nodes = append(newGroups.Groups[index].Nodes, otpNode)
		}
	}

	// Nothing to do
	if !update {
		return false, nil
	}

	return true, c.client.UpdateServerGroups(c.members, existingGroups.GetRevision(), &newGroups)
}

// TEMPORARY HACK
// Does exactly the same as above but tells us if we need to do anything
func (c *Cluster) wouldReconcileServerGroups() (bool, error) {
	// Cluster not server group aware
	if !c.cluster.Spec.ServerGroupsEnabled() {
		return false, nil
	}

	// Poll the server for exising information
	existingGroups, err := c.client.GetServerGroups(c.members)
	if err != nil {
		return false, err
	}

	// Create any server groups which need defining
	existingGroups, err = c.createServerGroups(existingGroups)
	if err != nil {
		return false, err
	}

	// Look at each node in each existing group building up the update
	// structure and also checking to see whether we need to dispatch this
	// change to Couchbase server
	for _, existingGroup := range existingGroups.Groups {
		for _, existingMember := range existingGroup.Nodes {
			// Extract the scheduled server group for the node
			podName := strings.Split(existingMember.HostName, ".")[0]
			scheduledServerGroup, err := k8sutil.GetServerGroup(c.config.KubeCli, c.cluster.Namespace, podName)

			// A status error from the API probably means a 404, just reuse the old
			// server group location
			if err != nil {
				switch err.(type) {
				case *errors.StatusError:
					scheduledServerGroup = existingGroup.Name
				default:
					return false, err
				}
			}

			// TODO: should we flag this as a warning and leave it where it is?
			if scheduledServerGroup == "" {
				return false, fmt.Errorf("server group unset for pod %s", podName)
			}

			// If the node is in the wrong server group schedule an update
			if scheduledServerGroup != existingGroup.Name {
				return true, nil
			}
		}
	}

	return false, nil
}

// createAlternateAddressesExternal calculates what the current state of the node's alternate
// addresses should be. For public addresses we maintain the default ports, however set the
// alternate address to the DDNS name.  For private addresses these will be an IP based on the
// node address and node ports in the 30000 range.
func (c *Cluster) createAlternateAddressesExternal(member *couchbaseutil.Member) (*cbmgr.AlternateAddressesExternal, error) {
	if c.cluster.Spec.IsExposedFeatureServiceTypePublic() {
		addresses := &cbmgr.AlternateAddressesExternal{
			Hostname: k8sutil.GetDNSName(c.cluster, member.Name),
		}
		return addresses, nil
	}

	// Lookup the node IP the pod is running on.
	hostname, err := k8sutil.GetHostIP(c.config.KubeCli, c.cluster.Namespace, member.Name)
	if err != nil {
		return nil, err
	}

	addresses := &cbmgr.AlternateAddressesExternal{
		Hostname: hostname,
	}

	// If any exposed ports are defined then add them to the external addresses.
	if ports, ok := c.status.ExposedPorts[member.Name]; ok {
		addresses.Ports = &cbmgr.AlternateAddressesExternalPorts{
			AdminServicePort:    ports.AdminServicePort,
			AdminServicePortTLS: ports.AdminServicePortTLS,
			// TODO: rename the library for consistency
			ViewServicePort:         ports.IndexServicePort,
			ViewServicePortTLS:      ports.IndexServicePortTLS,
			QueryServicePort:        ports.QueryServicePort,
			QueryServicePortTLS:     ports.QueryServicePortTLS,
			FtsServicePort:          ports.SearchServicePort,
			FtsServicePortTLS:       ports.SearchServicePortTLS,
			AnalyticsServicePort:    ports.AnalyticsServicePort,
			AnalyticsServicePortTLS: ports.AnalyticsServicePortTLS,
			DataServicePort:         ports.DataServicePort,
			DataServicePortTLS:      ports.DataServicePortTLS,
		}
	}

	return addresses, nil
}

// initMemberAlternateAddresses injects the K8S node's L3 address and alternate
// ports into the requested member.  Clients may use these addresses/ports to
// connect to the cluster if there is no direct L3 connectivity into the pod
// network.
func (c *Cluster) reconcileMemberAlternateAddresses() error {
	// Examine each member in turn as they will have different node
	// addresses (i.e. you must be using anti affinity or kubernetes
	// has no way of addressing individual cluster nodes).
	for _, member := range c.members {
		// Grab the current configuration
		existingAddresses, err := c.client.GetAlternateAddressesExternal(member)
		if err != nil {
			// If we cannot make contact then just continue, it may have been deleted
			c.logger.Warnf("unable to poll external addresses for pod %s", member.Name)
			return nil
		}

		// If we don't have any exposed ports, but the node reports it is configured so
		// then remove the configuration.
		if !c.cluster.Spec.HasExposedFeatures() {
			if existingAddresses != nil {
				if err := c.client.DeleteAlternateAddressesExternal(member); err != nil {
					return err
				}
			}
			continue
		}

		// Get the requested alternate address specification.
		addresses, err := c.createAlternateAddressesExternal(member)
		if err != nil {
			return err
		}

		// Check to see if we need to perform any updates, ignoring if not
		if reflect.DeepEqual(addresses, existingAddresses) {
			continue
		}

		// Perform the update
		if err := c.client.SetAlternateAddressesExternal(member, addresses); err != nil {
			return err
		}
	}
	return nil
}

// TEMPORARY HACK
// Does exactly the same as above but tells us if we need to do anything
func (c *Cluster) wouldReconcileMemberAlternateAddresses() (bool, error) {
	// Examine each member in turn as they will have different node
	// addresses (i.e. you must be using anti affinity or kubernetes
	// has no way of addressing individual cluster nodes).
	for _, member := range c.members {
		// Grab the current configuration
		existingAddresses, err := c.client.GetAlternateAddressesExternal(member)
		if err != nil {
			// If we cannot make contact then just continue, it may have been deleted
			c.logger.Warnf("unable to poll external addresses for pod %s", member.Name)
			return false, nil
		}

		// If we don't have any exposed ports, but the node reports it is configured so
		// then remove the configuration.
		if !c.cluster.Spec.HasExposedFeatures() {
			if existingAddresses != nil {
				return true, err
			}
			continue
		}

		// Get the requested alternate address specification.
		addresses, err := c.createAlternateAddressesExternal(member)
		if err != nil {
			return false, err
		}

		// Check to see if we need to perform any updates, ignoring if not
		if reflect.DeepEqual(addresses, existingAddresses) {
			continue
		}

		// Perform the update
		return true, nil
	}
	return false, nil
}

func (c *Cluster) reconcileClusterSettings() error {

	if err := c.reconcileAutoFailoverSettings(); err != nil {
		return err
	}
	if err := c.reconcileMemoryQuotaSettings(); err != nil {
		return err
	}
	if err := c.reconcileSoftwareUpdateNotificationSettings(); err != nil {
		return err
	}
	if err := c.reconcileIndexStorageSettings(); err != nil {
		return err
	}

	c.status.ClearCondition(api.ClusterConditionManageConfig)
	return nil
}

// ensure autofailover timeout matches spec setting
func (c *Cluster) reconcileAutoFailoverSettings() error {

	// Get the existing settings
	failoverSettings, err := c.client.GetAutoFailoverSettings(c.readyMembers())
	if err != nil {
		c.logger.Warnf("Failed to get auto-failover settings during reconcile: `%v` Will retry...", err)
		return err
	}

	// Marshal the CR spec into the same type as the existing failover settings
	clusterSettings := c.cluster.Spec.ClusterSettings
	specFailoverSettings := &cbmgr.AutoFailoverSettings{
		Enabled:  true,
		Timeout:  clusterSettings.AutoFailoverTimeout,
		MaxCount: clusterSettings.AutoFailoverMaxCount,
		FailoverOnDataDiskIssues: cbmgr.FailoverOnDiskFailureSettings{
			Enabled:    clusterSettings.AutoFailoverOnDataDiskIssues,
			TimePeriod: clusterSettings.AutoFailoverOnDataDiskIssuesTimePeriod,
		},
		FailoverServerGroup: clusterSettings.AutoFailoverServerGroup,
	}

	// Mask out any existing read only values, e.g. set it to the default value
	failoverSettings.Count = 0

	// NS server will not allow certain updates if a service is not enabled
	// which could result in spamming the server continuously with API update
	// requests when it refuses to obey our commands. Mask these out too if
	// irrelevant
	if !failoverSettings.FailoverOnDataDiskIssues.Enabled {
		failoverSettings.FailoverOnDataDiskIssues.TimePeriod = 0
	}
	if !specFailoverSettings.FailoverOnDataDiskIssues.Enabled {
		specFailoverSettings.FailoverOnDataDiskIssues.TimePeriod = 0
	}

	// Check to see if we need to reconcile
	if !reflect.DeepEqual(failoverSettings, specFailoverSettings) {
		err = c.client.SetAutoFailoverSettings(c.readyMembers(), specFailoverSettings)
		if err != nil {
			message := fmt.Sprintf("Failed to update autofailover settings: `%v`", err)
			c.logger.Warnf(message + " Will retry...")
			c.status.SetConfigRejectedCondition(message)
			return err
		} else {
			c.logger.Info("Updated autofailover settings")
		}
		c.raiseEvent(k8sutil.ClusterSettingsEditedEvent("autofailover", c.cluster))
	}

	return nil
}

// ensure memory quota's matche spec setting
func (c *Cluster) reconcileMemoryQuotaSettings() error {
	info, err := c.client.GetClusterInfo(c.readyMembers())
	if err != nil {
		c.logger.Warnf("Failed to get memory quotas/cluster name settings during reconcile: `%v` Will retry...", err)
		return err
	}

	current := info.PoolsDefaults()

	config := c.cluster.Spec.ClusterSettings
	requested := &cbmgr.PoolsDefaults{
		ClusterName:          c.cluster.Spec.ClusterSettings.ClusterName,
		DataMemoryQuota:      config.DataServiceMemQuota,
		IndexMemoryQuota:     config.IndexServiceMemQuota,
		SearchMemoryQuota:    config.SearchServiceMemQuota,
		EventingMemoryQuota:  config.EventingServiceMemQuota,
		AnalyticsMemoryQuota: config.AnalyticsServiceMemQuota,
	}

	if !reflect.DeepEqual(current, requested) {
		if err := c.client.SetPoolsDefault(c.readyMembers(), requested); err != nil {
			message := fmt.Sprintf("Unable update memory quota's [data:%d, index:%d, search:%d]: `%s`", config.DataServiceMemQuota, config.IndexServiceMemQuota, config.SearchServiceMemQuota, err.Error())
			c.logger.Warnf(message + " Will retry...")
			c.status.SetConfigRejectedCondition(message)
			return err
		} else {
			c.logger.Info("Updated memory quota settings or cluster name")
		}
		c.raiseEvent(k8sutil.ClusterSettingsEditedEvent("memory quota", c.cluster))
	}

	return nil
}

// reconcileSoftwareUpdateNotificationSettings looks to see if the UI displays software
// update notifications, and updates if different from the cluster specification.
func (c *Cluster) reconcileSoftwareUpdateNotificationSettings() error {
	actual, err := c.client.GetUpdatesEnabled(c.readyMembers())
	if err != nil {
		c.logger.Warnf("Failed to get software notification settings during reconcile: `%v` Will retry...", err)
		return err
	}

	requested := c.cluster.Spec.SoftwareUpdateNotifications
	if actual != requested {
		if err := c.client.SetUpdatesEnabled(c.readyMembers(), requested); err != nil {
			message := fmt.Sprintf("Unable update software notification settings: `%v`", err)
			c.logger.Warnf(message + " Will retry...")
			c.status.SetConfigRejectedCondition(message)
			return err
		} else {
			c.logger.Info("Updated software notification settings")
		}
		c.raiseEvent(k8sutil.ClusterSettingsEditedEvent("update notifications", c.cluster))
	}

	return nil
}

// Compare cluster index settings with spec, reconcile if necessary
func (c *Cluster) reconcileIndexStorageSettings() error {
	settings, err := c.client.GetIndexSettings(c.readyMembers(), c.username, c.password)
	if err != nil {
		c.logger.Warnf("Failed to get index settings during reconcile: `%v` Will retry...", err)
		return err
	}

	specStorageMode := c.cluster.Spec.ClusterSettings.IndexStorageSetting
	if specStorageMode != string(settings.StorageMode) {
		if err := c.client.SetIndexSettings(c.readyMembers(), c.username, c.password, specStorageMode, settings); err != nil {
			message := fmt.Sprintf("Unable set index storage mode to [%s]: %v", specStorageMode, err.Error())
			c.logger.Warnf(message + " Will retry...")
			c.status.SetConfigRejectedCondition(message)
			return err
		} else {
			c.logger.Info("Updated index settings")
		}
		c.raiseEvent(k8sutil.ClusterSettingsEditedEvent("index service", c.cluster))
	}

	return nil
}

// Verify that volumes are healthy for every
// member configured for persistence.
func (c *Cluster) verifyClusterVolumes() error {
	for _, memberName := range c.members.Names() {
		err := c.verifyMemberVolumes(c.members[memberName])
		if err != nil {

			// Raise an event detailing reason why volumes are unhealthy
			ev := k8sutil.MemberVolumeUnhealthyEvent(memberName, err.Error(), c.cluster)
			c.raiseEventCached(ev)
			return fmt.Errorf("%s", ev.Message)
		}
	}
	return nil
}

// Verify volumes of a single member
func (c *Cluster) verifyMemberVolumes(m *couchbaseutil.Member) error {
	config := c.cluster.Spec.GetServerConfigByName(m.ServerConfig)
	if config == nil {
		// Server class configuration has been deleted, and the member will too
		return nil
	}

	err := k8sutil.IsPodRecoverable(c.config.KubeCli, *config, m.Name, c.cluster.Name, c.cluster.Namespace)
	if err != nil {
		if _, ok := err.(cberrors.ErrNoVolumeMounts); ok {
			// Pod is not configured for volumes
			return nil
		}
		return err
	}
	return nil
}

// Check error the response from rebalance request (RetryError) and detect
// detect whether cause was due to inability to perform delta recovery
func (c *Cluster) didDeltaRecoveryFail(err error) bool {
	return retryutil.DidServerErrorOccurOnRetry(err, cbmgr.DeltaRecoveryNotPossible)
}

// Gets paths to use when initializing data, index, and analytics service.
// Default paths are used unless a claim is specified for the service in which
// case a custom mount path is used
func getServiceDataPaths(mounts *api.VolumeMounts) (string, string, []string) {
	dataPath := constants.DefaultDataPath
	indexPath := constants.DefaultDataPath
	analyticsPaths := []string{}
	if mounts != nil && mounts.LogsOnly() == false {
		if mounts.DataClaim != "" {
			dataPath = k8sutil.CouchbaseVolumeMountDataDir
		}
		if mounts.IndexClaim != "" {
			indexPath = k8sutil.CouchbaseVolumeMountIndexDir
		}
		if len(mounts.AnalyticsClaims) > 0 {
			analyticsPaths = mounts.GetAnalyticsVolumePaths()
		}
	}
	return dataPath, indexPath, analyticsPaths
}

// needsUpgrade does an ordered walk down the list of members, if a member is not
// the correct version then return it as an upgrade canididate  It also returns the
// counts of members in the various versions.
func (c *Cluster) needsUpgrade() (candidate *couchbaseutil.Member, target int) {
	version := c.cluster.Spec.Version
	// Names returns a sorted list for determinism.
	for _, member := range c.members.Names() {
		// Book keep the target version count for status reporting and
		// update the candidate with the first we find
		if c.members[member].Version == version {
			target++
		} else if candidate == nil {
			candidate = c.members[member]
		}
	}
	return
}

// reportUpgrade looks at the current state and any existing upgrade status
// condition, makes condition updates and raises events.
func (c *Cluster) reportUpgrade(status *api.UpgradeStatus) {
	// Look for an existing condition
	condition := c.status.GetCondition(api.ClusterConditionUpgrading)

	if condition == nil {
		// No existing condition, we are guaranteed to be upgrading.
		status.State = api.UpgradingMessageStateUpgrading
		c.raiseEvent(k8sutil.UpgradeStartedEvent(status.Source, status.Target, c.cluster))
	} else {
		// There is an existing condition, check to see which way we are going.
		// If we have switched directions we will need a new event.
		oldStatus := api.NewUpgradeStatus(condition.Message)
		if status.Target != oldStatus.Target {
			version, _ := couchbaseutil.NewVersion(status.Target)
			oldVersion, _ := couchbaseutil.NewVersion(oldStatus.Target)
			switch version.Compare(oldVersion) {
			case 1:
				// Upgrading
				status.State = api.UpgradingMessageStateUpgrading
				c.raiseEvent(k8sutil.UpgradeStartedEvent(status.Source, status.Target, c.cluster))
			case -1:
				// Rolling back
				status.State = api.UpgradingMessageStateRollback
				c.raiseEvent(k8sutil.RollbackStartedEvent(status.Source, status.Target, c.cluster))
			}
		} else {
			status.State = oldStatus.State
		}
	}

	c.status.SetUpgradingCondition(status)
	c.updateCRStatus()
}

// reportUpgradeComplete is called unconditionally when the reconcile is complete.
// If there was an unpgrade condition and the cluster no longer needs an upgrade clear
// the condition and raise any necessary events.
func (c *Cluster) reportUpgradeComplete() {
	// Still upgrading do nothing
	if candidate, _ := c.needsUpgrade(); candidate != nil {
		return
	}

	// There is no condition, we weren't upgrading, do nothing
	condition := c.status.GetCondition(api.ClusterConditionUpgrading)
	if condition == nil {
		return
	}

	status := api.NewUpgradeStatus(condition.Message)
	switch status.State {
	case api.UpgradingMessageStateUpgrading:
		c.raiseEvent(k8sutil.UpgradeFinishedEvent(status.Source, status.Target, c.cluster))
	case api.UpgradingMessageStateRollback:
		c.raiseEvent(k8sutil.RollbackFinishedEvent(status.Source, status.Target, c.cluster))
	}

	c.status.ClearCondition(api.ClusterConditionUpgrading)
	c.status.CurrentVersion = c.cluster.Spec.Version
	c.updateCRStatus()
}

// reconcileReadiness marks the pods as "ready" once everything is repaired, scaled and
// balanced.  It also reconciles a pod disruption budget so we only tolerate a certain
// number of evictions during a drain i.e. k8s upgrade.
func (c *Cluster) reconcileReadiness() error {
	for name, _ := range c.members {
		if err := k8sutil.FlagPodReady(c.config.KubeCli, c.cluster.Namespace, name); err != nil {
			return err
		}
	}
	return k8sutil.ReconcilePDB(c.config.KubeCli, c.cluster)
}
