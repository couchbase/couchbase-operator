package cluster

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"
	cberrors "github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/netutil"
	"github.com/couchbase/gocbmgr"
	"k8s.io/api/core/v1"
)

func (c *Cluster) reconcile(pods []*v1.Pod) error {
	c.logger.Infoln("Start reconciling")
	defer c.logger.Infoln("Finish reconciling")

	status, err := c.client.GetClusterStatus(c.members)
	if err != nil {
		c.logger.Warnf("Unable to get cluster state, skiping reconcile loop: %s", err.Error())
		return nil
	}

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

	state := &ReconcileMachine{
		runningPods: podsToMemberSet(pods, c.isSecureClient()),
		knownNodes:  couchbaseutil.NewMemberSet(),
		ejectNodes:  couchbaseutil.NewMemberSet(),
		couchbase:   status,
		state:       ReconcileInit,
	}

	c.reconcileClusterSettings()

	if !c.reconcileMembers(state) {
		return nil
	}

	if !c.reconcileBuckets() {
		return nil
	}

	c.reconcileAdminService()

	if err := c.reconcileExposedFeatures(); err != nil {
		return err
	}

	if err := c.reconcileMemberAlternateAddresses(); err != nil {
		return err
	}

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
func (c *Cluster) reconcileMembers(rm *ReconcileMachine) bool {
	done := false
	for !done {
		switch rm.state {
		case ReconcileInit:
			rm.handleInit(c)
		case ReconcileUnknownMembers:
			rm.handleUnknownMembers(c)
		case ReconcileRebalanceCheck:
			rm.handleRebalanceCheck(c)
		case ReconcileDownNodes:
			rm.handleDownNodes(c)
		case ReconcileUnclusteredNodes:
			rm.handleUnclusteredNodes(c)
		case ReconcileFailedAddNodes:
			rm.handleFailedAddNodes(c)
		case ReconcileAddBackNodes:
			rm.handleAddBackNodes(c)
		case ReconcileFailedNodes:
			rm.handleFailedNodes(c)
		case ReconcileServerConfigs:
			rm.handleUnknownServerConfigs(c)
		case ReconcileRemoveNodes:
			rm.handleRemoveNode(c)
		case ReconcileRemoveUnmanaged:
			rm.handleUnmanagedNodes(c)
		case ReconcileAddNodes:
			rm.handleAddNode(c)
		case ReconcileRebalance:
			rm.handleRebalance(c)
		case ReconcileDeadMembers:
			rm.handleDeadMembers(c)
		case ReconcileFinished:
			rm.handleFinished(c)
			done = true
		default:
			panic("Invalid state\n")
		}

		if err := c.updateCRStatus(); err != nil {
			c.logger.Warnf("update CR status failed: %v", err)
		}
	}

	return !rm.errored
}

// Create a new Couchbase cluster member
func (c *Cluster) createMember(serverSpec api.ServerConfig) (*couchbaseutil.Member, error) {
	// Allocate and create a new member
	newMember := c.newMember(c.memberCounter, serverSpec.Name)
	if err := c.createPod(newMember, serverSpec); err != nil {
		return nil, fmt.Errorf("fail to create member's pod (%s): %v", newMember.Name, err)
	}

	// Synchronize on pod creation and service availability
	ctx, cancel := context.WithTimeout(c.ctx, 300*time.Second)
	defer cancel()
	if err := k8sutil.WaitForPod(ctx, c.config.KubeCli, c.cluster.Namespace, newMember.Name, newMember.HostURL()); err != nil {
		if _, failed := err.(cberrors.ErrPodUnschedulable); failed {
			// remove pod
			c.removePod(newMember.Name)
		}
		return nil, err
	}

	// Increment the next member counter
	c.memberCounter++

	// Notify that we have created a new member
	c.clusterAddMember(newMember)
	if err := c.updateCRStatus(); err != nil {
		return nil, err
	}

	// The new node will not be part of the cluster yet  so the API calls will fail
	// when checking the UUID, temporarily disable these checks while installing
	// TLS configuration
	c.client.SetUUID("")
	defer c.client.SetUUID(c.status.ClusterID)

	// Enable TLS if requested
	if err := c.initMemberTLS(newMember, c.cluster.Spec); err != nil {
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

	// If something stopped a rebalance we must return an error condition to
	// prevent the reconcile loop/upgrade from deleting any managed nodes or
	// this will result in data loss or a terminal cluster condition
	for name, _ := range managed {
		if !status.NodeInState(name, couchbaseutil.NodeStateUnclustered) {
			c.raiseEvent(k8sutil.RebalanceIncompleteEvent(c.cluster))
			return fmt.Errorf("node %s is still in the cluster", name)
		}
	}

	// Conversely check that everything is in that should be.  This ensures we
	// don't report a balanced state if a node died before it was balanced in
	for name, _ := range c.members.Diff(managed) {
		if !status.NodeInState(name, couchbaseutil.NodeStateActive) {
			c.raiseEvent(k8sutil.RebalanceIncompleteEvent(c.cluster))
			return fmt.Errorf("node %s is not in the cluster", name)
		}
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
func (c *Cluster) reconcileBuckets() bool {
	existingBuckets, err := c.client.GetBucketNames(c.members)
	if err != nil {
		c.logger.Warnf("Unable to get buckets from cluster: %s", err.Error())
		return false
	}

	// when reconciling buckets, any buckets in cluster
	// created outside of operator are removed,
	// and any buckets removed by user are recreated
	// if still present in active spec
	spec := c.cluster.Spec
	bucketsToAdd, bucketsToRemove := spec.BucketDiff(existingBuckets)
	bucketsToEdit, err := c.client.GetBucketsToEdit(c.members, &spec)
	if err != nil {
		c.logger.Warnf("Unable to get list of buckets to edit: %s", err.Error())
		return false
	}

	if len(bucketsToRemove) > 0 {
		bucketName := bucketsToRemove[0]
		err := c.deleteClusterBucket(bucketName)
		if err != nil {
			msg := fmt.Sprintf("Bucket: %s %s", bucketName, err.Error())
			c.status.SetBucketManagementFailedCondition("Bucket delete failed", msg)
			c.logger.Warnf("Unable to delete bucket named - %s: %s", bucketName, err.Error())
			return false
		}
		c.logger.Infof("Removed bucket %s", bucketName)
	} else if len(bucketsToEdit) > 0 {
		bucketName := bucketsToEdit[0]
		err := c.editClusterBucket(bucketName)
		if err != nil {
			msg := fmt.Sprintf("Bucket: %s %s", bucketName, err.Error())
			c.status.SetBucketManagementFailedCondition("Bucket edit failed", msg)
			c.logger.Warnf("Unable to edit bucket named - %s: %s", bucketName, err.Error())
			return false
		}
		c.logger.Infof("Edited bucket %s", bucketName)
	} else if len(bucketsToAdd) > 0 {
		bucketName := bucketsToAdd[0]
		err := c.createClusterBucket(bucketName)
		if err != nil {
			msg := fmt.Sprintf("Bucket: %s %s", bucketName, err.Error())
			c.status.SetBucketManagementFailedCondition("Bucket add failed", msg)
			c.logger.Warnf("Unable to create bucket named - %s: %s", bucketName, err.Error())
			return false
		}
		c.logger.Infof("Created bucket %s", bucketName)
	}

	c.status.ClearCondition(api.ClusterConditionManageBuckets)

	return true
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
	status, err := k8sutil.UpdateExposedFeatures(c.config.KubeCli, c.cluster, &c.status)
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
	err := c.client.CreateBucket(c.members, config)
	if err == nil {
		c.status.UpdateBuckets(bucketName, config)
		c.raiseEvent(k8sutil.BucketCreateEvent(bucketName, c.cluster))
	}
	return err
}

func (c *Cluster) deleteClusterBucket(bucketName string) error {
	err := c.client.DeleteBucket(c.members, bucketName)
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
	err := c.client.EditBucket(c.members, config)
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

// initializes member with cluster settings
func (c *Cluster) initMember(m *couchbaseutil.Member, serverSpec api.ServerConfig) error {
	settings := c.cluster.Spec.ClusterSettings
	if err := c.client.InitializeCluster(m, c.username, c.password, c.cluster.Name,
		settings.DataServiceMemQuota, settings.IndexServiceMemQuota, settings.SearchServiceMemQuota,
		serverSpec.Services, serverSpec.DataPath, serverSpec.IndexPath, settings.IndexStorageSetting); err != nil {
		return err
	}

	// enables autofailover by default
	return c.client.SetAutoFailoverTimeout(c.members, true, settings.AutoFailoverTimeout)
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
			return err
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
					ViewServicePort:     ports.ViewServicePort,
					ViewServicePortTLS:  ports.ViewServicePortTLS,
					//QueryServicePort:        ports.QueryServicePort,
					//QueryServicePortTLS:     ports.QueryServicePortTLS,
					//FtsServicePort:          ports.FtsServicePort,
					//FtsServicePortTLS:       ports.FtsServicePortTLS,
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

// Remove all pods from running set that does not belong to member set.
func (c *Cluster) removeUnknownMembers(running couchbaseutil.MemberSet) (couchbaseutil.MemberSet, error) {
	unknownMembers := running.Diff(c.members)
	if unknownMembers.Size() > 0 {
		c.logger.Infof("removing unexpected pods: %v", unknownMembers)
		for _, m := range unknownMembers {
			if err := c.removePod(m.Name); err != nil {
				return running, err
			}
		}
	}
	return running.Diff(unknownMembers), nil
}

func (c *Cluster) reconcileClusterSettings() bool {

	if ok := c.reconcileAutoFailoverSettings(); !ok {
		return false
	}
	if ok := c.reconcileMemoryQuotaSettings(); !ok {
		return false
	}
	if ok := c.reconcileSoftwareUpdateNotificationSettings(); !ok {
		return false
	}
	if ok := c.reconcileIndexStorageSettings(); !ok {
		return false
	}

	c.status.ClearCondition(api.ClusterConditionManageConfig)
	return true
}

// ensure autofailover timeout matches spec setting
func (c *Cluster) reconcileAutoFailoverSettings() bool {
	failoverSettings, err := c.client.GetAutoFailoverSettings(c.members)
	if err != nil {
		c.logger.Warnf("Unable to get auto failover settings: %s", err.Error())
		return false
	}

	clusterSettings := c.cluster.Spec.ClusterSettings
	if (failoverSettings.Timeout != clusterSettings.AutoFailoverTimeout) ||
		(failoverSettings.Enabled != true) {

		// reset autofailover timeout
		err = c.client.SetAutoFailoverTimeout(c.members, true, clusterSettings.AutoFailoverTimeout)
		if err != nil {
			message := fmt.Sprintf("Unable to set autofailover timeout to %d: %s", clusterSettings.AutoFailoverTimeout, err.Error())
			c.status.SetConfigRejectedCondition(message)
			c.logger.Warnf(message)
			return false
		}
	}

	return true
}

// ensure memory quota's matche spec setting
func (c *Cluster) reconcileMemoryQuotaSettings() bool {
	info, err := c.client.GetClusterInfo(c.members)
	if err != nil {
		c.logger.Warnf("Unable to get cluster info: %s", err.Error())
		return false
	}

	config := c.cluster.Spec.ClusterSettings
	if config.DataServiceMemQuota != info.DataMemoryQuotaMB ||
		config.IndexServiceMemQuota != info.IndexMemoryQuotaMB ||
		config.SearchServiceMemQuota != info.SearchMemoryQuotaMB {
		err = c.client.SetPoolsDefault(c.members, config.DataServiceMemQuota, config.IndexServiceMemQuota, config.SearchServiceMemQuota)
		if err != nil {
			message := fmt.Sprintf("Unable update memory quota's [data:%d, index:%d, search:%d]: %s", config.DataServiceMemQuota, config.IndexServiceMemQuota, config.SearchServiceMemQuota, err.Error())
			c.status.SetConfigRejectedCondition(message)
			c.logger.Warnf(message)
			return false
		}
	}

	return true
}

// reconcileSoftwareUpdateNotificationSettings looks to see if the UI displays software
// update notifications, and updates if different from the cluster specification.
func (c *Cluster) reconcileSoftwareUpdateNotificationSettings() bool {
	actual, err := c.client.GetUpdatesEnabled(c.members)
	if err != nil {
		c.logger.Warnf("Unable to get cluster software updates: %v", err)
		return false
	}

	requested := c.cluster.Spec.SoftwareUpdateNotifications
	if actual != requested {
		if err := c.client.SetUpdatesEnabled(c.members, requested); err != nil {
			c.logger.Warnf("Unable to set cluster software updates: %v", err)
			return false
		}
	}

	return true
}

// Compare cluster index settings with spec, reconcile if necessary
func (c *Cluster) reconcileIndexStorageSettings() bool {
	settings, err := c.client.GetIndexSettings(c.members, c.username, c.password)
	if err != nil {
		c.logger.Warnf("Unable to get index storage settings: %v", err)
		return false
	}

	specStorageMode := c.cluster.Spec.ClusterSettings.IndexStorageSetting
	if specStorageMode != string(settings.StorageMode) {
		if err := c.client.SetIndexSettings(c.members, c.username, c.password, specStorageMode, settings); err != nil {
			emsg := fmt.Sprintf("Unable set index storage mode to [%s]: %v", specStorageMode, err.Error())
			c.status.SetConfigRejectedCondition(emsg)
			c.logger.Warnf(emsg)
			return false
		}
	}

	return true
}
