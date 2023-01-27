package cluster

import (
	"context"
	goerrors "errors"
	"fmt"
	"reflect"
	"regexp"
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
	"github.com/couchbase/couchbase-operator/pkg/util/scheduler"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// extendedRetryPeriod is an extended amount of time to wait for slow
	// API operations to report success.
	extendedRetryPeriod = 3 * time.Minute

	// secretSyncTimePeriod is approximately how long it takes for secret
	// updates to take effect in pod file systems.  Note, there is a race
	// because TLS updates aren't atomic.  It's possible for the shadow
	// secret to skip being updated, but the certs to be spotted as changed
	// and a futile reload cycle entered.  Setting this too high will cause
	// test failures.  I guess we need to check TLS at the start of the
	// loop and cache the certs/keys.  Better but not perfect.
	secretSyncTimePeriod = 2 * time.Minute
)

// reconcileFunc is a stable and consistent interface to a reconcile function.
// Where there is consistency, then there is peace, happiness and unicorns.
type reconcileFunc func(*Cluster) error

// reconcileFuncList wraps up reconcile functions into an ordered list of
// execution.
type reconcileFuncList []reconcileFunc

// run executes each reconcile function, in-order, and bombs out if anything
// goes wrong.
func (l reconcileFuncList) run(c *Cluster) error {
	for _, f := range l {
		if err := f(c); err != nil {
			return err
		}
	}

	return nil
}

// reconcile is the main function for the whole cluster lifecycle.  It works like this:
//
//   - Deletes any completed pods, these are not considered as members, but do pollute the
//     namespace and will prevent clusters from being resurrected.  Pods typically enter this
//     state when Kubernetes comes back from the dead.
//   - Setup any networking, this is essential for the Operator to be able to contact any
//     pods to perform initialization or reconciliation actions.
//   - Create an initial member if there are no members, this handles creation and recovery
//     if an in-memory cluster has vanished.
//   - Fix up TLS, we will be completely unable to communicate with the cluster if this is
//     not performed first.
//   - Topology and feature updates.
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
	}

	if err := preCreationReconcilers.run(c); err != nil {
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

	// Run pre-topology change reconcilers.  These are the things that need
	// a running cluster, but must be performed before any pods are modified.
	// These should not use c.members, preferring c.callableMembers to avoid
	// log spam until the cluster is repaired.
	preTopologyReconcilers := reconcileFuncList{
		(*Cluster).reconcilePersistentStatus,
		(*Cluster).reconcileAdminPassword,
		(*Cluster).reconcileTLSPreTopologyChange,
	}

	if err := preTopologyReconcilers.run(c); err != nil {
		return err
	}

	fsm, err := c.newReconcileMachine()
	if err != nil {
		return err
	}

	if aborted, err := c.reconcileMembers(fsm); err != nil {
		return err
	} else if aborted {
		return nil
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

	// Run post-topology reconcilers.  These typically manage Couchbase
	// features, and we are guaranteed that the cluster is in a stable
	// and happy state (most of the time, races can still occur).
	postTopologyReconcilers := reconcileFuncList{
		(*Cluster).reconcilePersistentStatus,
		(*Cluster).reconcileClusterNetworking,
		(*Cluster).reconcileTLSPostTopologyChange,
		(*Cluster).reconcilePods,
		(*Cluster).reconcileClusterSettings,
		(*Cluster).reconcileBuckets,
		(*Cluster).reconcileScopesAndCollections,
		(*Cluster).reconcileSynchronizeBuckets,
		(*Cluster).reconcileXDCR,
		(*Cluster).reconcileReadiness,
		(*Cluster).reconcileAdminService,
		(*Cluster).reconcilePodServices,
		(*Cluster).reconcileMemberAlternateAddresses,
		(*Cluster).reconcileRBAC,
		(*Cluster).reconcileBackup,
		(*Cluster).reconcileBackupRestore,
		(*Cluster).reconcileAutoscalers,
		(*Cluster).reconcileEndpointProxyService,
	}

	if err := postTopologyReconcilers.run(c); err != nil {
		return err
	}

	c.cluster.Status.Size = c.members.Size()
	c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionScaling)
	c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionScalingDown)
	c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionScalingUp)

	c.cluster.Status.SetBalancedCondition()
	c.cluster.Status.SetReadyCondition()

	return nil
}

func (c *Cluster) reconcilePeerServices() error {
	return k8sutil.ReconcilePeerServices(c.k8s, c.cluster)
}

// reconcileMembers reconciles
// - running pods on k8s and cluster membership
// - cluster membership and expected size of couchbase cluster
// Steps:
// 1. Remove running pods that we didn't create explicitly (unknown members)
// 2. If we are currently in a rebalance then we should finish it.
// 3. If any nodes are down then wait for them to be failed over.
// 4. Decide which nodes should be removed and whether we need to add nodes.
//   - Nodes added to the cluster that are failed, but not rebalanced should
//     be removed.
//   - Nodes that have been failed over should be removed from the cluster
//     and rebalanced out.
//   - Nodes that are pending addition, healthy, but not yet rebalanced in
//     should be fully added in.
//   - Healthy active nodes should remain in the cluster.
//     5. We will now know what the current cluster would look like if we handled
//     any issues with the current nodes in the cluster. We now need to either
//     remove healthy nodes if we're scaling down, or add noew nodes if we need
//     to scale up.
//     6. Run a rebalance if necessary.
//     7. Remove any nodes from the cached member set that are not part actually
//     part of the cluster.
//
// Returns true if reconciliation completed properly.
func (c *Cluster) reconcileMembers(rm *ReconcileMachine) (bool, error) {
	return rm.exec(c)
}

// reconcileAdminService changes to selected pod labels for
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

// reconcileEndpointProxyService looks for changes in endpoint proxy feature enablement
// and it's subfeatures.
func (c *Cluster) reconcileEndpointProxyService() error {
	return k8sutil.ReconcileEndpointProxyService(c.k8s, c.cluster)
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

	if err := c.reconcileDataSettings(); err != nil {
		return err
	}

	if err := c.reconcileIndexSettings(); err != nil {
		return err
	}

	if err := c.reconcileQuerySettings(); err != nil {
		return err
	}

	if err := c.reconcileAuditSettings(); err != nil {
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

	failoverServerGroup := false
	if over71, err := c.IsAtLeastVersion("7.1.0"); !over71 && err == nil {
		failoverServerGroup = c.cluster.Spec.ClusterSettings.AutoFailoverServerGroup
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
		FailoverServerGroup: failoverServerGroup,
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

// reconcileDataSettings updates stuff to do with the data service.
func (c *Cluster) reconcileDataSettings() error {
	// This is yet another quality API.  The values don't exist until you set them,
	// then remain for the rest of existence.  So if nothing is set, then leave it
	// alone.
	if c.cluster.Spec.ClusterSettings.Data == nil {
		return nil
	}

	current := couchbaseutil.MemcachedGlobals{}
	if err := couchbaseutil.GetMemcachedGlobalSettings(&current).On(c.api, c.readyMembers()); err != nil {
		return err
	}

	requested := current

	if c.cluster.Spec.ClusterSettings.Data.ReaderThreads != 0 {
		requested.NumReaderThreads = c.cluster.Spec.ClusterSettings.Data.ReaderThreads
	}

	if c.cluster.Spec.ClusterSettings.Data.WriterThreads != 0 {
		requested.NumWriterThreads = c.cluster.Spec.ClusterSettings.Data.WriterThreads
	}

	if reflect.DeepEqual(current, requested) {
		return nil
	}

	if err := couchbaseutil.SetMemcachedGlobalSettings(&requested).On(c.api, c.readyMembers()); err != nil {
		return err
	}

	log.Info("Memcached settings updated", "cluster", c.namespacedName())
	c.raiseEvent(k8sutil.ClusterSettingsEditedEvent("data service", c.cluster))

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
		// check for 7.0+ to add replicas
		if ok, err := c.IsAtLeastVersion("7.0.0"); ok && err == nil {
			requested.NumberOfReplica = apiSettings.NumberOfReplica
			requested.RedistributeIndexes = apiSettings.RedistributeIndexes
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

// Compare cluster audit settings with spec, reconcile if necessary.
func (c *Cluster) reconcileAuditSettings() error {
	// If auditing is not controlled by CRD then ignore
	auditSettings := c.cluster.Spec.Logging.Audit
	if auditSettings == nil {
		return nil
	}

	// Retrieve the current values to reconcile against (and pick up defaults)
	current := couchbaseutil.AuditSettings{}
	if err := couchbaseutil.GetAuditSettings(&current).On(c.api, c.readyMembers()); err != nil {
		log.Error(err, "Audit settings collection failed", "cluster", c.namespacedName())
		return err
	}

	// Copy over the current settings to maintain any defaults set by CB server.
	// No need for a deep copy as the arrays are explicitly always set again later.
	requested := current
	// Log path should never be set, we do not support relocation
	requested.LogPath = k8sutil.CouchbaseVolumeMountLogsDir

	// If auditing is explicitly configured then it can be set to disabled
	requested.Enabled = auditSettings.Enabled

	// For DeepEquals to work, we need to make sure the array is not nil but empty
	if auditSettings.DisabledEvents != nil {
		requested.DisabledEvents = auditSettings.DisabledEvents
	} else {
		requested.DisabledEvents = []int{}
	}

	// Deal with the conversion between two quite different arrays: JSON array of structs and CSV string
	requested.DisabledUsers = []couchbaseutil.AuditUser{}

	// The users are actually a compound string intended to feed a two-element struct (which is returned!)
	// The disabledUsers parameter disables filterable-event auditing on a per user basis.
	// Its value must be a list of users, specified as a comma-separated list, with no spaces. Each user may be:
	// 1. A local user, specified in the form localusername/local.
	// 2. An external user, specified in the form externalusername/external.
	// 3. An internal user, specified in the form @internalusername/local.
	//
	// We add a quick validation check to make sure these match and prevent being rejected by the API later: https://regex101.com/r/ubrkyg/2
	userValidator := regexp.MustCompile("^.+/(local|external)$")

	for _, s := range auditSettings.DisabledUsers {
		// Usage in a loop prefers a compiled regex
		stringValue := string(s)

		valid := userValidator.MatchString(stringValue)
		if !valid {
			err := fmt.Errorf("%w: audit defaults invalid user: %s", errors.NewStackTracedError(errors.ErrConfigurationInvalid), s)
			return err
		}

		splitValues := strings.Split(stringValue, "/")
		numberOfSplits := len(splitValues)

		// At this point must have a string with a slash in it but we may have more with some weirdness matching the regex
		if numberOfSplits == 2 {
			requested.DisabledUsers = append(requested.DisabledUsers, couchbaseutil.AuditUser{
				Name:   splitValues[0],
				Domain: splitValues[1],
			})
		} else {
			err := fmt.Errorf("%w: audit defaults invalid split (%d) of user: %s", errors.NewStackTracedError(errors.ErrConfigurationInvalid), numberOfSplits, s)
			return err
		}
	}

	if auditSettings.Rotation != nil {
		if auditSettings.Rotation.Interval != nil {
			requested.RotateInterval = int(auditSettings.Rotation.Interval.Seconds())
		}

		if auditSettings.Rotation.Size != nil {
			requested.RotateSize = int(auditSettings.Rotation.Size.Value())
		}
	}

	if reflect.DeepEqual(current, requested) {
		return nil
	}

	if err := couchbaseutil.SetAuditSettings(requested).On(c.api, c.readyMembers()); err != nil {
		log.Error(err, "Audit settings update failed", "cluster", c.namespacedName(), "old", current, "new", requested)
		c.cluster.Status.SetConfigRejectedCondition(fmt.Sprintf("Unable to set audit settings: %v", err.Error()))

		return err
	}

	log.Info("Audit settings updated", "cluster", c.namespacedName(), "old", current, "new", requested)
	c.raiseEvent(k8sutil.ClusterSettingsEditedEvent("audit", c.cluster))

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
		},
		PurgeInterval: c.cluster.Spec.ClusterSettings.AutoCompaction.TombstonePurgeInterval.Hours() / 24.0,
	}

	// Time window should only be provided if explicitly specified by user
	if c.cluster.Spec.ClusterSettings.AutoCompaction.TimeWindow.Start != nil {
		// Even though the DAC has validated the time window to ensure appropriate end time,
		// some people don't care about the DAC so some checks are still needed.
		if c.cluster.Spec.ClusterSettings.AutoCompaction.TimeWindow.End != nil {
			autoCompactionTimePeriod := couchbaseutil.AutoCompactionAllowedTimePeriod{
				AbortOutside: c.cluster.Spec.ClusterSettings.AutoCompaction.TimeWindow.AbortCompactionOutsideWindow,
			}
			parts := strings.Split(*c.cluster.Spec.ClusterSettings.AutoCompaction.TimeWindow.Start, ":")
			autoCompactionTimePeriod.FromHour, _ = strconv.Atoi(parts[0])
			autoCompactionTimePeriod.FromMinute, _ = strconv.Atoi(parts[1])
			parts = strings.Split(*c.cluster.Spec.ClusterSettings.AutoCompaction.TimeWindow.End, ":")
			autoCompactionTimePeriod.ToHour, _ = strconv.Atoi(parts[0])
			autoCompactionTimePeriod.ToMinute, _ = strconv.Atoi(parts[1])
			requested.AutoCompactionSettings.AllowedTimePeriod = &autoCompactionTimePeriod
		}
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
			analyticsPaths = k8sutil.GetAnalyticsVolumePaths(mounts)
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

// reconcileReadiness marks the pods as "ready" once everything is repaired, scaled and
// balanced.  It also reconciles a pod disruption budget so we only tolerate a certain
// number of evictions during a drain i.e. k8s upgrade.
func (c *Cluster) reconcileReadiness() error {
	for name := range c.callableMembers {
		if err := k8sutil.FlagPodReady(c.k8s, name); err != nil {
			return err
		}
	}

	return k8sutil.ReconcilePDB(c.k8s, c.cluster)
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

// reconcileRBAC reconciles users, groups, along with ldap settings.
func (c *Cluster) reconcileRBAC() error {
	// When RBAC management is enabled, then all related resources
	// such as users, groups, collections, and scopes, are fully managed.
	if c.cluster.Spec.Security.RBAC.Managed {
		if err := c.reconcileRBACResources(); err != nil {
			return err
		}
	}

	// When LDAP settings are provided to Couchbase Cluster then
	// LDAP is also managed.
	// When empty, then LDAP can be manually managed.
	if c.cluster.Spec.Security.LDAP != nil {
		if err := c.reconcileLDAPSettings(); err != nil {
			return err
		}
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
