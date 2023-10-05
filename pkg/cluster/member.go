package cluster

import (
	"context"
	goerrors "errors"
	"fmt"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/netutil"

	v1 "k8s.io/api/core/v1"
)

// updateMembers is the canonical source of truth for all membership information.
// It looks at Kubernetes for pods and pvcs (this is ostensibly free).
func (c *Cluster) updateMembers() error {
	// Get all pods these are running and ready for attempted API access.
	runningPods, _ := c.getClusterPodsByPhase()
	runningMembers := podsToMemberSet(runningPods)

	// All pods are members as are persistent volume claims.
	members := couchbaseutil.MemberSet{}
	members.Merge(runningMembers)
	members.Merge(c.pvcMembers())

	// The ready set is nodes that are in the active state.
	ready := couchbaseutil.MemberSet{}

	// The callable set is nodes that can be called with the Couchbase API.
	callable := couchbaseutil.MemberSet{}

	if !runningMembers.Empty() {
		// Try to find a Couchbase node that responds.
		status, err := c.GetStatusUnsafe(runningMembers)
		if err != nil {
			return err
		}

		// Add any nodes that Couchbase knows about that we do not.
		// Don't overwrite anything derived from PVCs as the member
		// metadata will be garbage from Couchbase.
		members.Merge(status.UnknownMembers)

		for name, member := range runningMembers {
			state, ok := status.NodeStates[name]
			if !ok {
				continue
			}

			if state == NodeStateActive {
				ready.Add(member)
			}

			if state.Callable() {
				callable.Add(member)
			}
		}
	}

	// The unready set is the boolean difference of all nodes with the ready set.
	unready := members.Diff(ready)

	// Update the cluster status
	c.updateMemberStatusWithClusterInfo(ready, unready)

	// Update the main cluster configuration
	c.members = members
	c.callableMembers = callable

	return nil
}

func (c *Cluster) newMember(id int, serverSpecName, image string) (couchbaseutil.Member, error) {
	version, err := k8sutil.CouchbaseVersion(image)
	if err != nil {
		return nil, err
	}

	name := couchbaseutil.CreateMemberName(c.cluster.Name, id)

	return couchbaseutil.NewMember(c.cluster.Namespace, c.cluster.Name, name, version, serverSpecName, c.cluster.IsTLSEnabled()), nil
}

func (c *Cluster) pvcMembers() couchbaseutil.MemberSet {
	return k8sutil.PVCToMemberset(c.k8s, c.cluster.Name, c.cluster.Namespace, c.cluster.IsTLSEnabled())
}

func podsToMemberSet(pods []*v1.Pod) couchbaseutil.MemberSet {
	members := couchbaseutil.MemberSet{}

	for _, pod := range pods {
		labels := pod.GetLabels()

		cluster := ""
		if val, ok := labels[constants.LabelCluster]; ok {
			cluster = val
		}

		config := ""
		if val, ok := labels[constants.LabelNodeConf]; ok {
			config = val
		}

		version := ""
		if val, ok := pod.Annotations[constants.CouchbaseVersionAnnotationKey]; ok {
			version = val
		}

		_, secure := pod.Annotations[constants.PodTLSAnnotation]

		members.Add(couchbaseutil.NewMember(pod.Namespace, cluster, pod.Name, version, config, secure))
	}

	return members
}

// logFailedMember outputs any debug information we can about a failed member creation.
func (c *Cluster) logFailedMember(message, name string) {
	log.Info(message, "cluster", c.namespacedName(), "name", name)
	log.V(1).Info(message, "cluster", c.namespacedName(), "name", name, "resource", k8sutil.LogPod(c.k8s, c.cluster.Namespace, name))
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

	// Setup networking.
	if err := c.initMemberNetworking(newMember); err != nil {
		return nil, err
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

	// Enable TLS if requested.
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
	if err := couchbaseutil.SetStoragePaths(dataPath, indexPath, analyticsPaths).RetryFor(time.Minute).On(c.api, newMember); err != nil {
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

	// Add to the cluster. Note we have to use the plain text url as
	// /controller/addNode will not work with a https reference
	services, err := couchbaseutil.ServiceListFromStringArray(couchbasev2.ServiceList(serverSpec.Services).StringSlice())
	if err != nil {
		return newMember, err
	}

	// Get dns name of member being added but reduce to plain text http if insecure annotation exists.
	url := newMember.GetDNSName()

	if _, ok := c.cluster.Annotations[constants.AddNodeInsecureAnnotation]; ok {
		log.Info("Enforcing HTTP to add member", "cluster", c.namespacedName(), "name", newMember.Name())
		url = newMember.GetHostURLPlaintext()
	}

	if err := couchbaseutil.AddNode(url, c.username, c.password, services).RetryFor(extendedRetryPeriod).On(c.api, c.readyMembers()); err != nil {
		return newMember, err
	}

	// Notify that we have added a new member, this makes it callable.
	c.clusterAddMember(newMember)

	log.Info("Pod added to cluster", "cluster", c.namespacedName(), "name", newMember.Name())
	c.raiseEvent(k8sutil.MemberAddEvent(newMember.Name(), c.cluster))

	if err := k8sutil.SetPodInitialized(c.k8s, newMember.Name()); err != nil {
		return newMember, err
	}

	return newMember, nil
}

// Destroys a Couchbase cluster member.
func (c *Cluster) destroyMember(name string, removeVolumes bool) error {
	if err := c.removePod(name, removeVolumes); err != nil {
		return err
	}

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

// createInitialMember picks a server class containing the data service and
// creates a member/pod for it.
func (c *Cluster) createInitialMember() (couchbaseutil.Member, *couchbasev2.ServerConfig, error) {
	if len(c.cluster.Spec.Servers) == 0 {
		return nil, nil, fmt.Errorf("cluster create: no server specification defined: %w", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	index := c.indexOfServerConfigWithService(couchbasev2.DataService)
	if index == -1 {
		return nil, nil, fmt.Errorf("%w: cluster create: at least one server specification must contain the data service", errors.NewStackTracedError(errors.ErrConfigurationInvalid))
	}

	c.members = couchbaseutil.NewMemberSet()

	class := c.cluster.Spec.Servers[index]

	member, err := c.createMember(class)
	if err != nil {
		return nil, nil, err
	}

	// Notify that we have added a new member, this makes it callable.
	c.clusterAddMember(member)

	return member, &class, nil
}

// configureInitialMember sets up passwords, defaults, that kind of stuff.  It's unlikely
// that this can go wrong, you've probably bypassed the admission controller (naughty)...
func (c *Cluster) configureInitialMember(member couchbaseutil.Member, class *couchbasev2.ServerConfig) error {
	if err := c.initMember(member, class); err != nil {
		// ... if we fail to initialize the cluster, then chances are we won't be
		// able to contact it and get stuck.  Like all pod creation/recreation code
		// we should clean up and let retries potentially work, either as transient
		// errors clear up, or the user unbreaks their bad configuration.
		c.raiseEventCached(k8sutil.MemberCreationFailedEvent(member.Name(), c.cluster))

		// Remove the volumes too, we want to recreate them in case they are the
		// problem.  They contain no data at this point.
		if err := c.removePod(member.Name(), true); err != nil {
			// Unlikely, print the error in scope, propagate the outer error.
			log.Info("Unable to remove failed member", "cluster", c.namespacedName(), "error", err)
		}

		return err
	}

	if err := k8sutil.SetPodInitialized(c.k8s, member.Name()); err != nil {
		return err
	}

	return nil
}

// initializes the first member in the cluster.
func (c *Cluster) initMember(m couchbaseutil.Member, serverSpec *couchbasev2.ServerConfig) error {
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

	indexSettings.StorageMode = couchbaseutil.IndexStorageMode(c.cluster.IndexStorageMode())

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

	// we're going to ignore AutoFailoverServerGroup for server 7.1 as it is no longer part of the couchbase api.
	failoverServerGroup := false
	if over71, err := c.IsAtLeastVersion("7.1.0"); !over71 && err == nil {
		failoverServerGroup = settings.AutoFailoverServerGroup
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
		FailoverServerGroup: failoverServerGroup,
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

	ok, err := c.IsAtLeastVersion("7.1.0")
	if err != nil {
		return err
	}

	if ok {
		return c.initMemberTLSNew(ctx, m)
	}

	return c.initMemberTLSLegacy(ctx, m)
}

// initMemberTLSNew handles TLS initialization on CBS versions 7.1+.
func (c *Cluster) initMemberTLSNew(ctx context.Context, m couchbaseutil.Member) error {
	if err := couchbaseutil.LoadCAs().InPlaintext().On(c.api, m); err != nil {
		return err
	}

	settings, err := c.passphraseSettings()
	if err != nil {
		return err
	}

	if err := couchbaseutil.ReloadNodeCert(settings).InPlaintext().On(c.api, m); err != nil {
		return err
	}

	// Wait for the port to come backup with the correct certificate chain
	if err := netutil.WaitForHostPortTLS(ctx, m.GetHostPort(), c.tlsCache.serverCA); err != nil {
		return err
	}

	return nil
}

// initMemberTLSLegacy handles TLS initialization on CBS versions <=7.0.
func (c *Cluster) initMemberTLSLegacy(ctx context.Context, m couchbaseutil.Member) error {
	// Update Couchbase's TLS configuration
	if err := couchbaseutil.SetClusterCACert(c.tlsCache.serverCA).InPlaintext().On(c.api, m); err != nil {
		return err
	}

	settings, err := c.passphraseSettings()
	if err != nil {
		return err
	}

	if err := couchbaseutil.ReloadNodeCert(settings).InPlaintext().On(c.api, m); err != nil {
		return err
	}

	// Wait for the port to come backup with the correct certificate chain
	if err := netutil.WaitForHostPortTLS(ctx, m.GetHostPort(), c.tlsCache.serverCA); err != nil {
		return err
	}

	return nil
}

func (c *Cluster) updateMemberStatus(firstMember bool) error {
	// Hack, NS server doesn't start to work properly until the full initialization
	// sequence is performed, so a call to /pools/default will not work :sadpanda:
	// We need to avoid this code path in order to avoid this hack.
	if firstMember {
		c.updateMemberStatusWithClusterInfo(c.members, nil)
		return nil
	}

	status, err := c.GetStatus()
	if err != nil {
		return err
	}

	ready := couchbaseutil.MemberSet{}

	for name, member := range c.members {
		state, ok := status.NodeStates[name]
		if !ok {
			continue
		}

		if state == NodeStateActive {
			ready.Add(member)
		}
	}

	unready := c.members.Diff(ready)

	c.updateMemberStatusWithClusterInfo(ready, unready)

	return nil
}

// use cluster info to set ready members from active nodes
// and all remaining nodes as unready.
func (c *Cluster) updateMemberStatusWithClusterInfo(ready, unready couchbaseutil.MemberSet) {
	if c.cluster.Status.Members == nil {
		c.cluster.Status.Members = &couchbasev2.MembersStatus{}
	}

	c.cluster.Status.Members.SetReady(ready.Names())
	c.cluster.Status.Members.SetUnready(unready.Names())

	if err := c.updateCRStatus(); err != nil {
		log.Info("unable to update status", "cluster", c.namespacedName(), "error", err)
	}
}

// clients should use ready members that are available to service requests
// according to status readiness.  Otherwise, fallback to cluster members.
func (c *Cluster) readyMembers() couchbaseutil.MemberSet {
	// This used to cross reference with pod liveness.  Why?
	// Performance improvment?  UX?
	return c.callableMembers
}

// Check if volume only has log volumes mounted.
func (c *Cluster) memberHasLogVolumes(name string) bool {
	return k8sutil.MemberHasLogVolumes(c.k8s, name)
}

// Verify volumes of a single member.
func (c *Cluster) verifyMemberVolumes(m couchbaseutil.Member) error {
	config := c.cluster.Spec.GetServerConfigByName(m.Config())
	if config == nil {
		// Server class configuration has been deleted, and the member will too
		return nil
	}

	err := k8sutil.IsPodRecoverable(c.k8s, *config, m)
	if err != nil {
		if goerrors.Is(err, errors.ErrNoVolumeMounts) {
			// Pod is not configured for volumes
			return nil
		}

		return err
	}

	return nil
}
