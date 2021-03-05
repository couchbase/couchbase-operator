package cluster

import (
	"context"
	"encoding/json"
	goerrors "errors"
	"fmt"
	"net"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/cluster/persistence"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/diff"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/netutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/pkg/util/scheduler"

	"k8s.io/api/batch/v1beta1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const (
	// extendedRetryPeriod is an extended amount of time to wait for slow
	// API operations to report success.
	extendedRetryPeriod = 3 * time.Minute
)

// reconcile is the main function for the whole cluster lifecycle.  It works like this:
//
// * Deletes any completed pods, these are not considered as members, but do pollute the
//   namespace and will prevent clusters from being resurrected.  Pods typically enter this
//   state when Kubernetes comes back from the dead.
// * Setup any networking, this is essential for the Operator to be able to contact any
//   pods to perform initialization or reconciliation actions.
// * Create an initial member if there are no members, this handles creation and recovery
//   if an in-memory cluster has vanished.
// * Fix up TLS, we will be completely unable to communicate with the cluster if this is
//   not performed first.
// * Topology and feature updates.
func (c *Cluster) reconcile() error {
	log.V(1).Info("Reconciliation starting", "cluster", c.namespacedName())
	defer log.V(1).Info("Reconciliation completed", "cluster", c.namespacedName())

	pods := c.getClusterPods()

	// Initialize the scheduler each time around, this saves us having to update
	// internal state in all the cases when a pod fails to be created, deleted,
	// or disappears.
	scheduler, err := scheduler.New(pods, c.cluster)
	if err != nil {
		return err
	}

	c.scheduler = scheduler

	// Hibernation trumps all.
	if c.cluster.Spec.Hibernate {
		if err := c.hibernate(); err != nil {
			return err
		}

		return nil
	}

	c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionHibernating)

	if err := c.reconcileStatus(); err != nil {
		return err
	}

	if err := c.reconcileCompletedPods(); err != nil {
		return err
	}

	// Establish DNS for cluster communication.
	if err := k8sutil.ReconcilePeerServices(c.k8s, c.cluster); err != nil {
		return err
	}

	// Setup the UI so we can monitor cluster creation.  This is legacy behaviour, but
	// makes for a good demo...
	if err := c.reconcileAdminService(); err != nil {
		return err
	}

	// Ensure any resources required by pods are in place.
	if err := c.refreshTLSShadowSecret(); err != nil {
		return err
	}

	// Pod recovery has precedence over cluster creation.  If cluster creation happened
	// first, there would actually be a pod, but the initial condition would be none
	// and weird could happen.
	if len(pods) == 0 {
		// Change to an unhealthy condition as soon as we detect it, otherwise
		// to the ourside world, we will look available and balanced until we
		// hit the node topology code.
		c.cluster.Status.SetCreatingCondition()

		if err := c.updateCRStatus(); err != nil {
			return err
		}

		// Always return, this allows the member and callable member set to be
		// correctly populated, whereas if we continued here, then the member
		// set wouldn't know about any ephemeral pods in the cluster, that
		// information comes from Couchbase Server.
		recovered, err := c.recoverClusterDown()
		if err != nil {
			return err
		}

		if recovered {
			return nil
		}
	}

	// Create/recreate the cluster.
	if c.members.Empty() {
		if err := c.create(); err != nil {
			return err
		}
	}

	// Persistent data may need to be reflected in the status.
	if err := c.reconcilePersistentStatus(); err != nil {
		return err
	}

	// Update the password after we have done TLS to ensure we can actually talk to
	// the API.
	if err := c.reconcileAdminPassword(); err != nil {
		return err
	}

	fsm, err := c.newReconcileMachine()
	if err != nil {
		return err
	}

	// Some features need to treat topology changes as atomic, the key one
	// being TLS.
	if err := c.reconcileTLSPreTopologyChange(); err != nil {
		return err
	}

	if err := c.reconcileMembers(fsm); err != nil {
		return err
	}

	// Update the status and ready list to reflect any new members added.
	if err := c.updateMembers(); err != nil {
		return err
	}

	if err := c.reportUpgradeComplete(); err != nil {
		return err
	}

	// If the cluster is upgrading, then don't interfere with anything else
	// as the data returned from Couchbase will vary depending on the version
	// leading to some very strange and possibly dangerous behaviour.
	if _, err := c.state.Get(persistence.Upgrading); err == nil {
		return nil
	}

	// Update the status in case an upgrade has bumped the version and allowed
	// new features.  Now we gate feature updated on upgrade completion we can
	// remove the use of Status.CurrentVersion and just use Spec.Image.
	if err := c.reconcilePersistentStatus(); err != nil {
		return err
	}

	if err := c.reconcileTLSPostTopologyChange(); err != nil {
		return err
	}

	if err := c.reconcilePods(); err != nil {
		return err
	}

	if err := c.reconcileClusterSettings(); err != nil {
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

	if err := c.reconcilePodServices(); err != nil {
		return err
	}

	if err := c.reconcileMemberAlternateAddresses(); err != nil {
		return err
	}

	if err := c.reconcileRBAC(); err != nil {
		return err
	}

	if err := c.reconcileBackup(); err != nil {
		return err
	}

	if err := c.reconcileBackupRestore(); err != nil {
		return err
	}

	if err := c.reconcileAutoscalers(); err != nil {
		return err
	}

	c.cluster.Status.Size = c.members.Size()
	c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionScaling)
	c.cluster.Status.SetBalancedCondition()
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
// Returns true if reconciliation completed properly.
func (c *Cluster) reconcileMembers(rm *ReconcileMachine) error {
	return rm.exec(c)
}

// logFailedMember outputs any debug information we can about a failed member creation.
func (c *Cluster) logFailedMember(message, name string) {
	log.Info(message, "cluster", c.namespacedName(), "name", name, "resource", k8sutil.LogPod(c.k8s, c.cluster.Namespace, name))
}

// Create a new Couchbase cluster member.
func (c *Cluster) createMember(serverSpec couchbasev2.ServerConfig) (m couchbaseutil.Member, err error) {
	// The pod creation timeout is global across this operation e.g. PVCs, pods, the lot.
	podCreateTimeout, err := time.ParseDuration(c.config.PodCreateTimeout)
	if err != nil {
		return nil, fmt.Errorf("PodCreateTimeout improperly formatted: %w", errors.NewStackTracedError(err))
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
	newMember, err := c.newMember(index, serverSpec.Name, c.cluster.Spec.CouchbaseImage())
	if err != nil {
		return nil, err
	}

	// Decrement the member number on error (probably don't need to actually do this
	// any more, or even generate names for that matter, let Kubernetes do it, that said
	// it makes event coalesing actually possible, perhaps we could generate a name from
	// a member hash or something?)
	defer func() {
		if err != nil {
			_ = c.decPodIndex()
		}
	}()

	// Delete volumes on error here, they contain no data, and restarting from scratch
	// *may* lead to success.
	if err := c.createPod(ctx, newMember, serverSpec, true); err != nil {
		return nil, fmt.Errorf("fail to create member's pod (%s): %w", newMember.Name(), err)
	}

	// From this point on, if something goes wrong, we blow the pod (and any volumes)
	// away, as they are uninitialized and not clustered, hoping it will fix itself
	// next time around.
	defer func() {
		if err == nil {
			return
		}

		c.raiseEventCached(k8sutil.MemberCreationFailedEvent(newMember.Name(), c.cluster))

		if rerr := c.removePod(newMember.Name(), true); rerr != nil {
			log.Info("Unable to remove failed member", "cluster", c.namespacedName(), "error", rerr)
		}
	}()

	// The new node will not be part of the cluster yet  so the API calls will fail
	// when checking the UUID, temporarily disable these checks while installing
	// TLS configuration
	// c.api.SetUUID("")
	// defer c.api.SetUUID(c.cluster.Status.ClusterID)

	// Explicitly change the network mode before doing anything else.
	if c.cluster.Spec.Networking.AddressFamily != nil {
		var family couchbaseutil.AddressFamily

		switch *c.cluster.Spec.Networking.AddressFamily {
		case couchbasev2.AFInet:
			family = couchbaseutil.AddressFamilyIPV4
		case couchbasev2.AFInet6:
			family = couchbaseutil.AddressFamilyIPV6
		default:
			return nil, fmt.Errorf("%w: unexpected address family %v", errors.NewStackTracedError(errors.ErrInternalError), family)
		}

		config := &couchbaseutil.NodeNetworkConfiguration{
			AddressFamily: family,
		}

		if err := couchbaseutil.SetNodeNetworkConfiguration(config).InPlaintext().RetryFor(time.Minute).On(c.api, newMember); err != nil {
			return nil, err
		}
	}

	// Check the pod is EE.  DNS should be working (as checked by the wait above), but
	// it has been observed that this may still need a retry.
	info := &couchbaseutil.PoolsInfo{}
	if err := couchbaseutil.GetPools(info).InPlaintext().RetryFor(time.Minute).On(c.api, newMember); err != nil {
		return nil, err
	}

	if !info.Enterprise {
		return nil, fmt.Errorf("%w: couchbase server reports community edition", errors.NewStackTracedError(errors.ErrConfigurationInvalid))
	}

	// Enable TLS if requested
	if err := c.initMemberTLS(ctx, newMember); err != nil {
		return nil, err
	}

	// Set the hostname.  Sometimes this returns a 500 so needs a retry, I'm guessing
	// although the server is running and returns cluster information it's not configured
	// enough to allow host name changes.
	if err := couchbaseutil.SetHostname(newMember.GetDNSName()).RetryFor(time.Minute).On(c.api, newMember); err != nil {
		return nil, err
	}

	// Initialize storage paths.
	dataPath, indexPath, analyticsPaths := getServiceDataPaths(serverSpec.GetVolumeMounts())
	if err := couchbaseutil.SetStoragePaths(dataPath, indexPath, analyticsPaths).On(c.api, newMember); err != nil {
		return nil, err
	}

	// Notify that we have created a new member
	if err := c.clusterCreateMember(newMember); err != nil {
		return nil, err
	}

	if err := c.updateCRStatus(); err != nil {
		return nil, err
	}

	return newMember, nil
}

// Creates and adds a new Couchbase cluster member.
func (c *Cluster) addMember(serverSpec couchbasev2.ServerConfig) (couchbaseutil.Member, error) {
	// Create the new member
	newMember, err := c.createMember(serverSpec)
	if err != nil {
		return nil, err
	}

	// TODO: 6.5.0+ probably allows HTTPS bootstrap all the time, not just
	// when N2N is enabled...
	url := newMember.GetHostURLPlaintext()

	if c.supportsNodeToNode() && c.nodeToNodeEnabled() {
		url = newMember.GetHostURL()
	}

	// Add to the cluster. Note we have to use the plain text url as
	// /controller/addNode will not work with a https reference
	services, err := couchbaseutil.ServiceListFromStringArray(couchbasev2.ServiceList(serverSpec.Services).StringSlice())
	if err != nil {
		return newMember, err
	}

	if err := couchbaseutil.AddNode(url, c.username, c.password, services).RetryFor(extendedRetryPeriod).On(c.api, c.readyMembers()); err != nil {
		return newMember, err
	}

	// Notify that we have added a new member, this makes it callable.
	c.clusterAddMember(newMember)

	log.Info("Pod added to cluster", "cluster", c.namespacedName(), "name", newMember.Name())
	c.raiseEvent(k8sutil.MemberAddEvent(newMember.Name(), c.cluster))

	return newMember, nil
}

// Destroys a Couchbase cluster member.
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

// Cancel a node addition.
func (c *Cluster) cancelAddMember(member couchbaseutil.Member) error {
	if err := couchbaseutil.CancelAddNode(member.GetOTPNode()).RetryFor(extendedRetryPeriod).On(c.api, c.readyMembers()); err != nil {
		return err
	}

	c.raiseEvent(k8sutil.FailedAddNodeEvent(member.Name(), c.cluster))

	return nil
}

// gatherBuckets loads up bucket configurations from Kubernetes and marshalls them into canonical form.
func (c *Cluster) gatherBuckets() ([]couchbaseutil.Bucket, error) {
	selector := labels.Everything()

	if c.cluster.Spec.Buckets.Selector != nil {
		var err error
		if selector, err = metav1.LabelSelectorAsSelector(c.cluster.Spec.Buckets.Selector); err != nil {
			return nil, err
		}
	}

	durable, err := couchbaseutil.VersionAfter(c.cluster.Status.CurrentVersion, "6.6.0")
	if err != nil {
		return nil, err
	}

	couchbaseBuckets := c.k8s.CouchbaseBuckets.List()
	couchbaseEphemeralBuckets := c.k8s.CouchbaseEphemeralBuckets.List()
	couchbaseMemcachedBuckets := c.k8s.CouchbaseMemcachedBuckets.List()

	buckets := []couchbaseutil.Bucket{}

	for _, bucket := range couchbaseBuckets {
		if !selector.Matches(labels.Set(bucket.Labels)) {
			continue
		}

		name := bucket.Name
		if bucket.Spec.Name != "" {
			name = bucket.Spec.Name
		}

		b := couchbaseutil.Bucket{
			BucketName:         name,
			BucketType:         constants.BucketTypeCouchbase,
			BucketMemoryQuota:  k8sutil.Megabytes(bucket.Spec.MemoryQuota),
			BucketReplicas:     bucket.Spec.Replicas,
			IoPriority:         couchbaseutil.IoPriorityType(bucket.Spec.IoPriority),
			EvictionPolicy:     string(bucket.Spec.EvictionPolicy),
			ConflictResolution: string(bucket.Spec.ConflictResolution),
			EnableFlush:        bucket.Spec.EnableFlush,
			EnableIndexReplica: bucket.Spec.EnableIndexReplica,
			CompressionMode:    couchbaseutil.CompressionMode(bucket.Spec.CompressionMode),
		}

		if durable {
			b.DurabilityMinLevel = couchbaseutil.Durability(bucket.GetMinimumDurability())
		}

		if bucket.Spec.MaxTTL != nil {
			b.MaxTTL = int(bucket.Spec.MaxTTL.Duration.Seconds())
		}

		buckets = append(buckets, b)
	}

	for _, bucket := range couchbaseEphemeralBuckets {
		if !selector.Matches(labels.Set(bucket.Labels)) {
			continue
		}

		name := bucket.Name

		if bucket.Spec.Name != "" {
			name = bucket.Spec.Name
		}

		b := couchbaseutil.Bucket{
			BucketName:         name,
			BucketType:         constants.BucketTypeEphemeral,
			BucketMemoryQuota:  k8sutil.Megabytes(bucket.Spec.MemoryQuota),
			BucketReplicas:     bucket.Spec.Replicas,
			IoPriority:         couchbaseutil.IoPriorityType(bucket.Spec.IoPriority),
			EvictionPolicy:     string(bucket.Spec.EvictionPolicy),
			ConflictResolution: string(bucket.Spec.ConflictResolution),
			EnableFlush:        bucket.Spec.EnableFlush,
			CompressionMode:    couchbaseutil.CompressionMode(bucket.Spec.CompressionMode),
		}

		if durable {
			b.DurabilityMinLevel = couchbaseutil.Durability(bucket.GetMinimumDurability())
		}

		if bucket.Spec.MaxTTL != nil {
			b.MaxTTL = int(bucket.Spec.MaxTTL.Duration.Seconds())
		}

		buckets = append(buckets, b)
	}

	for _, bucket := range couchbaseMemcachedBuckets {
		if !selector.Matches(labels.Set(bucket.Labels)) {
			continue
		}

		name := bucket.Name

		if bucket.Spec.Name != "" {
			name = bucket.Spec.Name
		}

		buckets = append(buckets, couchbaseutil.Bucket{
			BucketName:        name,
			BucketType:        constants.BucketTypeMemcached,
			BucketMemoryQuota: k8sutil.Megabytes(bucket.Spec.MemoryQuota),
			EnableFlush:       bucket.Spec.EnableFlush,
		})
	}

	return buckets, nil
}

// inspectBuckets compares Kubernetes buckets with Couchbase buckets and returns lists
// of buckets to create, update or remove and the requested set for status updates.
func (c *Cluster) inspectBuckets() ([]couchbaseutil.Bucket, []couchbaseutil.Bucket, []couchbaseutil.Bucket, []couchbaseutil.Bucket, error) {
	requested, err := c.gatherBuckets()
	if err != nil {
		return nil, nil, nil, nil, err
	}

	actual := couchbaseutil.BucketList{}
	if err := couchbaseutil.ListBuckets(&actual).On(c.api, c.readyMembers()); err != nil {
		return nil, nil, nil, nil, err
	}

	create := []couchbaseutil.Bucket{}
	update := []couchbaseutil.Bucket{}
	remove := []couchbaseutil.Bucket{}

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
// of existing buckets to cluster spec.
func (c *Cluster) reconcileBuckets() error {
	if !c.cluster.Spec.Buckets.Managed {
		return nil
	}

	create, update, remove, requested, err := c.inspectBuckets()
	if err != nil {
		return err
	}

	for i := range create {
		bucket := &create[i]

		if err := couchbaseutil.CreateBucket(bucket).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		log.Info("Bucket created", "cluster", c.namespacedName(), "name", bucket.BucketName)
		c.raiseEvent(k8sutil.BucketCreateEvent(bucket.BucketName, c.cluster))
	}

	for i := range update {
		bucket := &update[i]

		if err := couchbaseutil.UpdateBucket(bucket).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		log.Info("Bucket updated", "cluster", c.namespacedName(), "name", bucket.BucketName)
		c.raiseEvent(k8sutil.BucketEditEvent(bucket.BucketName, c.cluster))
	}

	for _, bucket := range remove {
		if err := couchbaseutil.DeleteBucket(bucket.BucketName).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		log.Info("Bucket deleted", "cluster", c.namespacedName(), "name", bucket.BucketName)
		c.raiseEvent(k8sutil.BucketDeleteEvent(bucket.BucketName, c.cluster))
	}

	// To avoid API updates, we record the name of each bucket on the system (this will
	// be lexically sorted), and we add buckets to the status in a deterministic order.
	names := make([]string, len(requested))
	statuses := map[string]couchbasev2.BucketStatus{}

	for i, bucket := range requested {
		names[i] = bucket.BucketName

		statuses[bucket.BucketName] = couchbasev2.BucketStatus{
			BucketName:         bucket.BucketName,
			BucketType:         bucket.BucketType,
			BucketMemoryQuota:  bucket.BucketMemoryQuota,
			BucketReplicas:     bucket.BucketReplicas,
			IoPriority:         string(bucket.IoPriority),
			EvictionPolicy:     bucket.EvictionPolicy,
			ConflictResolution: bucket.ConflictResolution,
			EnableFlush:        bucket.EnableFlush,
			EnableIndexReplica: bucket.EnableIndexReplica,
			CompressionMode:    string(bucket.CompressionMode),
		}
	}

	sort.Strings(names)

	c.cluster.Status.Buckets = []couchbasev2.BucketStatus{}

	for _, name := range names {
		c.cluster.Status.Buckets = append(c.cluster.Status.Buckets, statuses[name])
	}

	return nil
}

// reconcile changes to selected pod labels for
// the nodePort service exposing admin console.
func (c *Cluster) reconcileAdminService() error {
	serviceName := k8sutil.ConsoleServiceName(c.cluster.Name)

	_, ok := c.k8s.Services.Get(serviceName)

	// If the service exists, and it shouldn't delete it.
	if ok && !c.cluster.Spec.Networking.ExposeAdminConsole {
		if err := c.k8s.KubeClient.CoreV1().Services(c.cluster.Namespace).Delete(context.Background(), serviceName, *metav1.NewDeleteOptions(0)); err != nil {
			return err
		}

		log.Info("UI service deleted", "cluster", c.namespacedName(), "name", serviceName)

		c.raiseEvent(k8sutil.AdminConsoleSvcDeleteEvent(serviceName, c.cluster))

		return nil
	}

	if c.cluster.Spec.Networking.ExposeAdminConsole {
		if err := k8sutil.ReconcileAdminConsole(c.k8s, c.cluster); err != nil {
			return err
		}

		// Service didn't exist, notify that it now does!
		if !ok {
			log.Info("UI service created", "cluster", c.namespacedName(), "name", serviceName)

			c.raiseEvent(k8sutil.AdminConsoleSvcCreateEvent(serviceName, c.cluster))

			return nil
		}
	}

	return nil
}

// reconcilePodServices looks at the requested exported feature set in the
// specification and add/removes services as requested, raising events as
// appropriate.
func (c *Cluster) reconcilePodServices() error {
	// When exposed features exist, then ensure every member has the correct
	// service associated with it.
	if c.cluster.Spec.HasExposedFeatures() {
		for _, member := range c.members {
			_, ok := c.k8s.Services.Get(member.Name())

			if err := k8sutil.ReconcilePodService(c.k8s, c.cluster, member); err != nil {
				if goerrors.Is(err, errors.ErrResourceAttributeRequired) {
					log.Info("Unable to generate service for pod", "cluster", c.namespacedName(), "error", err)
					continue
				}

				return err
			}

			if !ok {
				log.Info("Created pod service", "cluster", c.namespacedName(), "name", member.Name())
			}
		}
	}

	// For every pod service ensure it has an associated member and that exposed
	// services are enabled, otherwise delete it.
	for _, service := range c.k8s.Services.List() {
		if _, ok := service.Labels[constants.LabelNode]; !ok {
			continue
		}

		if _, ok := c.members[service.Name]; ok && c.cluster.Spec.HasExposedFeatures() {
			continue
		}

		if err := c.k8s.KubeClient.CoreV1().Services(service.Namespace).Delete(context.Background(), service.Name, *metav1.NewDeleteOptions(0)); err != nil {
			return err
		}

		log.Info("Deleted pod service", "cluster", c.namespacedName(), "name", service.Name)
	}

	return nil
}

// initializes the first member in the cluster.
func (c *Cluster) initMember(m couchbaseutil.Member, serverSpec couchbasev2.ServerConfig) error {
	log.Info("Initial pod creating", "cluster", c.namespacedName())
	settings := c.cluster.Spec.ClusterSettings

	defaults := &couchbaseutil.PoolsDefaults{
		ClusterName:          c.cluster.Name,
		DataMemoryQuota:      k8sutil.Megabytes(settings.DataServiceMemQuota),
		IndexMemoryQuota:     k8sutil.Megabytes(settings.IndexServiceMemQuota),
		SearchMemoryQuota:    k8sutil.Megabytes(settings.SearchServiceMemQuota),
		EventingMemoryQuota:  k8sutil.Megabytes(settings.EventingServiceMemQuota),
		AnalyticsMemoryQuota: k8sutil.Megabytes(settings.AnalyticsServiceMemQuota),
	}

	// Set the cluster name and memory quotas.
	// This needs a retry, I've seen DDNS instability.
	if err := couchbaseutil.SetPoolsDefault(defaults).RetryFor(time.Minute).On(c.api, m); err != nil {
		return err
	}

	// Set index settings.
	indexSettings := &couchbaseutil.IndexSettings{}
	if err := couchbaseutil.GetIndexSettings(indexSettings).On(c.api, m); err != nil {
		return err
	}

	indexSettings.StorageMode = couchbaseutil.IndexStorageMode(settings.IndexStorageSetting)

	if err := couchbaseutil.SetIndexSettings(indexSettings).On(c.api, m); err != nil {
		return err
	}

	// Setup the services running on this node.
	services, err := couchbaseutil.ServiceListFromStringArray(couchbasev2.ServiceList(serverSpec.Services).StringSlice())
	if err != nil {
		return err
	}

	if err := couchbaseutil.SetServices(services).On(c.api, m); err != nil {
		return err
	}

	// Setup the "web UI", which really means set the username and password.
	if err := couchbaseutil.SetWebSettings(c.username, c.password, 8091).On(c.api, m); err != nil {
		return err
	}

	// Enable autofailover by default.
	autoFailoverSettings := &couchbaseutil.AutoFailoverSettings{
		Enabled:  true,
		Timeout:  k8sutil.Seconds(settings.AutoFailoverTimeout),
		MaxCount: settings.AutoFailoverMaxCount,
		FailoverOnDataDiskIssues: couchbaseutil.FailoverOnDiskFailureSettings{
			Enabled:    settings.AutoFailoverOnDataDiskIssues,
			TimePeriod: k8sutil.Seconds(settings.AutoFailoverOnDataDiskIssuesTimePeriod),
		},
		FailoverServerGroup: settings.AutoFailoverServerGroup,
	}

	// This needs a retry, setting the username and password causes server to
	// do something that may reject requests for a short period.
	if err := couchbaseutil.SetAutoFailoverSettings(autoFailoverSettings).RetryFor(time.Minute).On(c.api, m); err != nil {
		return err
	}

	// For some utterly bizarre reason the default is TLS1.0, screw that!
	securitySettings := &couchbaseutil.SecuritySettings{}
	if err := couchbaseutil.GetSecuritySettings(securitySettings).On(c.api, c.readyMembers()); err != nil {
		return err
	}

	securitySettings.TLSMinVersion = couchbaseutil.TLS12

	// This needs a retry, server doesn't gracefully shutdown and give us a response,
	// it may just slam the door shut and give us an EOF.
	if err := couchbaseutil.SetSecuritySettings(securitySettings).RetryFor(time.Minute).On(c.api, c.readyMembers()); err != nil {
		return err
	}

	return nil
}

// Initialize a member with TLS certificates.
func (c *Cluster) initMemberTLS(ctx context.Context, m couchbaseutil.Member) error {
	if !c.cluster.IsTLSEnabled() {
		return nil
	}

	ca, _, _, err := c.getTLSData()
	if err != nil {
		return err
	}

	// Update Couchbase's TLS configuration
	if err := couchbaseutil.SetClusterCACert(ca).InPlaintext().On(c.api, m); err != nil {
		return err
	}

	if err := couchbaseutil.ReloadNodeCert().InPlaintext().On(c.api, m); err != nil {
		return err
	}

	// Wait for the port to come backup with the correct certificate chain
	if err := netutil.WaitForHostPortTLS(ctx, m.GetHostPort(), ca); err != nil {
		return err
	}

	return nil
}

// getServerGroups looks over the spec and collects all server groups
// which are defined.
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
// whuch Couchbase doesn't know about.
func (c *Cluster) createServerGroups(existingGroups *couchbaseutil.ServerGroups) error {
	serverGroups := c.getServerGroups()

	ctx, cancel := context.WithTimeout(c.ctx, extendedRetryPeriod)
	defer cancel()

	// Create any that do not exist
	for i := range serverGroups {
		serverGroup := serverGroups[i]

		if existingGroups.GetServerGroup(serverGroup) != nil {
			continue
		}

		if err := couchbaseutil.CreateServerGroup(serverGroup).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		// 409s have been seen due to this not being updated quick enough, ensure the
		// new server group exists before continuing
		callback := func() error {
			if err := couchbaseutil.ListServerGroups(existingGroups).On(c.api, c.readyMembers()); err != nil {
				return err
			}

			if existingGroups.GetServerGroup(serverGroup) == nil {
				return fmt.Errorf("%w: server group %s not found", errors.NewStackTracedError(errors.ErrCouchbaseServerError), serverGroup)
			}

			return nil
		}

		if err := retryutil.Retry(ctx, 5*time.Second, callback); err != nil {
			return err
		}
	}

	return nil
}

// Given a server group update return the index of the named group
// TODO: Move to gocouchbaseutil as a receiver function.
func serverGroupIndex(update *couchbaseutil.ServerGroupsUpdate, name string) (int, error) {
	for index, group := range update.Groups {
		if group.Name == name {
			return index, nil
		}
	}

	return -1, fmt.Errorf("%w: server group %s undefined", errors.NewStackTracedError(errors.ErrInternalError), name)
}

// reconcileServerGroups looks at the cluster specification, if we have enabled
// server groups, lookup the availability zone the member is in, create the server
// group if it doesn't exist and add the member to the group.
func (c *Cluster) reconcileServerGroups() (bool, error) {
	// Cluster not server group aware
	if !c.cluster.Spec.ServerGroupsEnabled() {
		return false, nil
	}

	// Poll the server for existing information
	existingGroups := &couchbaseutil.ServerGroups{}
	if err := couchbaseutil.ListServerGroups(existingGroups).On(c.api, c.readyMembers()); err != nil {
		return false, err
	}

	// Create any server groups which need defining
	if err := c.createServerGroups(existingGroups); err != nil {
		return false, err
	}

	// Create a server group update
	newGroups := couchbaseutil.ServerGroupsUpdate{
		Groups: []couchbaseutil.ServerGroupUpdate{},
	}

	for _, existingGroup := range existingGroups.Groups {
		newGroup := couchbaseutil.ServerGroupUpdate{
			Name:  existingGroup.Name,
			URI:   existingGroup.URI,
			Nodes: []couchbaseutil.ServerGroupUpdateOTPNode{},
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
			podName := existingMember.HostName.GetMemberName()

			// Just reuse the old server group location on error, the pod is likely down
			scheduledServerGroup, err := k8sutil.GetServerGroup(c.k8s, podName)
			if err != nil {
				scheduledServerGroup = existingGroup.Name
			}

			// TODO: should we flag this as a warning and leave it where it is?
			if scheduledServerGroup == "" {
				return false, fmt.Errorf("%w: server group unset for pod %s", errors.NewStackTracedError(errors.ErrCouchbaseServerError), podName)
			}

			// If the node is in the wrong server group schedule an update
			if scheduledServerGroup != existingGroup.Name {
				update = true
			}

			// Calculate the server group to add the node to
			index, err := serverGroupIndex(&newGroups, scheduledServerGroup)
			if err != nil {
				// You have done something stupid like change the pod label
				return false, fmt.Errorf("server group %s for pod %s undefined: %w", scheduledServerGroup, podName, err)
			}

			// Insert the node in the correct server group
			otpNode := couchbaseutil.ServerGroupUpdateOTPNode{
				OTPNode: existingMember.OTPNode,
			}
			newGroups.Groups[index].Nodes = append(newGroups.Groups[index].Nodes, otpNode)
		}
	}

	// Nothing to do
	if !update {
		return false, nil
	}

	return true, couchbaseutil.UpdateServerGroups(existingGroups.GetRevision(), &newGroups).On(c.api, c.readyMembers())
}

// TEMPORARY HACK
// Does exactly the same as above but tells us if we need to do anything.
func (c *Cluster) wouldReconcileServerGroups() (bool, error) {
	// Cluster not server group aware
	if !c.cluster.Spec.ServerGroupsEnabled() {
		return false, nil
	}

	// Poll the server for existing information
	existingGroups := &couchbaseutil.ServerGroups{}
	if err := couchbaseutil.ListServerGroups(existingGroups).On(c.api, c.readyMembers()); err != nil {
		return false, err
	}

	// Create any server groups which need defining
	if err := c.createServerGroups(existingGroups); err != nil {
		return false, err
	}

	// Look at each node in each existing group building up the update
	// structure and also checking to see whether we need to dispatch this
	// change to Couchbase server
	for _, existingGroup := range existingGroups.Groups {
		for _, existingMember := range existingGroup.Nodes {
			// Extract the scheduled server group for the node
			podName := existingMember.HostName.GetMemberName()

			// Just reuse the old server group location on error, the pod is likely down
			scheduledServerGroup, err := k8sutil.GetServerGroup(c.k8s, podName)
			if err != nil {
				scheduledServerGroup = existingGroup.Name
			}

			// TODO: should we flag this as a warning and leave it where it is?
			if scheduledServerGroup == "" {
				return false, fmt.Errorf("%w: server group unset for pod %s", errors.NewStackTracedError(errors.ErrCouchbaseServerError), podName)
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
func (c *Cluster) createAlternateAddressesExternal(member couchbaseutil.Member) (*couchbaseutil.AlternateAddressesExternal, error) {
	var hostname string

	if c.cluster.Spec.Networking.DNS != nil {
		// Use the user provided DNS name.
		hostname = k8sutil.GetDNSName(c.cluster, member.Name())
	} else {
		// Lookup the node IP the pod is running on.
		var err error
		hostname, err = k8sutil.GetHostIP(c.k8s, member.Name())
		if err != nil {
			return nil, err
		}
	}

	ports, err := k8sutil.GetAlternateAddressExternalPorts(c.k8s, c.cluster.Namespace, member.Name())
	if err != nil {
		return nil, err
	}

	addresses := &couchbaseutil.AlternateAddressesExternal{
		Hostname: hostname,
		Ports:    ports,
	}

	return addresses, nil
}

// waitAlternateAddressReachable waits for advertised addresses to become reachable.
// This takes into account the time taken to create an external load balancer and
// DDNS updates.  Obviously this is a best effort as different DNS servers may behave
// differently, and what we see is not necessarily what the client sees.
func waitAlternateAddressReachable(addresses *couchbaseutil.AlternateAddressesExternal) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// All exposed features contain the admin port, only TLS enabled ports
	// are always guaranteed to exist.
	port := 18091

	if addresses.Ports != nil {
		port = int(addresses.Ports.AdminServicePortTLS)
	}

	// If the address is IPv6, wrap it in brackets as per https://golang.org/pkg/net/#Dial.
	hostname := addresses.Hostname

	ip := net.ParseIP(hostname)
	if ip != nil {
		if strings.Contains(ip.String(), ":") {
			hostname = fmt.Sprintf("[%s]", ip.String())
		}
	}

	return netutil.WaitForHostPort(ctx, fmt.Sprintf("%s:%d", hostname, port))
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
		existingAddresses, err := c.getAlternateAddressesExternal(member)
		if err != nil {
			// If we cannot make contact then just continue, it may have been deleted
			log.Info("External address collection failed", "cluster", c.namespacedName(), "name", member.Name())
			return nil
		}

		// If we don't have any exposed ports, but the node reports it is configured so
		// then remove the configuration.
		if !c.cluster.Spec.HasExposedFeatures() {
			if existingAddresses != nil {
				if err := couchbaseutil.DeleteAlternateAddressesExternal().On(c.api, member); err != nil {
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

		// Don't allow addresses to be advertised unless they can be used.
		if err := waitAlternateAddressReachable(addresses); err != nil {
			return err
		}

		// Check to see if we need to perform any updates, ignoring if not
		if reflect.DeepEqual(addresses, existingAddresses) {
			continue
		}

		// Perform the update
		if err := couchbaseutil.SetAlternateAddressesExternal(addresses).On(c.api, member); err != nil {
			return err
		}
	}

	return nil
}

// Get alternate addresses from server, when server is
// exposed over LoadBalancer then Ports can be ignored.
func (c *Cluster) getAlternateAddressesExternal(member couchbaseutil.Member) (*couchbaseutil.AlternateAddressesExternal, error) {
	existingAddresses := &couchbaseutil.AlternateAddressesExternal{}
	if err := c.getAlternateAddressesExternalInto(member, existingAddresses); err != nil {
		return nil, err
	}

	if existingAddresses.Hostname == "" {
		return nil, nil
	}

	// Remove Ports if member features are exposed with a loadbalancer
	if svc, found := c.k8s.Services.Get(member.Name()); found {
		if svc.Spec.Type == v1.ServiceTypeLoadBalancer {
			existingAddresses.Ports = nil
		}
	}

	return existingAddresses, nil
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

	if err := c.reconcileIndexSettings(); err != nil {
		return err
	}

	if err := c.reconcileQuerySettings(); err != nil {
		return err
	}

	if err := c.reconcileAutoCompactionSettings(); err != nil {
		return err
	}

	c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionManageConfig)

	return nil
}

// ensure autofailover timeout matches spec setting.
func (c *Cluster) reconcileAutoFailoverSettings() error {
	// Get the existing settings
	failoverSettings := &couchbaseutil.AutoFailoverSettings{}
	if err := couchbaseutil.GetAutoFailoverSettings(failoverSettings).On(c.api, c.readyMembers()); err != nil {
		log.Error(err, "Auto-failover settings collection failed", "cluster", c.namespacedName())
		return err
	}

	// Marshal the CR spec into the same type as the existing failover settings
	clusterSettings := c.cluster.Spec.ClusterSettings
	specFailoverSettings := &couchbaseutil.AutoFailoverSettings{
		Enabled:  true,
		Timeout:  k8sutil.Seconds(clusterSettings.AutoFailoverTimeout),
		MaxCount: clusterSettings.AutoFailoverMaxCount,
		FailoverOnDataDiskIssues: couchbaseutil.FailoverOnDiskFailureSettings{
			Enabled:    clusterSettings.AutoFailoverOnDataDiskIssues,
			TimePeriod: k8sutil.Seconds(clusterSettings.AutoFailoverOnDataDiskIssuesTimePeriod),
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
		if err := couchbaseutil.SetAutoFailoverSettings(specFailoverSettings).On(c.api, c.readyMembers()); err != nil {
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

// ensure memory quota's matche spec setting.
func (c *Cluster) reconcileMemoryQuotaSettings() error {
	info := &couchbaseutil.ClusterInfo{}
	if err := couchbaseutil.GetPoolsDefault(info).On(c.api, c.readyMembers()); err != nil {
		return err
	}

	current := info.PoolsDefaults()
	name := c.cluster.Name

	if c.cluster.Spec.ClusterSettings.ClusterName != "" {
		name = c.cluster.Spec.ClusterSettings.ClusterName
	}

	config := c.cluster.Spec.ClusterSettings
	requested := &couchbaseutil.PoolsDefaults{
		ClusterName:          name,
		DataMemoryQuota:      k8sutil.Megabytes(config.DataServiceMemQuota),
		IndexMemoryQuota:     k8sutil.Megabytes(config.IndexServiceMemQuota),
		SearchMemoryQuota:    k8sutil.Megabytes(config.SearchServiceMemQuota),
		EventingMemoryQuota:  k8sutil.Megabytes(config.EventingServiceMemQuota),
		AnalyticsMemoryQuota: k8sutil.Megabytes(config.AnalyticsServiceMemQuota),
	}

	if !reflect.DeepEqual(current, requested) {
		if err := couchbaseutil.SetPoolsDefault(requested).On(c.api, c.readyMembers()); err != nil {
			log.Error(err, "Cluster settings update failed", "cluster", c.namespacedName())
			message := fmt.Sprintf("Unable update memory quota's [data:%v, index:%v, search:%v]: `%s`", config.DataServiceMemQuota, config.IndexServiceMemQuota, config.SearchServiceMemQuota, err.Error())
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
	// Read
	actual := &couchbaseutil.SettingsStats{}
	if err := couchbaseutil.GetSettingsStats(actual).On(c.api, c.readyMembers()); err != nil {
		log.Error(err, "Software notification settings collection failed", "cluster", c.namespacedName())
		return err
	}

	// Modify
	requested := *actual
	requested.SendStats = c.cluster.Spec.SoftwareUpdateNotifications

	// Write
	if reflect.DeepEqual(actual, &requested) {
		return nil
	}

	if err := couchbaseutil.SetSettingsStats(&requested).On(c.api, c.readyMembers()); err != nil {
		log.Error(err, "Software notification settings update failed", "cluster", c.namespacedName())
		message := fmt.Sprintf("Unable update software notification settings: `%v`", err)
		c.cluster.Status.SetConfigRejectedCondition(message)

		return err
	}

	log.Info("Software notification settings updated", "cluster", c.namespacedName())
	c.raiseEvent(k8sutil.ClusterSettingsEditedEvent("update notifications", c.cluster))

	return nil
}

// Compare cluster index settings with spec, reconcile if necessary.
func (c *Cluster) reconcileIndexSettings() error {
	current := couchbaseutil.IndexSettings{}
	if err := couchbaseutil.GetIndexSettings(&current).On(c.api, c.readyMembers()); err != nil {
		log.Error(err, "Index storage settings collection failed", "cluster", c.namespacedName())
		return err
	}

	// By default (the old way), just patch the storage mode on top of the
	// current configuration.
	requested := current
	requested.StorageMode = couchbaseutil.IndexStorageMode(c.cluster.Spec.ClusterSettings.IndexStorageSetting)

	// However, if specified, give the user full control.
	apiSettings := c.cluster.Spec.ClusterSettings.Indexer
	if apiSettings != nil {
		requested = couchbaseutil.IndexSettings{
			Threads:            apiSettings.Threads,
			LogLevel:           couchbaseutil.IndexLogLevel(apiSettings.LogLevel),
			MaxRollbackPoints:  apiSettings.MaxRollbackPoints,
			MemSnapInterval:    int(apiSettings.MemorySnapshotInterval.Milliseconds()),
			StableSnapInterval: int(apiSettings.StableSnapshotInterval.Milliseconds()),
			StorageMode:        couchbaseutil.IndexStorageMode(apiSettings.StorageMode),
		}
	}

	if reflect.DeepEqual(current, requested) {
		return nil
	}

	if err := couchbaseutil.SetIndexSettings(&requested).On(c.api, c.readyMembers()); err != nil {
		log.Error(err, "Index storage settings update failed", "cluster", c.namespacedName())
		c.cluster.Status.SetConfigRejectedCondition(fmt.Sprintf("Unable set index settings: %v", err.Error()))

		return err
	}

	log.Info("Index storage settings updated", "cluster", c.namespacedName())
	c.raiseEvent(k8sutil.ClusterSettingsEditedEvent("index service", c.cluster))

	return nil
}

// reconcileQuerySettings updates anything query related.
func (c *Cluster) reconcileQuerySettings() error {
	// If we aren't managing the endpoint, then leave it alone.
	apiSettings := c.cluster.Spec.ClusterSettings.Query
	if apiSettings == nil {
		return nil
	}

	current := &couchbaseutil.QuerySettings{}
	if err := couchbaseutil.GetQuerySettings(current).On(c.api, c.readyMembers()); err != nil {
		return err
	}

	// Do a shallow copy here, for now, we don't need to copy and pointers.
	requested := *current

	// Hide away the magic numbers with some proper API design.  You explicitly
	// specify whether backfilling is enabled.  If it is, then either set to unlimited
	// or the requested size (in order of precedence).  The size should always be populated
	// with CRD defaulting.
	if apiSettings.BackfillEnabled != nil && *apiSettings.BackfillEnabled {
		if apiSettings.TemporarySpaceUnlimited {
			requested.TemporarySpaceSize = couchbaseutil.QueryTemporarySpaceSizeUnlimited
		} else if apiSettings.TemporarySpace != nil {
			requested.TemporarySpaceSize = k8sutil.Megabytes(apiSettings.TemporarySpace)
		}
	} else {
		requested.TemporarySpaceSize = couchbaseutil.QueryTemporarySpaceSizBackfillDisabled
	}

	if reflect.DeepEqual(current, &requested) {
		return nil
	}

	if err := couchbaseutil.SetQuerySettings(&requested).On(c.api, c.readyMembers()); err != nil {
		return err
	}

	log.Info("Query settings updated", "cluster", c.namespacedName())
	c.raiseEvent(k8sutil.ClusterSettingsEditedEvent("query service", c.cluster))

	return nil
}

// reconcileAutoCompactionSettings sets up disk defragmentation.
func (c *Cluster) reconcileAutoCompactionSettings() error {
	// Read
	current := &couchbaseutil.AutoCompactionSettings{}
	if err := couchbaseutil.GetAutoCompactionSettings(current).On(c.api, c.readyMembers()); err != nil {
		return err
	}

	// Modify
	databaseFragmentationThresholdPercentage := 0
	databaseFragmentationThresholdSize := int64(0)
	viewFragmentationThresholdPercentage := 0
	viewFragmentationThresholdSize := int64(0)

	if c.cluster.Spec.ClusterSettings.AutoCompaction.DatabaseFragmentationThreshold.Percent != nil {
		databaseFragmentationThresholdPercentage = *c.cluster.Spec.ClusterSettings.AutoCompaction.DatabaseFragmentationThreshold.Percent
	}

	if c.cluster.Spec.ClusterSettings.AutoCompaction.DatabaseFragmentationThreshold.Size != nil {
		databaseFragmentationThresholdSize = c.cluster.Spec.ClusterSettings.AutoCompaction.DatabaseFragmentationThreshold.Size.Value()
	}

	if c.cluster.Spec.ClusterSettings.AutoCompaction.ViewFragmentationThreshold.Percent != nil {
		viewFragmentationThresholdPercentage = *c.cluster.Spec.ClusterSettings.AutoCompaction.ViewFragmentationThreshold.Percent
	}

	if c.cluster.Spec.ClusterSettings.AutoCompaction.ViewFragmentationThreshold.Size != nil {
		viewFragmentationThresholdSize = c.cluster.Spec.ClusterSettings.AutoCompaction.ViewFragmentationThreshold.Size.Value()
	}

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

	requested := &couchbaseutil.AutoCompactionSettings{
		AutoCompactionSettings: couchbaseutil.AutoCompactionAutoCompactionSettings{
			DatabaseFragmentationThreshold: couchbaseutil.AutoCompactionDatabaseFragmentationThreshold{
				Percentage: databaseFragmentationThresholdPercentage,
				Size:       databaseFragmentationThresholdSize,
			},
			ViewFragmentationThreshold: couchbaseutil.AutoCompactionViewFragmentationThreshold{
				Percentage: viewFragmentationThresholdPercentage,
				Size:       viewFragmentationThresholdSize,
			},
			ParallelDBAndViewCompaction: c.cluster.Spec.ClusterSettings.AutoCompaction.ParallelCompaction,
			IndexCompactionMode:         "circular",
			IndexCircularCompaction: couchbaseutil.AutoCompactionIndexCircularCompaction{
				DaysOfWeek: "Sunday,Monday,Tuesday,Wednesday,Thursday,Friday,Saturday",
				Interval: couchbaseutil.AutoCompactionInterval{
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

	// Write
	if reflect.DeepEqual(current, requested) {
		return nil
	}

	log.Info("Updating auto compaction settings", "cluster", c.namespacedName())

	if err := couchbaseutil.SetAutoCompactionSettings(requested).On(c.api, c.readyMembers()); err != nil {
		return err
	}

	c.raiseEvent(k8sutil.ClusterSettingsEditedEvent("auto compaction", c.cluster))

	return nil
}

// Verify volumes of a single member.
func (c *Cluster) verifyMemberVolumes(m couchbaseutil.Member) error {
	config := c.cluster.Spec.GetServerConfigByName(m.Config())
	if config == nil {
		// Server class configuration has been deleted, and the member will too
		return nil
	}

	err := k8sutil.IsPodRecoverable(c.k8s, *config, m.Name())
	if err != nil {
		if goerrors.Is(err, errors.ErrNoVolumeMounts) {
			// Pod is not configured for volumes
			return nil
		}

		return err
	}

	return nil
}

// Gets paths to use when initializing data, index, and analytics service.
// Default paths are used unless a claim is specified for the service in which
// case a custom mount path is used.
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

// resourcesEqual compares any-old object and returns true if they are the same.
func (c *Cluster) resourcesEqual(current, requested interface{}) (bool, string) {
	// Using diff here does a number of things to be aware of.  First it marshals
	// the resources to JSON so that "omitempty" removes nil/empty iterables that
	// reflect.DeepEqual would say aren't the same.  Secondly it also sorts maps
	// so the output is deterministic.
	d, err := diff.Diff(current, requested)
	if err != nil {
		log.Error(err, "cluster", c.namespacedName())
		return true, ""
	}

	if d == "" {
		return true, ""
	}

	return false, d
}

// reconcilePods updates pod metadata only, this is mutable.  All other changes are done
// with the upgrade mechanism, as these are immutable and need a replacement.  The assumption
// here is that topology changes, e.g upgrades, have been detected and done before this call.
// If that dodesn't hold, then we risk updating the pod spec annotation and ignoring changes.
func (c *Cluster) reconcilePods() error {
	for name, member := range c.members {
		actual, exists := c.k8s.Pods.Get(name)
		if !exists {
			continue
		}

		// Get what the member should look like.
		serverClass := c.cluster.Spec.GetServerConfigByName(member.Config())
		if serverClass == nil {
			continue
		}

		pvcState, err := k8sutil.GetPodVolumes(c.k8s, member.Name(), c.cluster, *serverClass)
		if err != nil {
			return err
		}

		serverGroup := ""

		if actual.Spec.NodeSelector != nil {
			if group, ok := actual.Spec.NodeSelector[constants.ServerGroupLabel]; ok {
				serverGroup = group
			}
		}

		requested, err := k8sutil.CreateCouchbasePodSpec(c.k8s, member, c.cluster, *serverClass, serverGroup, pvcState)
		if err != nil {
			return err
		}

		// Preserve any labels or annotations that are updated by other means, typically
		// for pods this is only upgrades.
		requested.Annotations[constants.PodSpecAnnotation] = actual.Annotations[constants.PodSpecAnnotation]
		requested.Annotations[constants.CouchbaseVersionAnnotationKey] = actual.Annotations[constants.CouchbaseVersionAnnotationKey]

		if reflect.DeepEqual(actual.Labels, requested.Labels) && reflect.DeepEqual(actual.Annotations, requested.Annotations) {
			continue
		}

		// Don't modify the cache!!
		updated := actual.DeepCopy()
		updated.Labels = requested.Labels
		updated.Annotations = requested.Annotations

		if _, err := c.k8s.KubeClient.CoreV1().Pods(c.cluster.Namespace).Update(context.Background(), updated, metav1.UpdateOptions{}); err != nil {
			return err
		}
	}

	return nil
}

// needsUpgrade does an ordered walk down the list of members, if a member is not
// the correct version then return it as an upgrade canididate  It also returns the
// counts of members in the various versions.
func (c *Cluster) needsUpgrade() (couchbaseutil.MemberSet, error) {
	candidates := couchbaseutil.MemberSet{}

	var moves []scheduler.Move

	if c.cluster.Spec.ServerGroupsEnabled() {
		m, err := c.scheduler.Reschedule()
		if err != nil {
			return nil, err
		}

		for _, move := range m {
			log.V(1).Info("rescheduled member", "cluster", c.namespacedName(), "name", move.Name, "from", move.From, "to", move.To)
		}

		moves = m
	}

	for name, member := range c.members {
		// Get what the member actually looks like.
		actual, exists := c.k8s.Pods.Get(name)
		if !exists {
			continue
		}

		// Get what the member should look like.
		serverClass := c.cluster.Spec.GetServerConfigByName(member.Config())
		if serverClass == nil {
			continue
		}

		pvcState, err := k8sutil.GetPodVolumes(c.k8s, member.Name(), c.cluster, *serverClass)
		if err != nil {
			return nil, err
		}

		pvcsEqual := pvcState == nil || !pvcState.NeedsUpdate()

		// For server groups, if off, then leave it blank.  If it's enabled, default to
		// what was there originally, unless overridden by a resceduling move.
		serverGroup := ""

		if c.cluster.Spec.ServerGroupsEnabled() {
			// Keep the existing selector if one exists.
			if actual.Spec.NodeSelector != nil {
				if group, ok := actual.Spec.NodeSelector[constants.ServerGroupLabel]; ok {
					serverGroup = group
				}
			}

			// Check the rescheuling information for any overrides.
			for _, move := range moves {
				if move.Name == member.Name() {
					serverGroup = move.To

					break
				}
			}
		}

		requested, err := k8sutil.CreateCouchbasePodSpec(c.k8s, member, c.cluster, *serverClass, serverGroup, pvcState)
		if err != nil {
			return nil, err
		}

		// Check the specification at creation with the ones that are requested
		// currently.  If they differ then something has changed and we need to
		// "upgrade".  Otherwise accumulate the number of pods at the correct
		// target configuration.  Do this with reflection as the spec may contain
		// maps (e.g. NodeSelector)
		actualSpec := &v1.PodSpec{}

		if annotation, ok := actual.Annotations[constants.PodSpecAnnotation]; ok {
			if err := json.Unmarshal([]byte(annotation), actualSpec); err != nil {
				return nil, errors.NewStackTracedError(err)
			}
		}

		requestedSpec := &v1.PodSpec{}
		if err := json.Unmarshal([]byte(requested.Annotations[constants.PodSpecAnnotation]), requestedSpec); err != nil {
			return nil, errors.NewStackTracedError(err)
		}

		podsEqual, d := c.resourcesEqual(actualSpec, requestedSpec)

		// Nothing to do, carry on...
		if podsEqual && pvcsEqual {
			continue
		}

		if !pvcsEqual {
			d += pvcState.Diff()
		}

		log.Info("Pod upgrade candidate", "cluster", c.namespacedName(), "name", name, "diff", d)

		candidates.Add(member)
	}

	return candidates, nil
}

// reportUpgrade looks at the current state and any existing upgrade status
// condition, makes condition updates and raises events.
func (c *Cluster) reportUpgrade(status *couchbasev2.UpgradeStatus) error {
	// Look for an existing upgrading condition in the persistent storage.
	upgrading := true

	if _, err := c.state.Get(persistence.Upgrading); err != nil {
		upgrading = false
	}

	// No existing condition, we are guaranteed to be upgrading, as opposed to rolling back.
	// Set the persistent flag and raise an event.
	if !upgrading {
		if err := c.state.Insert(persistence.Upgrading, "true"); err != nil {
			return err
		}

		c.raiseEvent(k8sutil.UpgradeStartedEvent(c.cluster))
	}

	// All reports update the condition to reflect the current progress.
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
	// There is no condition, we weren't upgrading, do nothing
	if _, err := c.state.Get(persistence.Upgrading); err != nil {
		return nil
	}

	// Check to see if there are any more upgrade candidates.
	// If there are then we are still upgrading.
	candidates, err := c.needsUpgrade()
	if err != nil {
		return err
	}

	if !candidates.Empty() {
		return nil
	}

	// Upgrade has completed, raise and event, remove the cluster condition
	// update the current cluster version and clear the upgrading flag in
	// persistent storage.
	version, err := k8sutil.CouchbaseVersion(c.cluster.Spec.CouchbaseImage())
	if err != nil {
		return err
	}

	if err := c.state.Update(persistence.Version, version); err != nil {
		return err
	}

	if err := c.state.Delete(persistence.Upgrading); err != nil {
		return err
	}

	c.raiseEvent(k8sutil.UpgradeFinishedEvent(c.cluster))

	c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionUpgrading)

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
		if err := k8sutil.FlagPodReady(c.k8s, name); err != nil {
			return err
		}
	}

	return k8sutil.ReconcilePDB(c.k8s, c.cluster)
}

// replicationKey returns a unique identifier per replication.
func replicationKey(r couchbaseutil.Replication) string {
	return fmt.Sprintf("%s/%s/%s", r.ToCluster, r.FromBucket, r.ToBucket)
}

func (c *Cluster) listReplications() (couchbaseutil.ReplicationList, error) {
	tasks := &couchbaseutil.TaskList{}
	if err := couchbaseutil.ListTasks(tasks).On(c.api, c.readyMembers()); err != nil {
		return nil, err
	}

	tasks = tasks.FilterType(couchbaseutil.TaskTypeXDCR)

	replications := make(couchbaseutil.ReplicationList, len(*tasks))

	for i, task := range *tasks {
		// Parse the target to recover lost information.
		// Should be in the form /remoteClusters/c4c9af9ad62d8b5f665edac5ffc9c1be/buckets/default
		if task.Target == "" {
			return nil, fmt.Errorf("listReplications: target not populated: %w", errors.NewStackTracedError(errors.ErrCouchbaseServerError))
		}

		parts := strings.Split(task.Target, "/")
		if len(parts) != 5 {
			return nil, fmt.Errorf("listReplications: target incorrectly formatted: %v: %w", task.Target, errors.NewStackTracedError(errors.ErrCouchbaseServerError))
		}

		uuid := parts[2]
		to := parts[4]

		// Lookup the UUID to recover the cluster name.
		cluster, err := c.getRemoteClusterByUUID(uuid)
		if err != nil {
			return nil, err
		}

		// Lookup the settings to recover the compression type.
		settings := &couchbaseutil.ReplicationSettings{}
		if err := couchbaseutil.GetReplicationSettings(settings, uuid, task.Source, to).On(c.api, c.readyMembers()); err != nil {
			return nil, err
		}

		// By now your eyeballs will be dry from all the rolling they are doing.
		replications[i] = couchbaseutil.Replication{
			FromBucket:       task.Source,
			ToCluster:        cluster.Name,
			ToBucket:         to,
			Type:             task.ReplicationType,
			ReplicationType:  "continuous",
			CompressionType:  settings.CompressionType,
			FilterExpression: task.FilterExpression,
			PauseRequested:   settings.PauseRequested,
		}
	}

	return replications, nil
}

// getRemoteClusterByName helps manage the utter horror show that is XDCR
// replications.
func (c *Cluster) getRemoteClusterByName(name string) (*couchbaseutil.RemoteCluster, error) {
	clusters := &couchbaseutil.RemoteClusters{}
	if err := couchbaseutil.ListRemoteClusters(clusters).On(c.api, c.readyMembers()); err != nil {
		return nil, err
	}

	for _, cluster := range *clusters {
		if cluster.Name == name {
			return &cluster, nil
		}
	}

	return nil, fmt.Errorf("lookupUUIDForCluster: no cluster found for name %v: %w", name, errors.NewStackTracedError(errors.ErrCouchbaseServerError))
}

// getRemoteClusterByUUID helps manage the utter horror show that is XDCR
// replications.
func (c *Cluster) getRemoteClusterByUUID(uuid string) (*couchbaseutil.RemoteCluster, error) {
	clusters := &couchbaseutil.RemoteClusters{}
	if err := couchbaseutil.ListRemoteClusters(clusters).On(c.api, c.readyMembers()); err != nil {
		return nil, err
	}

	for _, cluster := range *clusters {
		if cluster.UUID == uuid {
			return &cluster, nil
		}
	}

	return nil, fmt.Errorf("lookupClusterForUUID: no cluster found for uuid %v: %w", uuid, errors.NewStackTracedError(errors.ErrCouchbaseServerError))
}

// reconcileXDCR creates and deletes XDCR connections dynamically.
func (c *Cluster) reconcileXDCR() error {
	if !c.cluster.Spec.XDCR.Managed {
		return nil
	}

	requestedClusters := []couchbaseutil.RemoteCluster{}
	requestedReplications := []couchbaseutil.Replication{}

	for _, cluster := range c.cluster.Spec.XDCR.RemoteClusters {
		// We act as a translation layer here, treating XDCR as just another client
		connectionString, err := url.Parse(cluster.Hostname)
		if err != nil {
			return err
		}

		// Default to host:port
		hostname := connectionString.Host

		// When using http chances are you are using node port networking
		// so will have to specify a port, couchbase means round-robin DNS
		// or SRV, and XDCR will default to 8091.
		// With https and couchbases we need to provide a default of 18091
		// because XDCR has no way of autoconfiguring.  These two modes
		// translate to public addressing, DNS based round-robin and SRV
		// (the port is stripped for the latter).
		switch connectionString.Scheme {
		case "https", "couchbases":
			if connectionString.Port() == "" {
				hostname += ":18091"
			}
		}

		requested := couchbaseutil.RemoteCluster{
			Name:       cluster.Name,
			UUID:       cluster.UUID,
			Hostname:   hostname,
			Network:    connectionString.Query().Get("network"),
			SecureType: couchbaseutil.RemoteClusterSecurityNone,
		}

		if cluster.AuthenticationSecret != nil {
			secret, found := c.k8s.Secrets.Get(*cluster.AuthenticationSecret)
			if !found {
				return fmt.Errorf("%w: unable to get remote cluster authentication secret %s", errors.NewStackTracedError(errors.ErrResourceRequired), *cluster.AuthenticationSecret)
			}

			requested.Username = string(secret.Data["username"])
			requested.Password = string(secret.Data["password"])
		}

		if cluster.TLS != nil {
			if cluster.TLS.Secret != nil {
				secret, found := c.k8s.Secrets.Get(*cluster.TLS.Secret)
				if !found {
					return fmt.Errorf("%w: unable to get remote cluster TLS secret %s", errors.NewStackTracedError(errors.ErrResourceRequired), *cluster.TLS.Secret)
				}

				if _, ok := secret.Data[couchbasev2.RemoteClusterTLSCA]; !ok {
					return fmt.Errorf("%w: CA certificate is required for TLS encryption", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
				}

				// No, we will never support any other type!
				requested.SecureType = couchbaseutil.RemoteClusterSecurityTLS

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

		selector := labels.Everything()

		if cluster.Replications.Selector != nil {
			var err error

			if selector, err = metav1.LabelSelectorAsSelector(cluster.Replications.Selector); err != nil {
				return err
			}
		}

		replications := c.k8s.CouchbaseReplications.List()

		for _, replication := range replications {
			if !selector.Matches(labels.Set(replication.Labels)) {
				continue
			}

			requestedReplications = append(requestedReplications, couchbaseutil.Replication{
				FromBucket:       replication.Spec.Bucket,
				ToCluster:        cluster.Name,
				ToBucket:         replication.Spec.RemoteBucket,
				Type:             "xmem",
				ReplicationType:  "continuous",
				CompressionType:  string(replication.Spec.CompressionType),
				FilterExpression: replication.Spec.FilterExpression,
				PauseRequested:   replication.Spec.Paused,
			})
		}
	}

	currentClusters := &couchbaseutil.RemoteClusters{}
	if err := couchbaseutil.ListRemoteClusters(currentClusters).On(c.api, c.readyMembers()); err != nil {
		return err
	}

	currentReplications, err := c.listReplications()
	if err != nil {
		return err
	}

	// Create any new clusters...
CreateNextCluster:
	for requestedIndex := range requestedClusters {
		requested := requestedClusters[requestedIndex]

		for _, current := range *currentClusters {
			if current.Name == requested.Name {
				// Load up the configuration that changes... OMG!!
				hostname, err := c.state.Get(persistence.GetPersistentKindXDCR(requested.Name, persistence.XDCRHostname))
				if err != nil {
					return err
				}

				current.Hostname = hostname

				// Load up configuration that is written to the API but not returned.
				password, err := c.state.Get(persistence.GetPersistentKindXDCR(requested.Name, persistence.XDCRPassword))
				if err != nil {
					if !goerrors.Is(err, persistence.ErrKeyError) {
						return err
					}
				} else {
					current.Password = password
				}

				key, err := c.state.Get(persistence.GetPersistentKindXDCR(requested.Name, persistence.XDCRClientKey))
				if err != nil {
					if !goerrors.Is(err, persistence.ErrKeyError) {
						return err
					}
				} else {
					current.Key = key
				}

				certificate, err := c.state.Get(persistence.GetPersistentKindXDCR(requested.Name, persistence.XDCRClientCertificate))
				if err != nil {
					if !goerrors.Is(err, persistence.ErrKeyError) {
						return err
					}
				} else {
					current.Certificate = certificate
				}

				// AGGGGGGHHHHH... I set to "external", it comes back blank.  This is a joke!
				// For now just ignore this, I'm not persisting it.
				current.Network = requested.Network

				// Diff, this is safe it doesn't have any slices in it...
				if reflect.DeepEqual(current, requested) {
					continue CreateNextCluster
				}

				log.Info("Updating XDCR remote cluster", "cluster", c.namespacedName(), "remote", requested.Name)
				log.V(2).Info("XDCR connection state", "cluster", c.namespacedName(), "requested", requested, "current", current)

				if err := couchbaseutil.UpdateRemoteCluster(&requested).On(c.api, c.readyMembers()); err != nil {
					return err
				}

				c.raiseEvent(k8sutil.RemoteClusterUpdatedEvent(c.cluster, requested.Name))

				// Remove any existing persistent items associated with the connection.
				// I can imagine someone will try changing from username/password to
				// mTLS at some point and we can't leave the old stuff behind as it will
				// come back from the dead next time around.
				if err := c.state.DeleteXDCR(requested.Name); err != nil {
					return err
				}

				// Save any updatable parameters that will not be returned by a GET from the
				// API.  We will use these to detect and trigger updates.
				if err := c.state.Insert(persistence.GetPersistentKindXDCR(requested.Name, persistence.XDCRHostname), requested.Hostname); err != nil {
					return err
				}

				if requested.Password != "" {
					if err := c.state.Insert(persistence.GetPersistentKindXDCR(requested.Name, persistence.XDCRPassword), requested.Password); err != nil {
						return err
					}
				}

				if requested.Key != "" {
					if err := c.state.Insert(persistence.GetPersistentKindXDCR(requested.Name, persistence.XDCRClientKey), requested.Key); err != nil {
						return err
					}
				}

				if requested.Certificate != "" {
					if err := c.state.Insert(persistence.GetPersistentKindXDCR(requested.Name, persistence.XDCRClientCertificate), requested.Certificate); err != nil {
						return err
					}
				}

				// On to the next!
				continue CreateNextCluster
			}
		}

		log.Info("Creating XDCR remote cluster", "cluster", c.namespacedName(), "remote", requested.Name)
		if err := couchbaseutil.CreateRemoteCluster(&requested).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		c.raiseEvent(k8sutil.RemoteClusterAddedEvent(c.cluster, requested.Name))

		// Save any updatable parameters that will not be returned by a GET from the
		// API.  We will use these to detect and trigger updates.
		if err := c.state.Insert(persistence.GetPersistentKindXDCR(requested.Name, persistence.XDCRHostname), requested.Hostname); err != nil {
			return err
		}

		if requested.Password != "" {
			if err := c.state.Insert(persistence.GetPersistentKindXDCR(requested.Name, persistence.XDCRPassword), requested.Password); err != nil {
				return err
			}
		}

		if requested.Key != "" {
			if err := c.state.Insert(persistence.GetPersistentKindXDCR(requested.Name, persistence.XDCRClientKey), requested.Key); err != nil {
				return err
			}
		}

		if requested.Certificate != "" {
			if err := c.state.Insert(persistence.GetPersistentKindXDCR(requested.Name, persistence.XDCRClientCertificate), requested.Certificate); err != nil {
				return err
			}
		}
	}

	// Create/update any replications...
CreateNextReplication:
	for requestedIndex := range requestedReplications {
		requested := requestedReplications[requestedIndex]

		for _, current := range currentReplications {
			if replicationKey(current) == replicationKey(requested) {
				if !reflect.DeepEqual(current, requested) {
					log.Info("Updating XDCR replication", "cluster", c.namespacedName(), "replication", replicationKey(requested))

					cluster, err := c.getRemoteClusterByName(requested.ToCluster)
					if err != nil {
						return err
					}

					if err := couchbaseutil.UpdateReplication(&requested, cluster.UUID, requested.FromBucket, requested.ToBucket).On(c.api, c.readyMembers()); err != nil {
						return err
					}

					c.raiseEvent(k8sutil.ClusterSettingsEditedEvent("xdcr replication", c.cluster))
				}

				continue CreateNextReplication
			}
		}

		log.Info("Creating XDCR replication", "cluster", c.namespacedName(), "replication", replicationKey(requested))

		if err := couchbaseutil.CreateReplication(&requested).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		c.raiseEvent(k8sutil.ReplicationAddedEvent(c.cluster, replicationKey(requested)))
	}

	// Delete any orphaned replications...
DeleteNextReplication:
	for _, currentReplication := range currentReplications {
		current := currentReplication

		for _, requested := range requestedReplications {
			if replicationKey(current) == replicationKey(requested) {
				continue DeleteNextReplication
			}
		}

		log.Info("Deleting XDCR replication", "cluster", c.namespacedName(), "replication", replicationKey(current))

		cluster, err := c.getRemoteClusterByName(current.ToCluster)
		if err != nil {
			return err
		}

		if err := couchbaseutil.DeleteReplication(&current, cluster.UUID, current.FromBucket, current.ToBucket).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		c.raiseEvent(k8sutil.ReplicationRemovedEvent(c.cluster, replicationKey(current)))
	}

	// Delete any orphaned clusters...
DeleteNextCluster:
	for _, currentCluster := range *currentClusters {
		current := currentCluster

		for _, requested := range requestedClusters {
			if current.Name == requested.Name {
				continue DeleteNextCluster
			}
		}

		log.Info("Deleting XDCR remote cluster", "cluster", c.namespacedName(), "remote", current.Name)

		if err := couchbaseutil.DeleteRemoteCluster(&current).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		c.raiseEvent(k8sutil.RemoteClusterRemovedEvent(c.cluster, current.Name))

		if err := c.state.DeleteXDCR(current.Name); err != nil {
			return err
		}
	}

	return nil
}

// reconcilePersistentStatus examines persistent state and synchronizes and
// dependant cluster status, in case someone or something thought it wise to
// delete it!
func (c *Cluster) reconcilePersistentStatus() error {
	uuid, err := c.state.Get(persistence.UUID)
	if err != nil {
		return err
	}

	version, err := c.state.Get(persistence.Version)
	if err != nil {
		return err
	}

	c.cluster.Status.ClusterID = uuid
	c.cluster.Status.CurrentVersion = version

	if err := c.updateCRStatus(); err != nil {
		log.Info("failed to update cluster status", "cluster", c.namespacedName())
	}

	return nil
}

// gatherBackups returns CouchbaseBackups based on the cluster Spec selector.
func (c *Cluster) gatherBackups() ([]couchbasev2.CouchbaseBackup, error) {
	selector := labels.Everything()

	if c.cluster.Spec.Backup.Selector != nil {
		var err error
		if selector, err = metav1.LabelSelectorAsSelector(c.cluster.Spec.Backup.Selector); err != nil {
			return nil, err
		}
	}

	couchbaseBackups := c.k8s.CouchbaseBackups.List()

	backups := []couchbasev2.CouchbaseBackup{}

	for _, backup := range couchbaseBackups {
		if !selector.Matches(labels.Set(backup.Labels)) {
			continue
		}

		backups = append(backups, *backup)
	}

	return backups, nil
}

// gatherBackupRestores returns CouchbaseBackupRestores based on the cluster Spec selector.
func (c *Cluster) gatherBackupRestores() ([]couchbasev2.CouchbaseBackupRestore, error) {
	selector := labels.Everything()

	if c.cluster.Spec.Backup.Selector != nil {
		var err error
		if selector, err = metav1.LabelSelectorAsSelector(c.cluster.Spec.Backup.Selector); err != nil {
			return nil, err
		}
	}

	couchbaseBackupRestores := c.k8s.CouchbaseBackupRestores.List()

	restores := []couchbasev2.CouchbaseBackupRestore{}

	for _, restore := range couchbaseBackupRestores {
		if !selector.Matches(labels.Set(restore.Labels)) {
			continue
		}

		restores = append(restores, *restore)
	}

	return restores, nil
}

func (c *Cluster) reconcileBackup() error {
	if !c.cluster.Spec.Backup.Managed {
		return nil
	}

	// get existing CouchbaseBackup resources
	currentBackups, err := c.gatherBackups()
	if err != nil {
		return err
	}

	// get cronjobs from all Backups
	cronjobs, err := c.generateCronJobs(currentBackups)
	if err != nil {
		return err
	}

	pvcs := generateBackupPVCs(currentBackups, c.cluster)

	// existing backups tells us about the state of the backup CRDs
	existing := map[string]bool{}
	// mutated tracks if a backup has been created/updated (aka mutated)
	// and so what events need to be raised
	mutated := map[string]bool{}

	for _, cronjob := range cronjobs {
		if current, ok := c.k8s.CronJobs.Get(cronjob.Name); ok {
			// if a backup cronjob needs editing (a backup can have max 2 cronjobs),
			existing[cronjob.Labels[constants.LabelBackup]] = true

			actualSpec := &v1beta1.CronJobSpec{}

			if annotation, ok := current.Annotations[constants.CronjobSpecAnnotation]; ok {
				if err := json.Unmarshal([]byte(annotation), actualSpec); err != nil {
					return errors.NewStackTracedError(err)
				}
			}

			requestedSpec := &v1beta1.CronJobSpec{}
			if err := json.Unmarshal([]byte(cronjob.Annotations[constants.CronjobSpecAnnotation]), requestedSpec); err != nil {
				return errors.NewStackTracedError(err)
			}

			if !reflect.DeepEqual(actualSpec, requestedSpec) {
				// update
				if _, err := c.k8s.KubeClient.BatchV1beta1().CronJobs(c.cluster.Namespace).Update(context.Background(), cronjob, metav1.UpdateOptions{}); err != nil {
					return err
				}

				log.Info("Backup Cronjob updated", "cbbackup", cronjob.Labels[constants.LabelBackup], "cronjob", cronjob.Name)

				mutated[cronjob.Labels[constants.LabelBackup]] = true
			}
		} else {
			// create new cronjob
			if _, err = c.k8s.KubeClient.BatchV1beta1().CronJobs(c.cluster.Namespace).Create(context.Background(), cronjob, metav1.CreateOptions{}); err != nil {
				return err
			}

			log.Info("Backup Cronjob created", "cbbackup", cronjob.Labels[constants.LabelBackup], "cronjob", cronjob.Name)

			mutated[cronjob.Labels[constants.LabelBackup]] = true
		}
	}

	for _, pvc := range pvcs {
		if existingPVC, ok := c.k8s.PersistentVolumeClaims.Get(pvc.Name); ok {
			// PVC update for later k8s versions
			existing[pvc.Name] = true

			if reflect.DeepEqual(pvc.Spec.Resources, existingPVC.Spec.Resources) {
				continue
			}

			updatedPVC := existingPVC.DeepCopy()
			updatedPVC.Spec.Resources = pvc.Spec.Resources

			if _, err := c.k8s.KubeClient.CoreV1().PersistentVolumeClaims(c.cluster.Namespace).Update(context.Background(), updatedPVC, metav1.UpdateOptions{}); err != nil {
				return fmt.Errorf("failed to update PVC: %w", errors.NewStackTracedError(err))
			}

			log.Info("Backup PVC resize pending", "cbbackup", pvc.Name, "new size", updatedPVC.Spec.Resources.Requests[v1.ResourceStorage])

			mutated[pvc.Name] = true
		} else {
			// create new PVC
			if _, err := c.k8s.KubeClient.CoreV1().PersistentVolumeClaims(c.cluster.Namespace).Create(context.Background(), pvc, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("failed to create PVC: %w", errors.NewStackTracedError(err))
			}

			log.Info("Backup PVC created", "cbbackup", pvc.Name)
			mutated[pvc.Name] = true
		}
	}

	for backup := range mutated {
		if existing[backup] {
			// if a resource is updated or if a resource is left over (existing),
			// then we create all other resources and we raise a backup updated event
			log.Info("Backup updated", "cbbackup", backup)

			c.raiseEvent(k8sutil.BackupUpdateEvent(backup, c.cluster))
		} else {
			// if the backup PVC and the cronjob(s) are created then we have
			// created a backup from scratch and we class this as a backup created event
			log.Info("Backup created", "cbbackup", backup)

			c.raiseEvent(k8sutil.BackupCreateEvent(backup, c.cluster))
		}
	}

	// delete
	if err := c.deleteBackups(); err != nil {
		return err
	}

	return nil
}

func (c *Cluster) reconcileBackupRestore() error {
	if !c.cluster.Spec.Backup.Managed {
		return nil
	}

	// poll for an existing CouchbaseBackupRestore resource
	currentRestores, err := c.gatherBackupRestores()
	if err != nil {
		return err
	}

	// for the current CouchbaseBackupRestores, loop through and see if they have a Job created
	for i := range currentRestores {
		currentRestore := currentRestores[i]

		// check if Repo field is populated, if not, try and find the repo
		if len(currentRestore.Spec.Repo) == 0 {
			if err := c.getBackupRepo(&currentRestore); err != nil {
				return err
			}
		}

		requested, err := c.generateRestoreJob(currentRestore)
		if err != nil {
			return err
		}

		k8sutil.ApplyBaseAnnotations(requested)

		// check if restore job already exists
		currentjob, ok := c.k8s.Jobs.Get(requested.Name)
		if ok {
			// update any existing requested specs
			if currentjob.Annotations[constants.CronjobSpecAnnotation] != requested.Annotations[constants.CronjobSpecAnnotation] {
				updatedRestore, err := c.k8s.CouchbaseClient.CouchbaseV2().CouchbaseBackupRestores(c.cluster.Namespace).Update(context.Background(), &currentRestore, metav1.UpdateOptions{})
				if err != nil {
					return err
				}

				// compare the specs
				if updatedRestore.Annotations[constants.CronjobSpecAnnotation] != requested.Annotations[constants.CronjobSpecAnnotation] {
					return fmt.Errorf("%w: inconsistency between requested job and actual job", errors.NewStackTracedError(errors.ErrInternalError))
				}

				log.Info("restore job updated", "cbrestore", currentRestore.Name, "updated job", requested.Name)
			}

			// cleanup completed restores
			if currentjob.Status.Succeeded == 1 {
				if err := c.k8s.CouchbaseClient.CouchbaseV2().CouchbaseBackupRestores(c.cluster.Namespace).Delete(context.Background(), currentRestore.Name, *metav1.NewDeleteOptions(0)); err != nil {
					return err
				}
			}
		} else {
			log.Info("Restore created", "cbrestore", currentRestore.Name)

			c.raiseEvent(k8sutil.BackupRestoreCreateEvent(currentRestore.Name, c.cluster))

			// else try to create the job as it does not exist
			createdJob, err := c.k8s.KubeClient.BatchV1().Jobs(c.cluster.Namespace).Create(context.Background(), requested, metav1.CreateOptions{})
			if err != nil {
				return err
			}

			log.Info("restore job created", "cbrestore", currentRestore.Name, "created job", createdJob.Name)
		}
	}

	jobs := c.k8s.Jobs.List()
	// loop over the current existing jobs
Outerloop:
	for _, job := range jobs {
		if _, ok := job.Labels[constants.LabelBackupRestore]; !ok {
			continue
		}

		// check if the job has an "owner" restore
		for _, currentRestore := range currentRestores {
			if job.Name == currentRestore.Name {
				continue Outerloop
			}
		}

		// no "owner" restore, must have been deleted. cleanup
		if err := c.k8s.KubeClient.BatchV1().Jobs(c.cluster.Namespace).Delete(context.Background(), job.Name, metav1.DeleteOptions{}); err != nil {
			return err
		}

		log.Info("Restore deleted", "cbrestore", job.Name)

		c.raiseEvent(k8sutil.BackupRestoreDeleteEvent(job.Name, c.cluster))
	}

	return nil
}

// reconcileRBAC reconciles users, groups, along with ldap settings.
func (c *Cluster) reconcileRBAC() error {
	// rbac features require 6.5
	version, err := couchbaseutil.NewVersion(c.cluster.Status.CurrentVersion)
	if err != nil {
		return err
	}

	required, _ := couchbaseutil.NewVersion(constants.CouchbaseVersion650)

	if version.GreaterEqual(required) {
		if err := c.reconcileLDAPSettings(); err != nil {
			return err
		}

		if err := c.reconcileRBACResources(); err != nil {
			return err
		}
	} else {
		// Warn if user is attempting to use rbac feature with an unsupported version
		if c.cluster.Spec.Security.LDAP != nil {
			log.V(1).Info("LDAP security settings are not allowed", "cluster", c.namespacedName(), "cluster_version", version, "required_version", constants.CouchbaseVersion650)
		}

		if len(c.k8s.CouchbaseGroups.List()) > 0 {
			log.V(1).Info("RBAC is not allowed", "cluster", c.namespacedName(), "cluster_version", version, "required_version", constants.CouchbaseVersion650)
		}
	}

	return nil
}

// supportsNodeToNode tells us whether the current version supports N2N encryption.
func (c *Cluster) supportsNodeToNode() bool {
	version, err := couchbaseutil.NewVersion(c.cluster.Status.CurrentVersion)
	if err != nil {
		return false
	}

	return version.GreaterEqualString("6.5.1")
}

// nodeToNodeEnabled tells us whether N2N encyption is enabled.
func (c *Cluster) nodeToNodeEnabled() bool {
	return c.cluster.Spec.Networking.TLS != nil && c.cluster.Spec.Networking.TLS.NodeToNodeEncryption != nil
}

// getAlternateAddressesExternal gets the alternate addresses for this node.
// It is *NOT* an error condition for this not to exist, which is an indication
// that the client code is not clever enough to handle the snafu.
func (c *Cluster) getAlternateAddressesExternalInto(m couchbaseutil.Member, alternateAddresses *couchbaseutil.AlternateAddressesExternal) error {
	nodeServices := &couchbaseutil.NodeServices{}
	if err := couchbaseutil.GetNodeServices(nodeServices).On(c.api, m); err != nil {
		return err
	}

	for _, node := range nodeServices.NodesExt {
		if !node.ThisNode {
			continue
		}

		if node.AlternateAddresses != nil {
			*alternateAddresses = *node.AlternateAddresses.External
		}

		return nil
	}

	// The absence of this node is probably due to it not being balanced in yet.
	// /pools/default/nodeServices apparently only shows nodes when the rebalance
	// starts.  Don't raise an error.
	return nil
}

// getNodeNetworkConfiguration gets the network configuration settings for a node.
func (c *Cluster) getNodeNetworkConfiguration(m couchbaseutil.Member, s *couchbaseutil.NodeNetworkConfiguration) error {
	node := &couchbaseutil.NodeInfo{}
	if err := couchbaseutil.GetNodesSelf(node).On(c.api, m); err != nil {
		return err
	}

	onOrOff := couchbaseutil.Off

	if node.NodeEncryption {
		onOrOff = couchbaseutil.On
	}

	*s = couchbaseutil.NodeNetworkConfiguration{
		NodeEncryption: onOrOff,
	}

	return nil
}

// reconcileCompletedPods looks for pods in the completed state.  This occurs after kubelet
// comes backup after a lights-out and the pod restart policy denies reuse of the pod from
// disk.  Which does beg the question, should we allow this?
func (c *Cluster) reconcileCompletedPods() error {
	for _, pod := range c.getClusterPods() {
		// Succeeded and failed means the pod has terminated, kill it!
		// Things to note: members are only considered so if they are Running or
		// Pending, so we need to do no book keeping.  Also persistent recovery
		// will fail unless we delete the pods.
		if pod.Status.Phase != v1.PodSucceeded && pod.Status.Phase != v1.PodFailed {
			continue
		}

		if err := c.k8s.KubeClient.CoreV1().Pods(c.cluster.Namespace).Delete(context.Background(), pod.Name, *metav1.NewDeleteOptions(0)); err != nil {
			return err
		}

		log.Info("Deleted terminated pod", "cluster", c.namespacedName(), "name", pod.Name)
	}

	return nil
}

// reconcileAdminPassword compares the source-of-truth in the perisistent cache
// with what's in the secret, rotating both the cluster and cache.  While not
// atomic, you'd have to be very unlucky for the former to happen and the operator
// bomb out before the latter!
func (c *Cluster) reconcileAdminPassword() error {
	secret, found := c.k8s.Secrets.Get(c.cluster.Spec.Security.AdminSecret)
	if !found {
		return fmt.Errorf("%w: unable to get admin secret %s", errors.NewStackTracedError(errors.ErrResourceRequired), c.cluster.Spec.Security.AdminSecret)
	}

	passwordRaw, ok := secret.Data[constants.AuthSecretPasswordKey]
	if !ok {
		return fmt.Errorf("%w: secret missing password", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	password := string(passwordRaw)

	if password == c.password {
		return nil
	}

	log.Info("Rotating admin password", "cluster", c.namespacedName())

	if err := couchbaseutil.ChangePassword(password).On(c.api, c.readyMembers()); err != nil {
		return err
	}

	if err := c.state.Upsert(persistence.Password, password); err != nil {
		return err
	}

	// Update all the places the password is cached.
	c.password = password

	c.api.SetPassword(password)

	c.raiseEvent(k8sutil.EventReasonAdminPasswordChangedEvent(c.cluster))

	return nil
}

// hibernate puts the cluster to sleep, zzzz.
func (c *Cluster) hibernate() error {
	for _, pod := range c.getClusterPods() {
		log.Info("Hibernating pod", "cluster", c.namespacedName(), "name", pod.Name)

		if err := c.k8s.KubeClient.CoreV1().Pods(c.cluster.Namespace).Delete(context.Background(), pod.Name, *metav1.NewDeleteOptions(0)); err != nil {
			return err
		}
	}

	c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionAvailable)
	c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionBalanced)
	c.cluster.Status.SetHibernatingCondition("Cluster hibernating")

	if err := c.updateCRStatus(); err != nil {
		return err
	}

	log.Info("Cluster is hibernating", "cluster", c.namespacedName())

	return nil
}

// Reconcile Autoscaler ensures all server configs have
// associated Autoscaler CR. Applies changes when sizing
// differs.
func (c *Cluster) reconcileAutoscalers() error {
	requestedAutoscalers := []string{}

	for i, config := range c.cluster.Spec.Servers {
		if config.AutoscaleEnabled {
			// reject stateful configs if when not in preview mode
			if !c.cluster.Spec.EnablePreviewScaling {
				if !config.IsStateless() {
					continue
				}

				// And...all buckets must be ephemeral <* *>
				numStatefulBuckets := len(c.k8s.CouchbaseBuckets.List()) + len(c.k8s.CouchbaseMemcachedBuckets.List())
				if numStatefulBuckets > 0 {
					continue
				}
			}

			requestedAutoscalers = append(requestedAutoscalers, config.Name)

			// Get associated Autoscaler CR
			name := config.AutoscalerName(c.cluster.Name)
			autoscaler, ok := c.k8s.CouchbaseAutoscalers.Get(name)

			if !ok {
				// Create Autoscaler CR
				var err error
				autoscaler, err = k8sutil.CreateCouchbaseAutoscaler(c.k8s, c.cluster, config)

				if err != nil {
					return fmt.Errorf("failed to create autoscaler: %w", errors.NewStackTracedError(err))
				}

				// Eventing
				message := fmt.Sprintf("Autoscaling enabled for config `%s`", config.Name)
				log.Info(message, "cluster", c.namespacedName(), "name", config.Name)
				c.raiseEventCached(k8sutil.AutoscalerCreateEvent(c.cluster, config.Name))
			} else {
				// Reconcile Autoscaler CR with CouchbaseCluster
				requestedSize := autoscaler.Spec.Size
				currentSize := config.Size
				if currentSize != requestedSize {
					// Apply size of Autoscaler CR to server config.
					// It's possible that HPA did not initiate this,
					// but since we implement the /scale api it's
					// possible for cli or other custom implementations
					// to trigger couchbase autoscaling
					c.cluster.Spec.Servers[i].Size = autoscaler.Spec.Size
					cluster, err := c.k8s.CouchbaseClient.CouchbaseV2().CouchbaseClusters(c.cluster.Namespace).Update(context.Background(), c.cluster, metav1.UpdateOptions{})
					if err != nil {
						return fmt.Errorf("failed to update cluster size: %w", errors.NewStackTracedError(err))
					}
					c.cluster = cluster

					// Scaling Events
					message := fmt.Sprintf("Autoscaled config `%s` from %d -> %d", config.Name, currentSize, requestedSize)
					log.Info(message, "cluster", c.namespacedName(), "name", config.Name)
					if currentSize < requestedSize {
						c.raiseEventCached(k8sutil.AutoscaleUpEvent(c.cluster, config.Name, currentSize, requestedSize))
					} else {
						c.raiseEventCached(k8sutil.AutoscaleDownEvent(c.cluster, config.Name, currentSize, requestedSize))
					}
				}
			}

			// update status subresource to actual ready pods from group
			configPods := 0

			for _, pod := range c.getClusterPods() {
				if config.Name == pod.Labels[constants.LabelNodeConf] {
					configPods++
				}
			}

			if configPods != autoscaler.Status.Size {
				autoscaler.Status.Size = configPods
				_, err := c.k8s.CouchbaseClient.CouchbaseV2().CouchbaseAutoscalers(c.cluster.Namespace).UpdateStatus(context.Background(), autoscaler, metav1.UpdateOptions{})

				if err != nil {
					return fmt.Errorf("%w: failed to update autoscaler status: %s", errors.NewStackTracedError(err), autoscaler.Name)
				}
			}
		}
	}

	// Update couchbase cluster status with actual set of autoscalers
	actualAutoscalers := []string{}

	for _, autoscaler := range c.k8s.CouchbaseAutoscalers.List() {
		// delete autoscaler if it is not requested
		configName := autoscaler.Spec.Servers
		if _, found := couchbasev2.HasItem(configName, requestedAutoscalers); !found {
			err := c.k8s.CouchbaseClient.CouchbaseV2().CouchbaseAutoscalers(c.cluster.Namespace).Delete(context.Background(), autoscaler.Name, *metav1.NewDeleteOptions(0))
			if err != nil {
				return err
			}

			message := fmt.Sprintf("Autoscaling disabled for config `%s`", configName)
			log.Info(message, "cluster", c.namespacedName(), "name", configName)
			c.raiseEventCached(k8sutil.AutoscalerDeleteEvent(c.cluster, configName))

			continue
		}

		// ensure autoscaler is added to status since it is requested for use
		actualAutoscalers = append(actualAutoscalers, autoscaler.Name)
	}

	c.cluster.Status.Autoscalers = actualAutoscalers

	return nil
}

// reconcileStatus generates any status attributes that can be statically derrived.
func (c *Cluster) reconcileStatus() error {
	statuses := []couchbasev2.ServerClassStatus{}

	for _, class := range c.cluster.Spec.Servers {
		status := couchbasev2.ServerClassStatus{
			Name:            class.Name,
			AllocatedMemory: &resource.Quantity{},
		}

		if value, ok := class.Resources.Requests["memory"]; ok {
			status.RequestedMemory = &value
		}

		for _, service := range class.Services {
			switch service {
			case couchbasev2.DataService:
				status.AllocatedMemory.Add(*c.cluster.Spec.ClusterSettings.DataServiceMemQuota)
				status.DataServiceAllocation = c.cluster.Spec.ClusterSettings.DataServiceMemQuota
			case couchbasev2.IndexService:
				status.AllocatedMemory.Add(*c.cluster.Spec.ClusterSettings.IndexServiceMemQuota)
				status.IndexServiceAllocation = c.cluster.Spec.ClusterSettings.IndexServiceMemQuota
			case couchbasev2.SearchService:
				status.AllocatedMemory.Add(*c.cluster.Spec.ClusterSettings.SearchServiceMemQuota)
				status.SearchServiceAllocation = c.cluster.Spec.ClusterSettings.SearchServiceMemQuota
			case couchbasev2.EventingService:
				status.AllocatedMemory.Add(*c.cluster.Spec.ClusterSettings.EventingServiceMemQuota)
				status.EventingServiceAllocation = c.cluster.Spec.ClusterSettings.EventingServiceMemQuota
			case couchbasev2.AnalyticsService:
				status.AllocatedMemory.Add(*c.cluster.Spec.ClusterSettings.AnalyticsServiceMemQuota)
				status.AnalyticsServiceAllocation = c.cluster.Spec.ClusterSettings.AnalyticsServiceMemQuota
			}
		}

		if status.RequestedMemory != nil {
			unused := status.RequestedMemory.DeepCopy()
			unused.Sub(*status.AllocatedMemory)
			status.UnusedMemory = &unused

			status.AllocatedMemoryPercent = int((status.AllocatedMemory.Value() * 100) / status.RequestedMemory.Value())
			status.UnusedMemoryPercent = int((status.UnusedMemory.Value() * 100) / status.RequestedMemory.Value())
		}

		statuses = append(statuses, status)
	}

	c.cluster.Status.Allocations = statuses

	return nil
}
