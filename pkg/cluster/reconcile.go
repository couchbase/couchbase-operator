package cluster

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/cluster/persistence"
	cberrors "github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/netutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/pkg/util/scheduler"
	"github.com/couchbase/gocbmgr"

	"github.com/google/go-cmp/cmp"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (c *Cluster) reconcile(pods []*v1.Pod) error {
	log.V(1).Info("Reconciliation starting", "cluster", c.namespacedName())
	defer log.V(1).Info("Reconciliation completed", "cluster", c.namespacedName())

	// Persistent data may need to be reflected in the status.
	if err := c.reconcilePersistentStatus(); err != nil {
		return err
	}

	// Establish DNS for cluster communication.
	if err := k8sutil.ReconcilePeerServices(c.kubeClient, c.cluster.Namespace, c.cluster.Name, c.cluster.AsOwner()); err != nil {
		return err
	}

	// First thing we must do is fix up TLS or we may not be able to talk to the
	// cluster for anything else.
	if err := c.reconcileTLS(); err != nil {
		return err
	}

	// Initialize the scheduler each time around, this saves us having to update
	// internal state in all the cases when a pod fails to be created, deleted,
	// or disappears
	var err error
	if c.scheduler, err = scheduler.New(c.kubeClient, c.cluster); err != nil {
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
		log.Error(err, "Cluster state collection failed, skipping", "cluster", c.namespacedName())
		return err
	}

	if err := c.updateMemberStatusWithClusterInfo(status); err != nil {
		return err
	}

	state := &ReconcileMachine{
		runningPods:   podsToMemberSet(pods),
		knownNodes:    couchbaseutil.NewMemberSet(),
		ejectNodes:    couchbaseutil.NewMemberSet(),
		couchbase:     status,
		state:         ReconcileInit,
		removeVolumes: make(map[string]bool),
	}

	if err := c.reconcileClusterSettings(); err != nil {
		return err
	}

	if err := c.reconcileMembers(state); err != nil {
		return err
	}

	// Update the status and ready list to reflect any new members added.
	if status, err = c.client.GetClusterStatus(c.members); err != nil {
		return err
	}

	if err := c.updateMemberStatusWithClusterInfo(status); err != nil {
		return err
	}

	if err := c.reconcileBuckets(); err != nil {
		return err
	}

	if err := c.reconcileXDCR(); err != nil {
		return err
	}

	if err := c.reconcileReadiness(); err != nil {
		return err
	}

	if err := c.reconcileAdminService(); err != nil {
		return err
	}

	if err := c.reconcileUsers(); err != nil {
		return err
	}

	if err := c.reportUpgradeComplete(); err != nil {
		return err
	}

	c.cluster.Status.Size = c.members.Size()
	c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionScaling)
	c.cluster.Status.SetReadyCondition()

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
// 6. Run a rebalance if necessary.
// 7. Remove any nodes from the cached member set that are not part actually
//    part of the cluster.
// Returns true if reconciliation completed properly
func (c *Cluster) reconcileMembers(rm *ReconcileMachine) error {
	return rm.exec(c)
}

// logFailedMember outputs any debug information we can about a failed member creation.
func (c *Cluster) logFailedMember(name string) {
	log.Info("Member creation failed", "cluster", c.namespacedName(), "name", name, "resource", k8sutil.LogPod(c.kubeClient, c.cluster.Namespace, name))
}

// Create a new Couchbase cluster member
func (c *Cluster) createMember(serverSpec couchbasev2.ServerConfig) (m *couchbaseutil.Member, err error) {
	// The pod creation timeout is global across this operation e.g. PVCs, pods, the lot.
	podCreateTimeout, err := time.ParseDuration(c.config.PodCreateTimeout)
	if err != nil {
		return nil, fmt.Errorf("PodCreateTimeout improperly formatted: %v", err)
	}
	ctx, cancel := context.WithTimeout(c.ctx, podCreateTimeout)
	defer cancel()

	// Allocate an index to be used in the name.  Get the current index then increment
	// and commit back to etcd.  That way we are guaranteed to never have conflicting
	// names
	index, err := c.getPodIndex()
	if err != nil {
		return nil, err
	}
	if err := c.incPodIndex(); err != nil {
		return nil, err
	}

	// Create a new member
	newMember, err := c.newMember(index, serverSpec.Name, c.cluster.Spec.Image)
	if err != nil {
		return nil, err
	}

	// Prepare to delete member Pod if any errors
	// occur during creation or configuration
	defer func() {
		if err != nil {
			_ = c.decPodIndex()
			// Deleting volumes, even log volumes
			// if node doesn't get to start
			_ = c.removePod(newMember.Name, true)
		}
	}()

	if err := c.createPod(ctx, newMember, serverSpec); err != nil {
		c.logFailedMember(newMember.Name)
		return nil, fmt.Errorf("fail to create member's pod (%s): %v", newMember.Name, err)
	}

	// Synchronize on pod creation and service availability
	if err := c.waitForCreatePod(ctx, newMember); err != nil {
		// We will delete the pod on error, so collect any ephemeral debug we can before
		// discarding it forever.  This will capture errors such as users specifying the
		// wrong image name (pull error), PVCs taking an age to become bound etc.
		c.logFailedMember(newMember.Name)
		c.raiseEventCached(k8sutil.MemberCreationFailedEvent(newMember.Name, c.cluster))
		return nil, err
	}

	// The new node will not be part of the cluster yet  so the API calls will fail
	// when checking the UUID, temporarily disable these checks while installing
	// TLS configuration
	c.client.SetUUID("")
	defer c.client.SetUUID(c.cluster.Status.ClusterID)

	// Check the pod is EE
	isEnterprise, err := c.client.IsEnterprise(newMember)
	if err != nil {
		return nil, err
	}
	if !isEnterprise {
		c.raiseEventCached(k8sutil.MemberCreationFailedEvent(newMember.Name, c.cluster))
		return nil, fmt.Errorf("couchbase server reports community edition")
	}

	// Enable TLS if requested
	if err := c.initMemberTLS(ctx, newMember, c.cluster.Spec); err != nil {
		c.raiseEventCached(k8sutil.MemberCreationFailedEvent(newMember.Name, c.cluster))
		return nil, err
	}

	// Initialize node paths
	dataPath, indexPath, analyticsPaths := getServiceDataPaths(serverSpec.GetVolumeMounts())
	if err := c.client.NodeInitialize(newMember, c.cluster.Name, dataPath, indexPath, analyticsPaths); err != nil {
		return nil, err
	}

	// Notify that we have created a new member
	if err := c.clusterAddMember(newMember); err != nil {
		return nil, err
	}

	if err := c.updateCRStatus(); err != nil {
		return nil, err
	}

	return newMember, nil
}

// Creates and adds a new Couchbase cluster member
func (c *Cluster) addMember(serverSpec couchbasev2.ServerConfig) (*couchbaseutil.Member, error) {
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

	log.Info("Pod added to cluster", "cluster", c.namespacedName(), "name", newMember.Name)
	c.raiseEvent(k8sutil.MemberAddEvent(newMember.Name, c.cluster))

	return newMember, nil
}

// Destroys a Couchbase cluster member
func (c *Cluster) destroyMember(name string, removeVolumes bool) error {
	if err := c.removePod(name, removeVolumes); err != nil {
		return err
	}

	// Notify of deletion
	if err := c.clusterRemoveMember(name); err != nil {
		return err
	}

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
	c.cluster.Status.SetUnbalancedCondition()
	if err := c.updateCRStatus(); err != nil {
		return err
	}

	// Perform the operation
	nodesToRemove := append(managed.HostURLsPlaintext(), unmanaged...)
	if err := c.client.Rebalance(c.members, nodesToRemove, true, c.namespacedName()); err != nil {
		c.raiseEvent(k8sutil.RebalanceIncompleteEvent(c.cluster))
		return err
	}

	// Error checking... perfom this a few times as there is a race between when
	// we check and when Server reports rebalance is complete.
	var status *couchbaseutil.ClusterStatus
	retryFunc := func() error {
		var err error
		status, err = c.client.GetClusterStatus(c.members)
		if err != nil {
			return err
		}
		if status.NeedsRebalance {
			return cberrors.NewRebalanceIncompleteError()
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
	defer cancel()
	if err := retryutil.RetryOnErr(ctx, time.Second, retryFunc); err != nil {
		c.raiseEvent(k8sutil.RebalanceIncompleteEvent(c.cluster))
		return err
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
	c.cluster.Status.SetBalancedCondition()
	if err := c.updateCRStatus(); err != nil {
		return err
	}
	return nil
}

// gatherBuckets loads up bucket configurations from Kubernetes and marshalls them into canonical form.
func (c *Cluster) gatherBuckets() ([]cbmgr.Bucket, error) {
	// TODO: Shared informers/cache please
	listOpts := metav1.ListOptions{}
	if c.cluster.Spec.Buckets.Selector != nil {
		listOpts.LabelSelector = metav1.FormatLabelSelector(c.cluster.Spec.Buckets.Selector)
	}
	couchbaseBuckets, err := c.couchbaseKubeClient.CouchbaseV2().CouchbaseBuckets(c.cluster.Namespace).List(listOpts)
	if err != nil {
		return nil, err
	}
	couchbaseEphemeralBuckets, err := c.couchbaseKubeClient.CouchbaseV2().CouchbaseEphemeralBuckets(c.cluster.Namespace).List(listOpts)
	if err != nil {
		return nil, err
	}
	couchbaseMemcachedBuckets, err := c.couchbaseKubeClient.CouchbaseV2().CouchbaseMemcachedBuckets(c.cluster.Namespace).List(listOpts)
	if err != nil {
		return nil, err
	}

	buckets := []cbmgr.Bucket{}
	for _, bucket := range couchbaseBuckets.Items {
		buckets = append(buckets, cbmgr.Bucket{
			BucketName:         bucket.Name,
			BucketType:         constants.BucketTypeCouchbase,
			BucketMemoryQuota:  bucket.Spec.MemoryQuota,
			BucketReplicas:     bucket.Spec.Replicas,
			IoPriority:         cbmgr.IoPriorityType(bucket.Spec.IoPriority),
			EvictionPolicy:     bucket.Spec.EvictionPolicy,
			ConflictResolution: bucket.Spec.ConflictResolution,
			EnableFlush:        bucket.Spec.EnableFlush,
			EnableIndexReplica: bucket.Spec.EnableIndexReplica,
			CompressionMode:    bucket.Spec.CompressionMode,
		})
	}
	for _, bucket := range couchbaseEphemeralBuckets.Items {
		buckets = append(buckets, cbmgr.Bucket{
			BucketName:         bucket.Name,
			BucketType:         constants.BucketTypeEphemeral,
			BucketMemoryQuota:  bucket.Spec.MemoryQuota,
			BucketReplicas:     bucket.Spec.Replicas,
			IoPriority:         cbmgr.IoPriorityType(bucket.Spec.IoPriority),
			EvictionPolicy:     bucket.Spec.EvictionPolicy,
			ConflictResolution: bucket.Spec.ConflictResolution,
			EnableFlush:        bucket.Spec.EnableFlush,
			CompressionMode:    bucket.Spec.CompressionMode,
		})
	}
	for _, bucket := range couchbaseMemcachedBuckets.Items {
		buckets = append(buckets, cbmgr.Bucket{
			BucketName:        bucket.Name,
			BucketType:        constants.BucketTypeMemcached,
			BucketMemoryQuota: bucket.Spec.MemoryQuota,
			EnableFlush:       bucket.Spec.EnableFlush,
		})
	}

	return buckets, nil
}

// inspectBuckets compares Kubernetes buckets with Couchbase buckets and returns lists
// of buckets to create, update or remove and the requested set for status updates.
func (c *Cluster) inspectBuckets() ([]cbmgr.Bucket, []cbmgr.Bucket, []cbmgr.Bucket, []cbmgr.Bucket, error) {
	requested, err := c.gatherBuckets()
	if err != nil {
		return nil, nil, nil, nil, err
	}

	actual, err := c.client.ListBuckets(c.readyMembers())
	if err != nil {
		return nil, nil, nil, nil, err
	}

	create := []cbmgr.Bucket{}
	update := []cbmgr.Bucket{}
	remove := []cbmgr.Bucket{}

	// Do an exhaustive search of requested buckets in the actual list, creating and
	// updating as necessary.
	for _, r := range requested {
		found := false
		for _, a := range actual {
			if r.BucketName == a.BucketName {
				if !reflect.DeepEqual(r, a) {
					update = append(update, r)
					c.logUpdate(a, r)
				}
				found = true
				break
			}
		}
		if !found {
			create = append(create, r)
		}
	}

	// Do an exhaustive search of actual buckets in the requested list, deleting
	// as necessary.
	for _, a := range actual {
		found := false
		for _, r := range requested {
			if a.BucketName == r.BucketName {
				found = true
				break
			}
		}
		if !found {
			remove = append(remove, a)
		}
	}

	return create, update, remove, requested, nil
}

// reconcile buckets by adding or removing
// buckets one at a time based on comparison
// of existing buckets to cluster spec
func (c *Cluster) reconcileBuckets() error {
	if !c.cluster.Spec.Buckets.Managed {
		return nil
	}

	create, update, remove, requested, err := c.inspectBuckets()
	if err != nil {
		return err
	}

	for _, bucket := range create {
		if err := c.client.CreateBucket(c.readyMembers(), bucket); err != nil {
			return err
		}
		log.Info("Bucket created", "cluster", c.namespacedName(), "name", bucket.BucketName)
		c.raiseEvent(k8sutil.BucketCreateEvent(bucket.BucketName, c.cluster))
	}
	for _, bucket := range update {
		if err := c.client.UpdateBucket(c.readyMembers(), bucket); err != nil {
			return err
		}
		log.Info("Bucket updated", "cluster", c.namespacedName(), "name", bucket.BucketName)
		c.raiseEvent(k8sutil.BucketEditEvent(bucket.BucketName, c.cluster))
	}
	for _, bucket := range remove {
		if err := c.client.DeleteBucket(c.readyMembers(), bucket); err != nil {
			return err
		}
		log.Info("Bucket deleted", "cluster", c.namespacedName(), "name", bucket.BucketName)
		c.raiseEvent(k8sutil.BucketDeleteEvent(bucket.BucketName, c.cluster))
	}

	c.cluster.Status.Buckets = requested
	return nil
}

// reconcile changes to selected pod labels for
// the nodePort service exposing admin console
func (c *Cluster) reconcileAdminService() error {
	status, err := k8sutil.UpdateAdminConsole(c.kubeClient, c.cluster, &c.cluster.Status)
	if err != nil {
		log.Error(err, "UI service update failed", "cluster", c.namespacedName())
		return err
	}

	serviceName := k8sutil.ConsoleServiceName(c.cluster.Name)
	switch status {
	case k8sutil.ReconcileStatusCreated:
		log.Info("UI service created", "cluster", c.namespacedName(), "name", serviceName)
		c.raiseEvent(k8sutil.AdminConsoleSvcCreateEvent(serviceName, c.cluster))
	case k8sutil.ReconcileStatusDeleted:
		log.Info("UI service deleted", "cluster", c.namespacedName(), "name", serviceName)
		c.raiseEvent(k8sutil.AdminConsoleSvcDeleteEvent(serviceName, c.cluster))
	case k8sutil.ReconcileStatusUpdated:
		log.Info("UI service updated", "cluster", c.namespacedName(), "name", serviceName)
	}

	return nil
}

// reconcileExposedFeatures looks at the requested exported feature set in the
// specification and add/removes services as requested, raising events as
// appropriate.
func (c *Cluster) reconcileExposedFeatures() error {
	status, err := k8sutil.UpdateExposedFeatures(c.kubeClient, c.members, c.cluster, &c.cluster.Status)
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

// initializes the first member in the cluster
func (c *Cluster) initMember(m *couchbaseutil.Member, serverSpec couchbasev2.ServerConfig) error {
	log.Info("Initial pod creating", "cluster", c.namespacedName())
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
func (c *Cluster) initMemberTLS(ctx context.Context, m *couchbaseutil.Member, cs couchbasev2.ClusterSpec) error {
	if cs.Networking.TLS != nil {
		// Static configuration:
		// * Upload the cluster CA certifcate
		// * Reload the server certifcate/key.  These were injected into
		//   the pod's file system from a secret during creation
		if cs.Networking.TLS.Static != nil {
			// Grab the operator secret
			secretName := cs.Networking.TLS.Static.OperatorSecret
			secret, err := k8sutil.GetSecret(c.kubeClient, secretName, c.cluster.Namespace, nil)
			if err != nil {
				return err
			}

			// Extract the CA's PEM data
			ca, ok := secret.Data[tlsOperatorSecretCACert]
			if !ok {
				return fmt.Errorf("unable to find %s in operator secret", tlsOperatorSecretCACert)
			}

			// Update Couchbase's TLS configuration
			if err := c.client.UploadClusterCACert(m, ca); err != nil {
				return err
			}
			if err := c.client.ReloadNodeCert(m); err != nil {
				return err
			}

			// Enable TLS verification now the certs are installed.
			m.SecureClient = true

			// Wait for the port to come backup with the correct certificate chain
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
	for _, serverClass := range c.cluster.Spec.Servers {
		for _, serverGroup := range serverClass.ServerGroups {
			serverGroups[serverGroup] = nil
		}
	}

	// Map into a list
	serverGroupList := []string{}
	for serverGroup := range serverGroups {
		serverGroupList = append(serverGroupList, serverGroup)
	}

	return serverGroupList
}

// createServerGroups creates any server groups defined in the specification
// whuch Couchbase doesn't know about
func (c *Cluster) createServerGroups(existingGroups *cbmgr.ServerGroups) (*cbmgr.ServerGroups, error) {
	serverGroups := c.getServerGroups()

	ctx, cancel := context.WithTimeout(c.ctx, couchbaseutil.ExtendedRetryPeriod)
	defer cancel()

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
		err := retryutil.Retry(ctx, 5*time.Second, func() (bool, error) {
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

	// Poll the server for existing information
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
			scheduledServerGroup, err := k8sutil.GetServerGroup(c.kubeClient, c.cluster.Namespace, podName)

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

	// Poll the server for existing information
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
			scheduledServerGroup, err := k8sutil.GetServerGroup(c.kubeClient, c.cluster.Namespace, podName)

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
	var hostname string
	if c.cluster.Spec.Networking.DNS != nil {
		// Use the user provided DNS name.
		hostname = k8sutil.GetDNSName(c.cluster, member.Name)
	} else {
		// Lookup the node IP the pod is running on.
		var err error
		hostname, err = k8sutil.GetHostIP(c.kubeClient, c.cluster.Namespace, member.Name)
		if err != nil {
			return nil, err
		}
	}

	ports, err := k8sutil.GetAlternateAddressExternalPorts(c.kubeClient, c.cluster.Namespace, member.Name)
	if err != nil {
		return nil, err
	}

	addresses := &cbmgr.AlternateAddressesExternal{
		Hostname: hostname,
		Ports:    ports,
	}

	return addresses, nil
}

// waitAlternateAddressReachable waits for advertised addresses to become reachable.
// This takes into account the time taken to create an external load balancer and
// DDNS updates.  Obviously this is a best effort as different DNS servers may behave
// differently, and what we see is not necessarily what the client sees.
func waitAlternateAddressReachable(addresses *cbmgr.AlternateAddressesExternal) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// All exposed features contain the admin port, only TLS enabled ports
	// are always guaranteed to exist.
	port := 18091
	if addresses.Ports != nil {
		port = int(addresses.Ports.AdminServicePortTLS)
	}

	return netutil.WaitForHostPort(ctx, fmt.Sprintf("%s:%d", addresses.Hostname, port))
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
			log.Info("External address collection failed", "cluster", c.namespacedName(), "name", member.Name)
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
			// Ignore this pod if the service hasn't been created yet.
			if errors.IsNotFound(err) {
				continue
			}
			return err
		}

		// Don't allow addresses to be advertised unless they can be used.
		if err := waitAlternateAddressReachable(addresses); err != nil {
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
			log.Info("External address collection failed", "cluster", c.namespacedName(), "name", member.Name)
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
			// Ignore this pod if the service hasn't been created yet.
			if errors.IsNotFound(err) {
				continue
			}
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
	if err := c.reconcileAutoCompactionSettings(); err != nil {
		return err
	}

	c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionManageConfig)
	return nil
}

// ensure autofailover timeout matches spec setting
func (c *Cluster) reconcileAutoFailoverSettings() error {

	// Get the existing settings
	failoverSettings, err := c.client.GetAutoFailoverSettings(c.readyMembers())
	if err != nil {
		log.Error(err, "Auto-failover settings collection failed", "cluster", c.namespacedName())
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
			log.Error(err, "Auto-failover settings update failed", "cluster", c.namespacedName())
			message := fmt.Sprintf("Failed to update autofailover settings: `%v`", err)
			c.cluster.Status.SetConfigRejectedCondition(message)
			return err
		}
		log.Info("Auto-failover settings updated", "cluster", c.namespacedName())
		c.raiseEvent(k8sutil.ClusterSettingsEditedEvent("autofailover", c.cluster))
	}

	return nil
}

// ensure memory quota's matche spec setting
func (c *Cluster) reconcileMemoryQuotaSettings() error {
	info, err := c.client.GetClusterInfo(c.readyMembers())
	if err != nil {
		log.Error(err, "Cluster settings collection failed", "cluster", c.namespacedName())
		return err
	}

	current := info.PoolsDefaults()

	name := c.cluster.Name
	if c.cluster.Spec.ClusterSettings.ClusterName != "" {
		name = c.cluster.Spec.ClusterSettings.ClusterName
	}

	config := c.cluster.Spec.ClusterSettings
	requested := &cbmgr.PoolsDefaults{
		ClusterName:          name,
		DataMemoryQuota:      config.DataServiceMemQuota,
		IndexMemoryQuota:     config.IndexServiceMemQuota,
		SearchMemoryQuota:    config.SearchServiceMemQuota,
		EventingMemoryQuota:  config.EventingServiceMemQuota,
		AnalyticsMemoryQuota: config.AnalyticsServiceMemQuota,
	}

	if !reflect.DeepEqual(current, requested) {
		if err := c.client.SetPoolsDefault(c.readyMembers(), requested); err != nil {
			log.Error(err, "Cluster settings update failed", "cluster", c.namespacedName())
			message := fmt.Sprintf("Unable update memory quota's [data:%d, index:%d, search:%d]: `%s`", config.DataServiceMemQuota, config.IndexServiceMemQuota, config.SearchServiceMemQuota, err.Error())
			c.cluster.Status.SetConfigRejectedCondition(message)
			return err
		}
		log.Info("Cluster settings updated", "cluster", c.namespacedName())
		c.raiseEvent(k8sutil.ClusterSettingsEditedEvent("memory quota", c.cluster))
	}

	return nil
}

// reconcileSoftwareUpdateNotificationSettings looks to see if the UI displays software
// update notifications, and updates if different from the cluster specification.
func (c *Cluster) reconcileSoftwareUpdateNotificationSettings() error {
	actual, err := c.client.GetUpdatesEnabled(c.readyMembers())
	if err != nil {
		log.Error(err, "Software notification settings collection failed", "cluster", c.namespacedName())
		return err
	}

	requested := c.cluster.Spec.SoftwareUpdateNotifications
	if actual != requested {
		if err := c.client.SetUpdatesEnabled(c.readyMembers(), requested); err != nil {
			log.Error(err, "Software notification settings update failed", "cluster", c.namespacedName())
			message := fmt.Sprintf("Unable update software notification settings: `%v`", err)
			c.cluster.Status.SetConfigRejectedCondition(message)
			return err
		}
		log.Info("Software notification settings updated", "cluster", c.namespacedName())
		c.raiseEvent(k8sutil.ClusterSettingsEditedEvent("update notifications", c.cluster))
	}

	return nil
}

// Compare cluster index settings with spec, reconcile if necessary
func (c *Cluster) reconcileIndexStorageSettings() error {
	settings, err := c.client.GetIndexSettings(c.readyMembers(), c.username, c.password)
	if err != nil {
		log.Error(err, "Index storage settings collection failed", "cluster", c.namespacedName())
		return err
	}

	specStorageMode := c.cluster.Spec.ClusterSettings.IndexStorageSetting
	if specStorageMode != string(settings.StorageMode) {
		if err := c.client.SetIndexSettings(c.readyMembers(), c.username, c.password, specStorageMode, settings); err != nil {
			log.Error(err, "Index storage settings update failed", "cluster", c.namespacedName())
			message := fmt.Sprintf("Unable set index storage mode to [%s]: %v", specStorageMode, err.Error())
			c.cluster.Status.SetConfigRejectedCondition(message)
			return err
		}
		log.Info("Index storage settings updated", "cluster", c.namespacedName())
		c.raiseEvent(k8sutil.ClusterSettingsEditedEvent("index service", c.cluster))
	}

	return nil
}

// reconcileAutoCompactionSettings sets up disk defragmentation
func (c *Cluster) reconcileAutoCompactionSettings() error {
	current, err := c.client.GetAutoCompactionSettings(c.readyMembers())
	if err != nil {
		return err
	}

	databaseFragmentationThresholdPercentage := 0
	if c.cluster.Spec.ClusterSettings.AutoCompaction.DatabaseFragmentationThreshold.Percent != nil {
		databaseFragmentationThresholdPercentage = *c.cluster.Spec.ClusterSettings.AutoCompaction.DatabaseFragmentationThreshold.Percent
	}
	databaseFragmentationThresholdSize, _ := c.cluster.Spec.ClusterSettings.AutoCompaction.DatabaseFragmentationThreshold.Size.AsInt64()

	viewFragmentationThresholdPercentage := 0
	if c.cluster.Spec.ClusterSettings.AutoCompaction.ViewFragmentationThreshold.Percent != nil {
		viewFragmentationThresholdPercentage = *c.cluster.Spec.ClusterSettings.AutoCompaction.ViewFragmentationThreshold.Percent
	}
	viewFragmentationThresholdSize, _ := c.cluster.Spec.ClusterSettings.AutoCompaction.ViewFragmentationThreshold.Size.AsInt64()

	fromHour := 0
	fromMinute := 0
	if c.cluster.Spec.ClusterSettings.AutoCompaction.TimeWindow.Start != "" {
		// validated by DAC
		parts := strings.Split(c.cluster.Spec.ClusterSettings.AutoCompaction.TimeWindow.Start, ":")
		fromHour, _ = strconv.Atoi(parts[0])
		fromMinute, _ = strconv.Atoi(parts[1])
	}

	toHour := 0
	toMinute := 0
	if c.cluster.Spec.ClusterSettings.AutoCompaction.TimeWindow.End != "" {
		// validated by DAC
		parts := strings.Split(c.cluster.Spec.ClusterSettings.AutoCompaction.TimeWindow.End, ":")
		toHour, _ = strconv.Atoi(parts[0])
		toMinute, _ = strconv.Atoi(parts[1])
	}

	requested := &cbmgr.AutoCompactionSettings{
		AutoCompactionSettings: cbmgr.AutoCompactionAutoCompactionSettings{
			DatabaseFragmentationThreshold: cbmgr.AutoCompactionDatabaseFragmentationThreshold{
				Percentage: databaseFragmentationThresholdPercentage,
				Size:       databaseFragmentationThresholdSize,
			},
			ViewFragmentationThreshold: cbmgr.AutoCompactionViewFragmentationThreshold{
				Percentage: viewFragmentationThresholdPercentage,
				Size:       viewFragmentationThresholdSize,
			},
			ParallelDBAndViewCompaction: c.cluster.Spec.ClusterSettings.AutoCompaction.ParallelCompaction,
			IndexCompactionMode:         "circular",
			IndexCircularCompaction: cbmgr.AutoCompactionIndexCircularCompaction{
				DaysOfWeek: "Sunday,Monday,Tuesday,Wednesday,Thursday,Friday,Saturday",
				Interval: cbmgr.AutoCompactionInterval{
					FromHour:     fromHour,
					FromMinute:   fromMinute,
					ToHour:       toHour,
					ToMinute:     toMinute,
					AbortOutside: c.cluster.Spec.ClusterSettings.AutoCompaction.TimeWindow.AbortCompactionOutsideWindow,
				},
			},
		},
		PurgeInterval: c.cluster.Spec.ClusterSettings.AutoCompaction.TombstonePurgeInterval.Hours() / 24.0,
	}

	if !reflect.DeepEqual(current, requested) {
		log.Info("Updating auto compaction settings", "cluster", c.namespacedName())
		if err := c.client.SetAutoCompactionSettings(c.readyMembers(), requested); err != nil {
			return err
		}
		c.raiseEvent(k8sutil.ClusterSettingsEditedEvent("auto compaction", c.cluster))
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

	err := k8sutil.IsPodRecoverable(c.kubeClient, *config, m.Name, c.cluster.Name, c.cluster.Namespace)
	if err != nil {
		if _, ok := err.(cberrors.ErrNoVolumeMounts); ok {
			// Pod is not configured for volumes
			return nil
		}
		return err
	}
	return nil
}

// Gets paths to use when initializing data, index, and analytics service.
// Default paths are used unless a claim is specified for the service in which
// case a custom mount path is used
func getServiceDataPaths(mounts *couchbasev2.VolumeMounts) (string, string, []string) {
	dataPath := constants.DefaultDataPath
	indexPath := constants.DefaultDataPath
	analyticsPaths := []string{}
	if mounts != nil && !mounts.LogsOnly() {
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
func (c *Cluster) needsUpgrade() (*couchbaseutil.Member, int, string, error) {
	var candidate *couchbaseutil.Member
	var targetConfiguration int
	var diff string

	// Names returns a sorted list for determinism.
	for _, name := range c.members.Names() {
		member := c.members[name]

		// Get what the member actualliy looks like.
		actual, err := c.kubeClient.CoreV1().Pods(c.cluster.Namespace).Get(name, metav1.GetOptions{})
		if err != nil {
			// Pod doesn't exist (deleted or evicted), ignore it
			if errors.IsNotFound(err) {
				continue
			}
			return nil, -1, "", err
		}

		// Get what the member should look like.
		serverClass := c.cluster.Spec.GetServerConfigByName(member.ServerConfig)
		if serverClass == nil {
			return nil, -1, "", err
		}
		requested, _, err := k8sutil.CreateCouchbasePodSpec(c.kubeClient, member, c.cluster, *serverClass)
		if err != nil {
			return nil, -1, "", err
		}

		// Check the specification at creation with the ones that are requested
		// currently.  If they differ then something has changed and we need to
		// "upgrade".  Otherwise accumulate the number of pods at the correct
		// target configuration.
		if actual.Annotations[constants.PodSpecAnnotation] == requested.Annotations[constants.PodSpecAnnotation] {
			targetConfiguration++
			continue
		}

		if candidate == nil {
			diff = cmp.Diff(actual.Annotations[constants.PodSpecAnnotation], requested.Annotations[constants.PodSpecAnnotation])
			candidate = member
		}
	}
	return candidate, targetConfiguration, diff, nil
}

// reportUpgrade looks at the current state and any existing upgrade status
// condition, makes condition updates and raises events.
func (c *Cluster) reportUpgrade(status *couchbasev2.UpgradeStatus) error {
	// Look for an existing condition
	condition := c.cluster.Status.GetCondition(couchbasev2.ClusterConditionUpgrading)

	if condition == nil {
		// No existing condition, we are guaranteed to be upgrading.
		status.State = couchbasev2.UpgradingMessageStateUpgrading
		c.raiseEvent(k8sutil.UpgradeStartedEvent(status.Source, status.Target, c.cluster))
	} else {
		// There is an existing condition, check to see which way we are going.
		// If we have switched directions we will need a new event.
		oldStatus := couchbasev2.NewUpgradeStatus(condition.Message)
		if status.Target != oldStatus.Target {
			version, _ := couchbaseutil.NewVersion(status.Target)
			oldVersion, _ := couchbaseutil.NewVersion(oldStatus.Target)
			switch version.Compare(oldVersion) {
			case 1:
				// Upgrading
				status.State = couchbasev2.UpgradingMessageStateUpgrading
				c.raiseEvent(k8sutil.UpgradeStartedEvent(status.Source, status.Target, c.cluster))
			case -1:
				// Rolling back
				status.State = couchbasev2.UpgradingMessageStateRollback
				c.raiseEvent(k8sutil.RollbackStartedEvent(status.Source, status.Target, c.cluster))
			}
		} else {
			status.State = oldStatus.State
		}
	}

	c.cluster.Status.SetUpgradingCondition(status)

	if err := c.updateCRStatus(); err != nil {
		return err
	}

	return nil
}

// reportUpgradeComplete is called unconditionally when the reconcile is complete.
// If there was an unpgrade condition and the cluster no longer needs an upgrade clear
// the condition and raise any necessary events.
func (c *Cluster) reportUpgradeComplete() error {
	// Still upgrading do nothing
	if candidate, _, _, err := c.needsUpgrade(); err != nil {
		return err
	} else if candidate != nil {
		return nil
	}

	// There is no condition, we weren't upgrading, do nothing
	condition := c.cluster.Status.GetCondition(couchbasev2.ClusterConditionUpgrading)
	if condition == nil {
		return nil
	}

	status := couchbasev2.NewUpgradeStatus(condition.Message)
	switch status.State {
	case couchbasev2.UpgradingMessageStateUpgrading:
		c.raiseEvent(k8sutil.UpgradeFinishedEvent(status.Source, status.Target, c.cluster))
	case couchbasev2.UpgradingMessageStateRollback:
		c.raiseEvent(k8sutil.RollbackFinishedEvent(status.Source, status.Target, c.cluster))
	}

	c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionUpgrading)

	version, err := k8sutil.CouchbaseVersion(c.cluster.Spec.Image)
	if err != nil {
		return err
	}
	c.cluster.Status.CurrentVersion = version

	if err := c.updateCRStatus(); err != nil {
		return err
	}

	return nil
}

// reconcileReadiness marks the pods as "ready" once everything is repaired, scaled and
// balanced.  It also reconciles a pod disruption budget so we only tolerate a certain
// number of evictions during a drain i.e. k8s upgrade.
func (c *Cluster) reconcileReadiness() error {
	for name := range c.members {
		if err := k8sutil.FlagPodReady(c.kubeClient, c.cluster.Namespace, name); err != nil {
			return err
		}
	}
	return k8sutil.ReconcilePDB(c.kubeClient, c.cluster)
}

// replicationKey returns a unique identifier per replication.
func replicationKey(r cbmgr.Replication) string {
	return fmt.Sprintf("%s/%s/%s", r.ToCluster, r.FromBucket, r.ToBucket)
}

// reconcileXDCR creates and deletes XDCR connections dynamically.
func (c *Cluster) reconcileXDCR() error {
	if !c.cluster.Spec.XDCR.Managed {
		return nil
	}

	requestedClusters := []cbmgr.RemoteCluster{}
	requestedReplications := []cbmgr.Replication{}

	for _, cluster := range c.cluster.Spec.XDCR.RemoteClusters {
		requested := cbmgr.RemoteCluster{
			Name:     cluster.Name,
			UUID:     cluster.UUID,
			Hostname: cluster.Hostname,
		}

		if cluster.AuthenticationSecret != nil {
			secret, err := k8sutil.GetSecret(c.kubeClient, *cluster.AuthenticationSecret, c.cluster.Namespace, nil)
			if err != nil {
				return err
			}
			requested.Username = string(secret.Data["username"])
			requested.Password = string(secret.Data["password"])
		}

		if cluster.TLS != nil {
			if cluster.TLS.Secret != nil {
				secret, err := k8sutil.GetSecret(c.kubeClient, *cluster.TLS.Secret, c.cluster.Namespace, nil)
				if err != nil {
					return err
				}
				if _, ok := secret.Data[couchbasev2.RemoteClusterTLSCA]; !ok {
					return fmt.Errorf("CA certificate is required for TLS encryption")
				}
				// No, we will never support any other type!
				requested.SecureType = "full"

				// While we should pass through the raw []byte, it makes life simpler for the client
				// library if we pass it as a string.
				requested.CA = string(secret.Data[couchbasev2.RemoteClusterTLSCA])

				// Add in client certificates if requested.
				if cert, ok := secret.Data[couchbasev2.RemoteClusterTLSCertificate]; ok {
					requested.Certificate = string(cert)
				}
				if key, ok := secret.Data[couchbasev2.RemoteClusterTLSKey]; ok {
					requested.Key = string(key)
				}
			}
		}

		requestedClusters = append(requestedClusters, requested)

		listOpts := metav1.ListOptions{}
		if cluster.Replications.Selector != nil {
			listOpts.LabelSelector = metav1.FormatLabelSelector(c.cluster.Spec.Buckets.Selector)
		}
		replications, err := c.couchbaseKubeClient.CouchbaseV2().CouchbaseReplications(c.cluster.Namespace).List(listOpts)
		if err != nil {
			return err
		}

		for _, replication := range replications.Items {
			requestedReplications = append(requestedReplications, cbmgr.Replication{
				FromBucket:       replication.Spec.Bucket,
				ToCluster:        cluster.Name,
				ToBucket:         replication.Spec.RemoteBucket,
				Type:             "xmem",
				ReplicationType:  "continuous",
				CompressionType:  string(replication.Spec.CompressionType),
				FilterExpression: replication.Spec.FilterExpression,
			})
		}
	}

	currentClusters, err := c.client.ListRemoteClusters(c.readyMembers())
	if err != nil {
		return err
	}

	currentReplications, err := c.client.ListReplications(c.readyMembers())
	if err != nil {
		return err
	}

	// Create any new clusters...
CreateNextCluster:
	for _, requested := range requestedClusters {
		for _, current := range currentClusters {
			if current.Name == requested.Name {
				continue CreateNextCluster
			}
		}
		log.Info("Creating XDCR remote cluster", "cluster", c.namespacedName(), "remote", requested.Name)
		if err := c.client.CreateRemoteCluster(c.readyMembers(), &requested); err != nil {
			return err
		}
		c.raiseEvent(k8sutil.RemoteClusterAddedEvent(c.cluster, requested.Name))
	}

	// Create any new replications...
CreateNextReplication:
	for _, requested := range requestedReplications {
		for _, current := range currentReplications {
			if replicationKey(current) == replicationKey(requested) {
				continue CreateNextReplication
			}
		}
		log.Info("Creating XDCR replication", "cluster", c.namespacedName(), "replication", replicationKey(requested))
		if err := c.client.CreateReplication(c.readyMembers(), &requested); err != nil {
			return err
		}
		c.raiseEvent(k8sutil.ReplicationAddedEvent(c.cluster, replicationKey(requested)))
	}

	// Delete any orphaned replications...
DeleteNextReplication:
	for _, current := range currentReplications {
		for _, requested := range requestedReplications {
			if replicationKey(current) == replicationKey(requested) {
				continue DeleteNextReplication
			}
		}
		log.Info("Deleting XDCR replication", "cluster", c.namespacedName(), "replication", replicationKey(current))
		if err := c.client.DeleteReplication(c.readyMembers(), &current); err != nil {
			return err
		}
		c.raiseEvent(k8sutil.ReplicationRemovedEvent(c.cluster, replicationKey(current)))
	}

	// Delete any orphaned clusters...
DeleteNextCluster:
	for _, current := range currentClusters {
		for _, requested := range requestedClusters {
			if current.Name == requested.Name {
				continue DeleteNextCluster
			}
		}
		log.Info("Deleting XDCR remote cluster", "cluster", c.namespacedName(), "remote", current.Name)
		if err := c.client.DeleteRemoteCluster(c.readyMembers(), &current); err != nil {
			return err
		}
		c.raiseEvent(k8sutil.RemoteClusterRemovedEvent(c.cluster, current.Name))
	}

	return nil
}

// reconcilePersistentStatus examines persistent state and synchronizes and
// dependant cluster status, in case someone or something thought it wise to
// delete it!
func (c *Cluster) reconcilePersistentStatus() error {
	phase, err := c.state.Get(persistence.Phase)
	if err != nil {
		return err
	}
	uuid, err := c.state.Get(persistence.UUID)
	if err != nil {
		return err
	}
	version, err := c.state.Get(persistence.Version)
	if err != nil {
		return err
	}

	c.cluster.Status.Phase = couchbasev2.ClusterPhase(phase)
	c.cluster.Status.ClusterID = uuid
	c.cluster.Status.CurrentVersion = version
	if err := c.updateCRStatus(); err != nil {
		log.Info("failed to update cluster status", "cluster", c.namespacedName())
	}
	return nil
}

// gatherUsers loads up user configurations from Kubernetes and marshalls them into canonical form.
func (c *Cluster) gatherUsers() ([]cbmgr.User, error) {
	listOpts := metav1.ListOptions{}

	// only users with role bindings can be created
	couchbaseRoleBindings, err := c.couchbaseKubeClient.CouchbaseV2().CouchbaseRoleBindings(c.cluster.Namespace).List(listOpts)
	if err != nil {
		return nil, err
	}

	// requested users
	users := make(map[string]*cbmgr.User)

	// map of uid+roleref combinations
	boundUserRoles := make(map[string]bool)

	// step through role binding gathering requested users
	for _, roleBinding := range couchbaseRoleBindings.Items {

		// gather roles
		roles := []cbmgr.UserRole{}
		roleRef := roleBinding.Spec.RoleRef
		roleName := roleRef.Name
		couchbaseRole, err := c.couchbaseKubeClient.CouchbaseV2().CouchbaseRoles(c.cluster.Namespace).Get(roleName, metav1.GetOptions{})

		// attempt to get referenced role, and warn if missing because
		// users may be deleted if there are no roles to bind
		if k8sutil.IsKubernetesResourceNotFoundError(err) {
			err = fmt.Errorf("rolebinding `%s` refers to a missing role resource `%s`", roleBinding.Name, roleName)
			log.Error(err, "User may be deleted if no longer bound to roles", "cluster", c.namespacedName())
			continue
		} else if err != nil {
			return nil, err
		}

		for _, role := range couchbaseRole.Spec.Roles {
			roles = append(roles, cbmgr.UserRole{
				Role:       role.Name,
				BucketName: role.Bucket,
			})
		}

		// when all roles are missing, then the users bound to the role
		// will also be deleted unless found in a different rolebinding
		if len(roles) == 0 {
			continue
		}

		// gather users bound to roles
		for _, subject := range roleBinding.Spec.Subjects {
			subjectName := subject.Name
			couchbaseUser, err := c.couchbaseKubeClient.CouchbaseV2().CouchbaseUsers(c.cluster.Namespace).Get(subjectName, metav1.GetOptions{})

			// missing users will be deleted
			if k8sutil.IsKubernetesResourceNotFoundError(err) {
				err = fmt.Errorf("rolebinding `%s` refers to a missing user resource `%s`", roleBinding.Name, subjectName)
				log.Error(err, "User may be deleted if no referenced by a role binding", "cluster", c.namespacedName())
				continue
			} else if err != nil {
				return nil, err
			}

			// get secret info when using local auth
			user := &cbmgr.User{
				Name:  couchbaseUser.Spec.FullName,
				ID:    couchbaseUser.Name,
				Roles: roles,
			}

			// extend current roles if user is previously bound
			if boundUser, ok := users[user.ID]; ok {
				// ...ignoring duplicates
				for _, r := range roles {
					userRoleID := user.ID + "-" + r.Role
					if _, ok := boundUserRoles[userRoleID]; !ok {
						// associate additional roles from ref with user
						boundUser.Roles = append(boundUser.Roles, r)
					}
				}
			} else {
				// New user to set
				if couchbaseUser.Spec.AuthDomain == string(cbmgr.InternalAuthDomain) {
					user.Domain = cbmgr.InternalAuthDomain
					password, err := c.getRBACAuthPassword(couchbaseUser.Spec.AuthSecret)
					if err != nil {
						return nil, err
					}
					user.Password = password
				}
				users[user.ID] = user

				// keep track of specific roles so we can
				// detect if a role has already been added
				for _, r := range roles {
					userRoleID := user.ID + "-" + r.Role
					boundUserRoles[userRoleID] = true
				}
			}
		}
	}

	// represent as slice
	requestedUsers := []cbmgr.User{}
	for _, user := range users {
		requestedUsers = append(requestedUsers, *user)
	}
	return requestedUsers, nil
}

// Get auth password to be set for user
func (c *Cluster) getRBACAuthPassword(authSecret string) (string, error) {
	var password string
	opts := metav1.GetOptions{}
	secret, err := c.kubeClient.CoreV1().Secrets(c.cluster.Namespace).Get(authSecret, opts)
	if err != nil {
		return password, err
	}

	data := secret.Data
	if dataPassword, ok := data[constants.AuthSecretPasswordKey]; ok {
		password = string(dataPassword)
	} else {
		return password, cberrors.ErrSecretMissingPassword{Reason: authSecret}
	}

	return password, nil
}

// inspectUsers looks up users to create and roles to apply or update.
// Users in the cluster without rolebinding are deleted.
func (c *Cluster) inspectUsers() ([]cbmgr.User, []cbmgr.User, []cbmgr.User, error) {

	requested, err := c.gatherUsers()
	if err != nil {
		return nil, nil, nil, err
	}

	actual, err := c.client.ListUsers(c.readyMembers())
	if err != nil {
		return nil, nil, nil, err
	}

	create := []cbmgr.User{}
	update := []cbmgr.User{}
	remove := []cbmgr.User{}

	// Detect which users do not exist and need to be created
	// or which exist and need to be updated
	for _, r := range requested {
		found := false
		for _, a := range actual {
			if r.ID == a.ID {
				// compare roles which are the only mutable user attributes
				if !reflect.DeepEqual(cbmgr.RolesToStr(r.Roles), cbmgr.RolesToStr(a.Roles)) {
					update = append(update, r)
				}
				found = true
				break
			}
		}
		if !found {
			create = append(create, r)
		}
	}

	// Do an exhaustive search of actual users in the requested list, deleting
	// as necessary.
	for _, a := range actual {
		found := false
		for _, r := range requested {
			if a.ID == r.ID {
				found = true
				break
			}
		}
		if !found {
			remove = append(remove, a)
		}
	}
	return create, update, remove, nil
}

// reconcileUsers synchronizes couchbase users with requested users
// and raising events as appropriate
func (c *Cluster) reconcileUsers() error {

	if !c.cluster.Spec.Security.RBAC.Managed {
		return nil
	}

	create, update, remove, err := c.inspectUsers()
	if err != nil {
		return err
	}

	if len(create) == 0 && len(update) == 0 && len(remove) == 0 {
		// nothing to reconcile
		return nil
	}

	existingUsers := []string{}
	for _, user := range create {
		if err := c.client.CreateUser(c.readyMembers(), user); err != nil {
			return err
		}
		log.Info("User created", "cluster", c.namespacedName(), "name", user.ID)
		c.raiseEvent(k8sutil.UserCreateEvent(user.ID, c.cluster))
		existingUsers = append(existingUsers, user.ID)
	}
	for _, user := range update {
		// User update is treated the same as create
		if err := c.client.CreateUser(c.readyMembers(), user); err != nil {
			return err
		}
		log.Info("User updated", "cluster", c.namespacedName(), "name", user.ID)
		c.raiseEvent(k8sutil.UserEditEvent(user.ID, c.cluster))
		existingUsers = append(existingUsers, user.ID)
	}
	for _, user := range remove {
		if err := c.client.DeleteUser(c.readyMembers(), user); err != nil {
			return err
		}
		log.Info("User deleted", "cluster", c.namespacedName(), "name", user.ID)
		c.raiseEvent(k8sutil.UserDeleteEvent(user.ID, c.cluster))
	}

	c.cluster.Status.Users = existingUsers
	return nil
}
