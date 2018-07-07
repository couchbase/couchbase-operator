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

	status, err := c.client.GetClusterStatus(c.members)
	if err != nil {
		c.logger.Warnf("Unable to get cluster state, skiping reconcile loop: %s", err.Error())
		return nil
	}

	if c.config.EnableUpgrades {
		// Upgrading must override reconciliation as during the process the
		// cluster will be oversized and we don't want nodes to disappear ...
		upgrading := c.upgrading()
		if err := c.upgrade(status); err != nil {
			return err
		}
		// ... addtionally if we were previously or currently are upgrading then
		// state may have altered and reality may not match 'pods' without
		// probing the API again
		if upgrading || c.upgrading() {
			return nil
		}
	} else {
		if c.cluster.Spec.Version != c.status.CurrentVersion {
			return fmt.Errorf("cluster upgrades are unsupported")
		}
	}

	state := &ReconcileMachine{
		runningPods: podsToMemberSet(pods, c.isSecureClient()),
		knownNodes:  couchbaseutil.NewMemberSet(),
		ejectNodes:  couchbaseutil.NewMemberSet(),
		couchbase:   status,
		state:       ReconcileInit,
	}

	if err := c.reconcileClusterSettings(); err != nil {
		c.logger.Warnf("%s", err.Error())
		return err
	}

	if err := c.reconcileMembers(state); err != nil {
		return err
	}

	if err := c.reconcileBuckets(); err != nil {
		return err
	}

	if err := c.verifyClusterVolumes(); err != nil {
		return err
	}

	c.reconcileAdminService()

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

// Create a new Couchbase cluster member
func (c *Cluster) createMember(serverSpec api.ServerConfig) (m *couchbaseutil.Member, err error) {
	// Allocate an index to be used in the name.  Get the current index then increment
	// and commit back to etcd.  That way we are guaranteed to never have conflicting
	// names
	index := c.getPodIndex()
	c.incPodIndex()

	// Create a new member
	newMember := c.newMember(index, serverSpec.Name)

	// Prepare to delete member Pod if any errors
	// occur during creation or configuration
	defer func() {
		if err != nil {
			c.decPodIndex()
			c.removePod(newMember.Name)
		}
	}()

	if err := c.createPod(newMember, serverSpec); err != nil {
		return nil, fmt.Errorf("fail to create member's pod (%s): %v", newMember.Name, err)
	}

	// Synchronize on pod creation and service availability
	ctx, cancel := context.WithTimeout(c.ctx, 300*time.Second)
	defer cancel()
	if err := k8sutil.WaitForPod(ctx, c.config.KubeCli, c.cluster.Namespace, newMember.Name, newMember.HostURL()); err != nil {
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
func (c *Cluster) destroyMember(name string) error {
	if err := c.removePod(name); err != nil {
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

	// Error checking...
	status, err := c.client.GetClusterStatus(c.members)
	if err != nil {
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

	if len(bucketsToRemove) > 0 {
		bucketName := bucketsToRemove[0]
		err := c.deleteClusterBucket(bucketName)
		if err != nil {
			msg := fmt.Sprintf("Bucket: %s %s", bucketName, err.Error())
			c.status.SetBucketManagementFailedCondition("Bucket delete failed", msg)
			return fmt.Errorf("Unable to delete bucket named - %s: %v", bucketName, err)
		}
		c.logger.Infof("Removed bucket %s", bucketName)
	} else if len(bucketsToEdit) > 0 {
		bucketName := bucketsToEdit[0]
		err := c.editClusterBucket(bucketName)
		if err != nil {
			msg := fmt.Sprintf("Bucket: %s %s", bucketName, err.Error())
			c.status.SetBucketManagementFailedCondition("Bucket edit failed", msg)
			return fmt.Errorf("Unable to edit bucket named - %s: %v", bucketName, err)
		}
		c.logger.Infof("Edited bucket %s", bucketName)
	} else if len(bucketsToAdd) > 0 {
		bucketName := bucketsToAdd[0]
		err := c.createClusterBucket(bucketName)
		if err != nil {
			msg := fmt.Sprintf("Bucket: %s %s", bucketName, err.Error())
			c.status.SetBucketManagementFailedCondition("Bucket add failed", msg)
			return fmt.Errorf("Unable to create bucket named - %s: %v", bucketName, err)
		}
		c.logger.Infof("Created bucket %s", bucketName)
	}

	c.status.ClearCondition(api.ClusterConditionManageBuckets)

	return nil
}

// reconcile changes to selected pod labels for
// the nodePort service exposing admin console
func (c *Cluster) reconcileAdminService() {

	svcName := k8sutil.AdminServiceName(c.cluster.Name)
	svc, err := k8sutil.GetService(c.config.KubeCli, svcName, c.cluster.Namespace, nil)

	if (err == nil) && !c.cluster.Spec.ExposeAdminConsole {
		// deleting admin service
		err = c.deleteUIService(svcName)
		if err != nil {
			c.logger.Warnf("Error occured deleting admin service: %s", err.Error())
		} else {
			c.raiseEvent(k8sutil.AdminConsoleSvcDeleteEvent(svc.Name, c.cluster))
		}
		return
	}

	// create service if it doesn't exist and new expose requested
	desiredServices := c.cluster.Spec.AdminConsoleServices
	if k8sutil.IsKubernetesResourceNotFoundError(err) {
		if c.cluster.Spec.ExposeAdminConsole {
			svc, err = c.createUIService(desiredServices)
			if err != nil {
				c.logger.Warnf("Error occured creating admin service: %s", err.Error())
			} else {
				c.raiseEvent(k8sutil.AdminConsoleSvcCreateEvent(svc.Name, c.cluster))
			}
		}
		return
	} else if err != nil {
		c.logger.Warnf("Unable to get admin service: %s", err.Error())
		return
	}

	desiredSelector := k8sutil.LabelsForAdminConsole(c.cluster.Name, desiredServices)
	if !reflect.DeepEqual(svc.Spec.Selector, desiredSelector) {
		// update admin service
		svc.Spec.Selector = desiredSelector
		if _, err = k8sutil.UpdateService(c.config.KubeCli, c.cluster.Namespace, svc); err != nil {
			c.logger.Warnf("Error occured updating admin service: %s", err.Error())
		}
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
	if mounts := serverSpec.GetVolumeMounts(); mounts != nil {
		if mounts.DataClaim != "" {
			dataPath = k8sutil.CouchbaseVolumeMountDataDir
		}
		if mounts.IndexClaim != "" {
			indexPath = k8sutil.CouchbaseVolumeMountIndexDir
		}
	}

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
			pem, ok := secret.Data[tlsOperatorSecretCACert]
			if !ok {
				return fmt.Errorf("unable to find %s in operator secret", tlsOperatorSecretCACert)
			}

			// Update Couchbase's TLS configuration
			if err := c.client.UploadClusterCACert(m, pem); err != nil {
				return err
			}
			if err := c.client.ReloadNodeCert(m); err != nil {
				return err
			}

			// TODO: Not available until >=5.5.0, even then does authz which we don't want :(
			//settings := &cbmgr.ClientCertAuth{
			//	State: "mandatory",
			//}
			//if err := c.client.SetClientCertAuth(m, settings); err != nil {
			//	return err
			//}

			// Indicate that comms with this member are now encrypted
			m.SecureClient = true

			// Wait for the port to come backup with the correct certificate chain
			ctx, cancel := context.WithTimeout(c.ctx, 60*time.Second)
			defer cancel()
			if err := netutil.WaitForHostPortTLS(ctx, m.HostURL(), pem); err != nil {
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

// initMemberAlternateAddresses injects the K8S node's L3 address and alternate
// ports into the requested member.  Clients may use these addresses/ports to
// connect to the cluster if there is no direct L3 connectivity into the pod
// network.
func (c *Cluster) reconcileMemberAlternateAddresses() error {
	// Examine each member in turn as they will have different node
	// addresses (i.e. you must be using anti affinity or kubernetes
	// has no way of addressing individual cluster nodes).
	for memberName, member := range c.members {
		// Grab the current configuration
		existingAddresses, err := c.client.GetAlternateAddressesExternal(member)
		if err != nil {
			// If we cannot make contact then just continue, it may have been deleted
			c.logger.Warnf("unable to poll external addresses for pod %s", member.Name)
			return nil
		}

		// Calculate what the current state of the node's alternate addresses
		// should be.
		//
		// Unfortunately clients outside of the pod network are not able to access
		// all services, so we need to moderate what we set as externally available.
		//
		// See the following for guidance:
		// https://github.com/couchbase/ns_server/blob/6071474b0bd8d625955b60e0ee94802d66e47cfc/src/menelaus_web_node.erl#L281
		hostname, err := k8sutil.GetHostIP(c.config.KubeCli, c.cluster.Namespace, member.Name)
		if err != nil {
			return err
		}

		// Marshal status data into manager library format taking note if any
		// ports are actually set
		addresses := &cbmgr.AlternateAddressesExternal{}
		anyPortSet := false
		if ports, ok := c.status.ExposedPorts[memberName]; ok {
			addresses = &cbmgr.AlternateAddressesExternal{
				Hostname: hostname,
				Ports: cbmgr.AlternateAddressesExternalPorts{
					AdminServicePort:    ports.AdminServicePort,
					AdminServicePortTLS: ports.AdminServicePortTLS,
					// TODO: rename the library for consistency
					ViewServicePort:    ports.IndexServicePort,
					ViewServicePortTLS: ports.IndexServicePortTLS,
					//QueryServicePort:        ports.QueryServicePort,
					//QueryServicePortTLS:     ports.QueryServicePortTLS,
					//FtsServicePort:          ports.SearchServicePort,
					//FtsServicePortTLS:       ports.SearchServicePortTLS,
					//AnalyticsServicePort:    ports.AnalyticsServicePort,
					//AnalyticsServicePortTLS: ports.AnalyticsServicePortTLS,
					DataServicePort: ports.DataServicePort,
					//DataServicePortTLS:      ports.DataServicePortTLS,
				},
			}
			ports := reflect.ValueOf(addresses.Ports)
			for i := 0; i < ports.NumField(); i++ {
				value := ports.Field(i)
				if value.Int() != 0 {
					anyPortSet = true
					break
				}
			}
		}

		// If no ports are set, but the server reports the hostname is set we have
		// existing configuration which needs to be deleted.  If the hostname is
		// not set then there is no configuration to worry about
		if !anyPortSet {
			if existingAddresses.Hostname != "" {
				if err := c.client.DeleteAlternateAddressesExternal(member); err != nil {
					return err
				}
			}
			continue
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
	for memberName, member := range c.members {
		// Grab the current configuration
		existingAddresses, err := c.client.GetAlternateAddressesExternal(member)
		if err != nil {
			// If we cannot make contact then just continue, it may have been deleted
			return false, nil
		}

		// Calculate what the current state of the node's alternate addresses
		// should be.
		//
		// Unfortunately clients outside of the pod network are not able to access
		// all services, so we need to moderate what we set as externally available.
		//
		// See the following for guidance:
		// https://github.com/couchbase/ns_server/blob/6071474b0bd8d625955b60e0ee94802d66e47cfc/src/menelaus_web_node.erl#L281
		hostname, err := k8sutil.GetHostIP(c.config.KubeCli, c.cluster.Namespace, member.Name)
		if err != nil {
			return false, err
		}

		// Marshal status data into manager library format taking note if any
		// ports are actually set
		addresses := &cbmgr.AlternateAddressesExternal{}
		anyPortSet := false
		if ports, ok := c.status.ExposedPorts[memberName]; ok {
			addresses = &cbmgr.AlternateAddressesExternal{
				Hostname: hostname,
				Ports: cbmgr.AlternateAddressesExternalPorts{
					AdminServicePort:    ports.AdminServicePort,
					AdminServicePortTLS: ports.AdminServicePortTLS,
					// TODO: rename the library for consistency
					ViewServicePort:    ports.IndexServicePort,
					ViewServicePortTLS: ports.IndexServicePortTLS,
					//QueryServicePort:        ports.QueryServicePort,
					//QueryServicePortTLS:     ports.QueryServicePortTLS,
					//FtsServicePort:          ports.SearchServicePort,
					//FtsServicePortTLS:       ports.SearchServicePortTLS,
					//AnalyticsServicePort:    ports.AnalyticsServicePort,
					//AnalyticsServicePortTLS: ports.AnalyticsServicePortTLS,
					DataServicePort: ports.DataServicePort,
					//DataServicePortTLS:      ports.DataServicePortTLS,
				},
			}
			ports := reflect.ValueOf(addresses.Ports)
			for i := 0; i < ports.NumField(); i++ {
				value := ports.Field(i)
				if value.Int() != 0 {
					anyPortSet = true
					break
				}
			}
		}

		// If no ports are set, but the server reports the hostname is set we have
		// existing configuration which needs to be deleted.  If the hostname is
		// not set then there is no configuration to worry about
		if !anyPortSet {
			if existingAddresses.Hostname != "" {
				return true, nil
			}
			continue
		}

		// Check to see if we need to perform any updates, ignoring if not
		if !reflect.DeepEqual(addresses, existingAddresses) {
			return true, nil
		}
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
	if mounts != nil {
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
