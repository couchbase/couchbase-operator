package cluster

import (
	"encoding/json"
	goerrors "errors"
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/cluster/persistence"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/annotations"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

var (
	ErrXDCRDuplicateReplication          = fmt.Errorf("duplicate replication")
	ErrXDCRMigrationDefaultFilterInUse   = fmt.Errorf("invalid migration rule as default filter in use")
	ErrXDCRMigrationNoRules              = fmt.Errorf("no migration rules defined")
	ErrXDCRMigrationNoTargetCollection   = fmt.Errorf("no target collection specified")
	ErrXDCRReplicationInvalidMappingRule = fmt.Errorf("invalid replication rule")
	ErrXDCRCheckFailed                   = fmt.Errorf("XDCR pre check failed")
)

const (
	defaultMigrationFilter                    = couchbasev2.DefaultScopeOrCollection + "." + couchbasev2.DefaultScopeOrCollection
	RemoteClusterOperatorManagedSuffix string = "-operator-managed"
)

// replicationKey returns a unique identifier per replication.
func replicationKey(r couchbaseutil.Replication) string {
	return fmt.Sprintf("%s/%s/%s", r.ToCluster, r.FromBucket, r.ToBucket)
}

func (c *Cluster) ListReplications() (couchbaseutil.ReplicationList, error) {
	mappingsPossible, err := c.isScopesAndCollectionsSupported()
	if err != nil {
		return nil, err
	}

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
			return nil, fmt.Errorf("ListReplications: target not populated: %w", errors.NewStackTracedError(errors.ErrCouchbaseServerError))
		}

		parts := strings.Split(task.Target, "/")
		if len(parts) != 5 {
			return nil, fmt.Errorf("ListReplications: target incorrectly formatted: %v: %w", task.Target, errors.NewStackTracedError(errors.ErrCouchbaseServerError))
		}

		uuid := parts[2]
		to := parts[4]

		// Lookup the UUID to recover the cluster name.
		cluster, err := c.getRemoteClusterByUUID(uuid)
		if err != nil {
			return nil, err
		}

		// Lookup the settings to recover the compression type and any mappings - this must be done per-replication
		settings := &couchbaseutil.ReplicationSettings{}
		if err := couchbaseutil.GetReplicationSettings(settings, uuid, task.Source, to).On(c.api, c.readyMembers()); err != nil {
			return nil, err
		}

		// By now your eyeballs will be dry from all the rolling they are doing.
		newReplication := couchbaseutil.Replication{
			// Core immutable fields
			FromBucket:         task.Source,
			ToCluster:          cluster.Name,
			ToBucket:           to,
			Type:               couchbaseutil.ReplicationTypeXMEM,
			ReplicationType:    couchbaseutil.ReplicationReplicationTypeContinuous,
			FilterSkipRestream: settings.FilterSkipRestream,

			// Legacy core fields (from server settings)
			PauseRequested: settings.PauseRequested,

			// Advanced settings (only those supported during creation)
			CompressionType:                settings.CompressionType,
			DesiredLatency:                 settings.DesiredLatency,
			FilterExpression:               settings.FilterExpression,
			FilterDeletion:                 settings.FilterDeletion,
			FilterExpiration:               settings.FilterExpiration,
			FilterBypassExpiry:             settings.FilterBypassExpiry,
			FilterBinary:                   settings.FilterBinary,
			Priority:                       settings.Priority,
			OptimisticReplicationThreshold: settings.OptimisticReplicationThreshold,
			FailureRestartInterval:         settings.FailureRestartInterval,
			DocBatchSizeKb:                 settings.DocBatchSizeKb,
			WorkerBatchSize:                settings.WorkerBatchSize,
			CheckpointInterval:             settings.CheckpointInterval,
			SourceNozzlePerNode:            settings.SourceNozzlePerNode,
			TargetNozzlePerNode:            settings.TargetNozzlePerNode,
			StatsInterval:                  settings.StatsInterval,
			LogLevel:                       settings.LogLevel,
			NetworkUsageLimit:              settings.NetworkUsageLimit,
			Mobile:                         settings.Mobile,
		}

		// Deal with any additional mappings for scopes and collections
		if mappingsPossible {
			newReplication.ExplicitMapping = settings.CollectionsExplicitMapping
			newReplication.MigrationMapping = settings.CollectionsMigrationMode

			if settings.ColMappingRules != nil {
				newReplication.MappingRules = settings.ColMappingRules
			}
		}

		if settings.ConflictLogging != nil {
			newReplication.ConflictLogging = settings.ConflictLogging
		}

		replications[i] = newReplication
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

func generateKeyspace(k couchbasev2.CouchbaseReplicationKeyspace) string {
	if k.Collection != "" {
		return fmt.Sprintf("%s.%s", k.Scope, k.Collection)
	}

	return string(k.Scope)
}

func generateMigrationMappingRules(migration *couchbasev2.CouchbaseMigrationReplication) (string, error) {
	rules := make(map[string]string)

	for _, mapping := range migration.MigrationMapping.Mappings {
		// If we use the default filter to grab everything then nothing else can be filtered
		if mapping.Filter == defaultMigrationFilter && len(migration.MigrationMapping.Mappings) > 1 {
			return "", ErrXDCRMigrationDefaultFilterInUse
		}

		if mapping.TargetKeyspace.Collection == "" {
			return "", ErrXDCRMigrationNoTargetCollection
		}

		target := generateKeyspace(mapping.TargetKeyspace)
		rules[mapping.Filter] = target
	}

	// We must have some rules or else it is anarchy!
	if len(rules) == 0 {
		return "", ErrXDCRMigrationNoRules
	}

	bytes, err := json.Marshal(rules)
	if err != nil {
		return "", fmt.Errorf("%w: unable to marshal JSON for explicit mapping rules", err)
	}

	return string(bytes), nil
}

func generateExplicitMappingRules(replication *couchbasev2.CouchbaseReplication) (string, error) {
	// Rules are a "JSON document", what that means is they should be an array but they do not use array delimiters.
	// {"source_scope.source_collection":"target_scope.target_collection", "anothersource":"anothertarget"}
	rules := make(map[string]*string)

	for index, allowRule := range replication.ExplicitMapping.AllowRules {
		source := generateKeyspace(allowRule.SourceKeyspace)
		target := generateKeyspace(allowRule.TargetKeyspace)

		if _, exists := rules[source]; exists {
			return "", fmt.Errorf("%w: duplicate source keyspace %q for explicitMapping.allowRules[%d]", ErrXDCRReplicationInvalidMappingRule, source, index)
		}

		rules[source] = &target
	}

	// Deny rules have a null target for the map key hence the use of the pointer above
	// {"inventory.airport":null}
	for index, denyRule := range replication.ExplicitMapping.DenyRules {
		source := generateKeyspace(denyRule.SourceKeyspace)

		if _, exists := rules[source]; exists {
			return "", fmt.Errorf("%w: duplicate source keyspace %q for explicitMapping.denyRules[%d]", ErrXDCRReplicationInvalidMappingRule, source, index)
		}

		rules[source] = nil
	}

	bytes, err := json.Marshal(rules)
	if err != nil {
		return "", fmt.Errorf("%w: unable to marshal JSON for explicit mapping rules", err)
	}

	return string(bytes), nil
}

// getXDCRHostnameAndNetwork translates the common connection string format we advertise
// and translate it into something XDCR understands.
func getXDCRHostnameAndNetwork(cluster couchbasev2.RemoteCluster) (string, string, error) {
	// We act as a translation layer here, treating XDCR as just another client
	connectionString, err := url.Parse(cluster.Hostname)
	if err != nil {
		return "", "", err
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
			hostname += ":" + strconv.Itoa(k8sutil.AdminServicePortTLS)
		}
	}

	network := connectionString.Query().Get("network")

	return hostname, network, nil
}

func (c *Cluster) isScopesAndCollectionsSupported() (bool, error) {
	// Minimum supported version for scopes and collections is 7
	return c.IsAtLeastVersion("7.0.0")
}

func generateConflictLoggingSettings(conflictLogging *couchbasev2.CouchbaseConflictLoggingSpec) *couchbaseutil.ConflictLoggingSettings {
	if conflictLogging == nil {
		return &couchbaseutil.ConflictLoggingSettings{}
	}

	disabled := !conflictLogging.Enabled

	settings := &couchbaseutil.ConflictLoggingSettings{
		Disabled:   &disabled,
		Bucket:     string(conflictLogging.LogCollection.Bucket),
		Collection: fmt.Sprintf("%s.%s", conflictLogging.LogCollection.Scope, conflictLogging.LogCollection.Collection),
	}

	getRuleKey := func(scope, collection couchbasev2.ScopeOrCollectionNameIncludingDefault) string {
		if collection != "" {
			return fmt.Sprintf("%s.%s", scope, collection)
		}

		return string(scope)
	}

	rules := make(map[string]*couchbaseutil.ConflictLoggingLocation)

	for _, r := range conflictLogging.LoggingRules.NoLoggingRules {
		rules[getRuleKey(r.Scope, r.Collection)] = nil
	}

	for _, r := range conflictLogging.LoggingRules.DefaultCollectionRules {
		rules[getRuleKey(r.Scope, r.Collection)] = &couchbaseutil.ConflictLoggingLocation{}
	}

	for _, r := range conflictLogging.LoggingRules.CustomCollectionRules {
		rules[getRuleKey(r.Scope, r.Collection)] = &couchbaseutil.ConflictLoggingLocation{
			Bucket:     string(r.LogCollection.Bucket),
			Collection: fmt.Sprintf("%s.%s", r.LogCollection.Scope, r.LogCollection.Collection),
		}
	}

	// Note this is important, we only set the LoggingRules if there are any rules to set.
	// This is because the DeepEqual check will fail if the Requested LoggingRules is an empty map
	// and the API returns nil for the LoggingRules field if there are no rules to set.
	if len(rules) > 0 {
		settings.LoggingRules = rules
	}

	return settings
}

// generateXDCR combines API and secret data to construct remote clusters.
// Note: Replication generation is now handled by BuildDesiredReplicationStates().
func (c *Cluster) generateXDCR() ([]couchbaseutil.RemoteCluster, error) {
	var clusters []couchbaseutil.RemoteCluster

	for _, remoteCluster := range c.cluster.Spec.XDCR.RemoteClusters {
		hostname, network, err := getXDCRHostnameAndNetwork(remoteCluster)
		if err != nil {
			return nil, err
		}

		// renaming c.cluster.spec.xdcr.remoteClusters.name
		// any remoteCluster created/added via Operator must have this unique suffix
		remoteCluster.Name += RemoteClusterOperatorManagedSuffix

		// If the UUID is not provided then we should look it up, if it's not
		// there then it will be created and we can pick it up on the next
		// reconciliation.
		if remoteCluster.UUID == "" {
			if cluster, err := c.getRemoteClusterByName(remoteCluster.Name); err == nil {
				remoteCluster.UUID = cluster.UUID
			}
		}

		requested := couchbaseutil.RemoteCluster{
			Name:       remoteCluster.Name,
			UUID:       remoteCluster.UUID,
			Hostname:   hostname,
			Network:    network,
			SecureType: couchbaseutil.RemoteClusterSecurityNone,
		}

		if remoteCluster.AuthenticationSecret != nil {
			secret, found := c.k8s.Secrets.Get(*remoteCluster.AuthenticationSecret)
			if !found {
				return nil, fmt.Errorf("%w: unable to get remote cluster authentication secret %s", errors.NewStackTracedError(errors.ErrResourceRequired), *remoteCluster.AuthenticationSecret)
			}

			requested.Username = string(secret.Data["username"])
			requested.Password = string(secret.Data["password"])
		}

		if remoteCluster.TLS != nil && remoteCluster.TLS.Secret != nil {
			secret, found := c.k8s.Secrets.Get(*remoteCluster.TLS.Secret)
			if !found {
				return nil, fmt.Errorf("%w: unable to get remote cluster TLS secret %s", errors.NewStackTracedError(errors.ErrResourceRequired), *remoteCluster.TLS.Secret)
			}

			if _, ok := secret.Data[couchbasev2.RemoteClusterTLSCA]; !ok {
				return nil, fmt.Errorf("%w: CA certificate is required for TLS encryption", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
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

		clusters = append(clusters, requested)
	}

	return clusters, nil
}

// getPersistentXDCRData grabs a persistent data string.
func (c *Cluster) getPersistentXDCRData(cluster *couchbaseutil.RemoteCluster, key persistence.PersistentKindXDCR, value *string) error {
	v, err := c.state.Get(persistence.GetPersistentKindXDCR(cluster.Name, key))
	if err != nil {
		return err
	}

	*value = v

	return nil
}

// getOptionalPersistentXDCRData grabs a persistent data string, but doesn't error if it doesn't exist.
func (c *Cluster) getOptionalPersistentXDCRData(cluster *couchbaseutil.RemoteCluster, key persistence.PersistentKindXDCR, value *string) error {
	if err := c.getPersistentXDCRData(cluster, key, value); err != nil {
		if !goerrors.Is(err, persistence.ErrKeyError) {
			return err
		}
	}

	return nil
}

// setPersistentXDCRData sets a persistent data string.
func (c *Cluster) setPersistentXDCRData(cluster *couchbaseutil.RemoteCluster, key persistence.PersistentKindXDCR, value string) error {
	return c.state.Insert(persistence.GetPersistentKindXDCR(cluster.Name, key), value)
}

// setOptionalPersistentXDCRData sets a persistent data string, but only if there's something to store.
func (c *Cluster) setOptionalPersistentXDCRData(cluster *couchbaseutil.RemoteCluster, key persistence.PersistentKindXDCR, value string) error {
	if value == "" {
		return nil
	}

	return c.setPersistentXDCRData(cluster, key, value)
}

// listRemoteClusters does what it says fom Couchbase.  The XDCR API doesn't even attempt to
// support read/modify/write, and in some cases it's acceptable, such as not giving out
// credentials.  We, however, do need RMW, so we need to get what the API provides and then
// fill in the blanks with persistent data.
func (c *Cluster) listRemoteClusters() (couchbaseutil.RemoteClusters, error) {
	var remoteClusters couchbaseutil.RemoteClusters

	if err := couchbaseutil.ListRemoteClusters(&remoteClusters).On(c.api, c.readyMembers()); err != nil {
		return nil, err
	}

	// check for the unique suffix "-operator-managed" to discard the rest
	// this is necessary to rule out all remoteClusters for this cluster
	// which were not created/added via operator
	if len(remoteClusters) > 0 {
		for i, remoteCluster := range remoteClusters {
			// probably added to this cluster as remoteCluster via UI/API (not managed by operator)
			if !strings.HasSuffix(remoteCluster.Name, RemoteClusterOperatorManagedSuffix) {
				// delete those remote clusters via API
				if err := couchbaseutil.DeleteRemoteCluster(&remoteCluster).On(c.api, c.readyMembers()); err != nil {
					return nil, err
				}
				// remove them from the list
				remoteClusters = append(remoteClusters[:i], remoteClusters[i+1:]...)
			}
		}
	}

	for i := range remoteClusters {
		cluster := &remoteClusters[i]

		// Load up the configuration that changes... OMG!!
		if err := c.getOptionalPersistentXDCRData(cluster, persistence.XDCRHostname, &cluster.Hostname); err != nil {
			return nil, err
		}

		// Load up configuration that is written to the API but not returned.
		if err := c.getOptionalPersistentXDCRData(cluster, persistence.XDCRPassword, &cluster.Password); err != nil {
			return nil, err
		}

		if err := c.getOptionalPersistentXDCRData(cluster, persistence.XDCRClientKey, &cluster.Key); err != nil {
			return nil, err
		}

		if err := c.getOptionalPersistentXDCRData(cluster, persistence.XDCRClientCertificate, &cluster.Certificate); err != nil {
			return nil, err
		}
	}

	return remoteClusters, nil
}

// updateXDCRPersistentState flushes any existing XDCR persistent data out, so if the
// user wanted to swtich from basic auth to mTLS we won't load up the wrong stuff.
// Then we conditionally add any stuff that is set and isn't returned by an API read.
func (c *Cluster) updateXDCRPersistentState(cluster *couchbaseutil.RemoteCluster) error {
	if err := c.state.DeleteXDCR(cluster.Name); err != nil {
		return err
	}

	if err := c.setPersistentXDCRData(cluster, persistence.XDCRHostname, cluster.Hostname); err != nil {
		return err
	}

	if err := c.setOptionalPersistentXDCRData(cluster, persistence.XDCRPassword, cluster.Password); err != nil {
		return err
	}

	if err := c.setOptionalPersistentXDCRData(cluster, persistence.XDCRClientKey, cluster.Key); err != nil {
		return err
	}

	return c.setOptionalPersistentXDCRData(cluster, persistence.XDCRClientCertificate, cluster.Certificate)
}

// remoteClusterCreations is a generator that returns clusters that need to be created.
func remoteClusterCreations(current, requested couchbaseutil.RemoteClusters) couchbaseutil.RemoteClusters {
	var clusters couchbaseutil.RemoteClusters

Next:
	for _, req := range requested {
		for _, cur := range current {
			if cur.Name == req.Name {
				continue Next
			}
		}

		clusters = append(clusters, req)
	}

	return clusters
}

// remoteClusterUpdates is a generator that returns clusters that need to be updated.
func (c *Cluster) remoteClusterUpdates(current, requested couchbaseutil.RemoteClusters) couchbaseutil.RemoteClusters {
	var clusters couchbaseutil.RemoteClusters

Next:
	for _, req := range requested {
		for _, cur := range current {
			if req.Name != cur.Name {
				continue
			}

			// XDCR doesn't return the network mode, and that's a bug on their side
			// so I'm not persisting it and worrying about it.  Just perform a hack
			// here.
			req.Network = cur.Network

			if !reflect.DeepEqual(req, cur) {
				log.V(2).Info("XDCR connection state", "cluster", c.namespacedName(), "requested", req, "current", cur)

				clusters = append(clusters, req)
			}

			continue Next
		}
	}

	return clusters
}

// remoteClusterDeletions is a generator that returns clusters that need deleting.
func remoteClusterDeletions(current, requested couchbaseutil.RemoteClusters) couchbaseutil.RemoteClusters {
	var clusters couchbaseutil.RemoteClusters

Next:
	for _, cur := range current {
		for _, req := range requested {
			if req.Name == cur.Name {
				continue Next
			}
		}

		clusters = append(clusters, cur)
	}

	return clusters
}

// updateCreateDeleteXDCRReplications handles the creation, update and removal of replications.
// This must be called after new remotes are added, and before old remotes are removed.
// Note: requestedReplications are now generated internally via BuildDesiredReplicationStates().
func (c *Cluster) updateCreateDeleteXDCRReplications(currentReplications couchbaseutil.ReplicationList) error {
	// Build desired state from CRDs
	desiredStates, err := c.BuildDesiredReplicationStates()
	if err != nil {
		return err
	}

	// Fetch current state from server (including settings)
	currentStates, err := c.FetchCurrentReplicationStates(currentReplications)
	if err != nil {
		return err
	}

	// Diff and reconcile - handle all operations (create/update/delete)
	toCreate, toUpdate, toDelete := c.diffReplicationStates(desiredStates, currentStates)

	// Handle deletions first (replications must be deleted before their remote clusters)
	for _, current := range toDelete {
		log.Info("Deleting XDCR replication", "cluster", c.namespacedName(), "replication", current.Key)

		cluster, err := c.getRemoteClusterByName(current.Create.ToCluster)
		if err != nil {
			return err
		}

		if err := couchbaseutil.DeleteReplication(cluster.UUID, current.Create.FromBucket, current.Create.ToBucket).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		c.raiseEvent(k8sutil.ReplicationRemovedEvent(c.cluster, current.Key))
	}

	// Handle updates (settings changes only)
	for _, desired := range toUpdate {
		log.Info("Updating XDCR replication settings", "cluster", c.namespacedName(), "replication", desired.Key)

		current := currentStates[desired.Key]

		// Compute minimal patch
		patch := c.computeSettingsPatch(&desired.Settings, &current.Settings)

		// If patch is empty, skip
		if c.isEmptySettings(patch) {
			continue
		}

		// Apply settings patch
		if err := couchbaseutil.UpdateReplicationSettings(&patch, current.RemoteUUID, current.Create.FromBucket, current.Create.ToBucket).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		c.raiseEvent(k8sutil.ClusterSettingsEditedEvent("xdcr replication settings", c.cluster))
	}

	// Handle creations
	for _, desired := range toCreate {
		log.Info("Creating XDCR replication", "cluster", c.namespacedName(), "replication", desired.Key)

		// Create via creation API
		if err := couchbaseutil.CreateReplication(&desired.Create).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		c.raiseEvent(k8sutil.ReplicationAddedEvent(c.cluster, desired.Key))

		// Apply settings immediately after creation
		cluster, err := c.getRemoteClusterByName(desired.RemoteCluster)
		if err != nil {
			return err
		}

		// Fetch current settings (should be defaults/globals)
		currentSettings := &couchbaseutil.ReplicationSettings{}
		if err := couchbaseutil.GetReplicationSettings(currentSettings, cluster.UUID, desired.Create.FromBucket, desired.Create.ToBucket).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		// Compute patch against current settings
		patch := c.computeSettingsPatch(&desired.Settings, currentSettings)

		// Apply settings if there are changes
		if !c.isEmptySettings(patch) {
			if err := couchbaseutil.UpdateReplicationSettings(&patch, cluster.UUID, desired.Create.FromBucket, desired.Create.ToBucket).On(c.api, c.readyMembers()); err != nil {
				return err
			}

			c.raiseEvent(k8sutil.ClusterSettingsEditedEvent("xdcr replication settings", c.cluster))
		}
	}

	return nil
}

// DesiredReplicationState represents the complete desired state of a replication from CRD.
type DesiredReplicationState struct {
	// Creation fields (sent to /controller/createReplication)
	Create couchbaseutil.Replication

	// Settings fields (sent to /settings/replications/<id>)
	Settings couchbaseutil.ReplicationSettings

	// Metadata
	CRDName       string
	RemoteCluster string
	Key           string // replicationKey for efficient lookup
}

// CurrentReplicationState represents the current state of a replication on the server.
type CurrentReplicationState struct {
	// Creation state (from ListReplications)
	Create couchbaseutil.Replication

	// Settings state (from GET /settings/replications/<id>)
	Settings couchbaseutil.ReplicationSettings

	// Metadata
	Key        string
	RemoteUUID string
}

// BuildDesiredReplicationStates builds comprehensive replication states from all CRDs.
func (c *Cluster) BuildDesiredReplicationStates() (map[string]DesiredReplicationState, error) {
	states := make(map[string]DesiredReplicationState)

	// Check if scopes and collections are supported for migration replications
	xdcrScopesAndCollectionsSupported, err := c.isScopesAndCollectionsSupported()
	if err != nil {
		return nil, err
	}

	for _, remoteCluster := range c.cluster.Spec.XDCR.RemoteClusters {
		// Convert the label selector.
		selector := labels.Everything()

		if remoteCluster.Replications.Selector != nil {
			var err error
			if selector, err = metav1.LabelSelectorAsSelector(remoteCluster.Replications.Selector); err != nil {
				return nil, err
			}
		}

		// Compute the operator-managed remote cluster name deterministically.
		generatedName := remoteCluster.Name + RemoteClusterOperatorManagedSuffix

		// Process migration replications first (if supported)
		if xdcrScopesAndCollectionsSupported {
			if err := c.processMigrationReplications(selector, generatedName, states); err != nil {
				return nil, err
			}
		}

		// Process regular replications
		if err := c.processRegularReplications(selector, generatedName, xdcrScopesAndCollectionsSupported, states); err != nil {
			return nil, err
		}
	}

	return states, nil
}

// processMigrationReplications processes CouchbaseMigrationReplication resources.
func (c *Cluster) processMigrationReplications(selector labels.Selector, generatedName string, states map[string]DesiredReplicationState) error {
	apiMigrations := c.k8s.CouchbaseMigrationReplications.List()

	for _, migration := range apiMigrations {
		if !selector.Matches(labels.Set(migration.Labels)) {
			continue
		}

		// Populate spec from annotations (allows annotation-based overrides)
		// Errors are logged but don't stop processing
		if err := annotations.Populate(&migration.Spec, migration.Annotations); err != nil {
			log.Error(err, "failed to populate migration with annotation")
		}

		// Build creation struct for migration
		create := c.buildReplicationCreatePayload(&migration.Spec, generatedName)

		// Enable migration mode - this tells the server to migrate documents from the
		// default collection to collections determined by the mapping rules
		migrationTrue := true
		create.MigrationMapping = &migrationTrue

		// Generate migration mapping rules from CouchbaseMigrationReplication.MigrationMapping.Mappings
		// These rules specify which documents (via filter expressions) go to which target collections
		rules, err := generateMigrationMappingRules(migration)
		if err != nil {
			return fmt.Errorf("%w: invalid migration replication for %s", errors.NewStackTracedError(err), replicationKey(create))
		}
		create.MappingRules = convertColMappingRulesFromJSON(rules)

		// Build settings struct and propagate migration mode
		settings := c.buildSettingsFromSpec(&migration.Spec)
		settings.CollectionsMigrationMode = create.MigrationMapping

		key := replicationKey(create)
		if _, exists := states[key]; exists {
			return fmt.Errorf("%w: duplicate migration replication for %s", errors.NewStackTracedError(ErrXDCRDuplicateReplication), key)
		}
		states[key] = DesiredReplicationState{
			Create:        create,
			Settings:      settings,
			CRDName:       migration.Name,
			RemoteCluster: generatedName,
			Key:           key,
		}
	}

	return nil
}

// processRegularReplications processes CouchbaseReplication resources.
func (c *Cluster) processRegularReplications(selector labels.Selector, generatedName string, xdcrScopesAndCollectionsSupported bool, states map[string]DesiredReplicationState) error {
	apiReplications := c.k8s.CouchbaseReplications.List()

	for _, replication := range apiReplications {
		if !selector.Matches(labels.Set(replication.Labels)) {
			continue
		}

		// Populate spec from annotations (allows annotation-based overrides)
		// Errors are logged but don't stop processing
		if err := annotations.Populate(&replication.Spec, replication.Annotations); err != nil {
			log.Error(err, "failed to populate replication with annotation")
		}

		// Build creation struct (only creation-supported fields)
		create := c.buildReplicationCreatePayload(&replication.Spec, generatedName)

		// Build settings struct (all per-replication settings)
		settings := c.buildSettingsFromSpec(&replication.Spec)

		// Handle explicit mapping rules from CouchbaseReplication.ExplicitMapping
		// Explicit mapping allows replicating specific collections to specific target collections,
		// with allow rules (replicate A->B) and deny rules (don't replicate C)
		if xdcrScopesAndCollectionsSupported && (len(replication.ExplicitMapping.AllowRules) > 0 || len(replication.ExplicitMapping.DenyRules) > 0) {
			rules, err := generateExplicitMappingRules(replication)
			if err != nil {
				return fmt.Errorf("%w: invalid replication for %s", errors.NewStackTracedError(err), replicationKey(create))
			}

			if rules != "{}" {
				// Enable explicit mapping mode and set the mapping rules
				explicitTrue := true
				create.ExplicitMapping = &explicitTrue
				create.MappingRules = convertColMappingRulesFromJSON(rules)
				settings.CollectionsExplicitMapping = &explicitTrue
				settings.ColMappingRules = create.MappingRules
			}
		}

		key := replicationKey(create)
		if _, exists := states[key]; exists {
			return fmt.Errorf("%w: duplicate replication for %s", errors.NewStackTracedError(ErrXDCRDuplicateReplication), key)
		}
		states[key] = DesiredReplicationState{
			Create:        create,
			Settings:      settings,
			CRDName:       replication.Name,
			RemoteCluster: generatedName,
			Key:           key,
		}
	}

	return nil
}

// buildCreateFromSpec builds the creation API struct from a CRD spec (only creation-supported fields).
func (c *Cluster) buildReplicationCreatePayload(spec *couchbasev2.CouchbaseReplicationSpec, remoteClusterName string) couchbaseutil.Replication {
	replication := couchbaseutil.Replication{
		// Core immutable fields
		FromBucket:         string(spec.Bucket),
		ToCluster:          remoteClusterName,
		ToBucket:           string(spec.RemoteBucket),
		Type:               couchbaseutil.ReplicationTypeXMEM,
		ReplicationType:    couchbaseutil.ReplicationReplicationTypeContinuous,
		FilterSkipRestream: spec.FilterSkipRestream,

		// Legacy core fields (can be set during creation)
		PauseRequested: spec.Paused,

		// Advanced settings supported during replication creation (from createReplication API docs)
		CompressionType:                spec.CompressionType,
		DesiredLatency:                 spec.DesiredLatency,
		FilterExpression:               spec.FilterExpression,
		FilterDeletion:                 spec.FilterDeletion,
		FilterExpiration:               spec.FilterExpiration,
		FilterBypassExpiry:             spec.FilterBypassExpiry,
		FilterBinary:                   spec.FilterBinary,
		Priority:                       spec.Priority,
		OptimisticReplicationThreshold: spec.OptimisticReplicationThreshold,
		FailureRestartInterval:         spec.FailureRestartInterval,
		DocBatchSizeKb:                 spec.DocBatchSizeKb,
		WorkerBatchSize:                spec.WorkerBatchSize,
		CheckpointInterval:             spec.CheckpointInterval,
		SourceNozzlePerNode:            spec.SourceNozzlePerNode,
		TargetNozzlePerNode:            spec.TargetNozzlePerNode,
		StatsInterval:                  spec.StatsInterval,
		LogLevel:                       spec.LogLevel,
		NetworkUsageLimit:              spec.NetworkUsageLimit,
	}

	// Add version-specific fields
	if isAtleast76, err := c.cluster.IsAtLeastVersion("7.6.0"); err == nil && isAtleast76 {
		replication.Mobile = spec.Mobile
	}

	if c.SupportsVersionFeatures("8.0.0") {
		replication.ConflictLogging = generateConflictLoggingSettings(spec.ConflictLogging)
	}

	return replication
}

// buildSettingsFromSpec builds the settings API struct from a CRD spec (all per-replication settings).
func (c *Cluster) buildSettingsFromSpec(spec *couchbasev2.CouchbaseReplicationSpec) couchbaseutil.ReplicationSettings {
	var dest couchbaseutil.ReplicationSettings

	// Legacy core fields (map from CRD spec to ReplicationSettings)
	dest.PauseRequested = spec.Paused

	// Global / Per-replication advanced settings (from CRD spec)
	dest.CompressionType = spec.CompressionType
	dest.DesiredLatency = spec.DesiredLatency
	dest.FilterDeletion = spec.FilterDeletion
	dest.FilterExpiration = spec.FilterExpiration
	dest.FilterBypassExpiry = spec.FilterBypassExpiry
	dest.FilterBinary = spec.FilterBinary
	// FilterSkipRestream is create-only (immutable), not included in settings updates
	dest.Priority = spec.Priority
	dest.OptimisticReplicationThreshold = spec.OptimisticReplicationThreshold
	dest.FailureRestartInterval = spec.FailureRestartInterval
	dest.DocBatchSizeKb = spec.DocBatchSizeKb
	dest.WorkerBatchSize = spec.WorkerBatchSize
	dest.CheckpointInterval = spec.CheckpointInterval
	dest.SourceNozzlePerNode = spec.SourceNozzlePerNode
	dest.TargetNozzlePerNode = spec.TargetNozzlePerNode
	dest.StatsInterval = spec.StatsInterval
	dest.LogLevel = spec.LogLevel
	dest.NetworkUsageLimit = spec.NetworkUsageLimit
	dest.Mobile = spec.Mobile

	// Per-replication only settings (from CRD spec)
	dest.FilterExpression = spec.FilterExpression
	dest.MergeFunctionMapping = convertMergeFunctionMappingRules(&spec.MergeFunctionMapping)

	// Additional settings only available in ReplicationSettings API
	dest.CollectionsOSOMode = spec.CollectionsOSOMode

	// HlvPruningWindowSec is not supported in Couchbase Server 8.0+
	// Set to nil if version >= 7.6.0 to avoid issues
	if isAtLeast76, err := c.cluster.IsAtLeastVersion("7.6.0"); err == nil && isAtLeast76 {
		dest.HlvPruningWindowSec = nil
	} else {
		dest.HlvPruningWindowSec = spec.HlvPruningWindowSec
	}

	dest.JSFunctionTimeoutMs = spec.JSFunctionTimeoutMs
	dest.RetryOnRemoteAuthErr = spec.RetryOnRemoteAuthErr
	dest.RetryOnRemoteAuthErrMaxWaitSec = spec.RetryOnRemoteAuthErrMaxWaitSec

	// Conflict logging (convert from CRD spec)
	if spec.ConflictLogging != nil {
		dest.ConflictLogging = generateConflictLoggingSettings(spec.ConflictLogging)
	}

	return dest
}

// FetchCurrentReplicationStates fetches current state from the server.
func (c *Cluster) FetchCurrentReplicationStates(currentReplications couchbaseutil.ReplicationList) (map[string]CurrentReplicationState, error) {
	states := make(map[string]CurrentReplicationState)

	for _, replication := range currentReplications {
		key := replicationKey(replication)

		// Get remote cluster UUID for this replication
		remoteCluster, err := c.getRemoteClusterByName(replication.ToCluster)
		if err != nil {
			return nil, err
		}

		// Fetch current settings from server
		currentSettings := &couchbaseutil.ReplicationSettings{}
		if err := couchbaseutil.GetReplicationSettings(currentSettings, remoteCluster.UUID, replication.FromBucket, replication.ToBucket).On(c.api, c.readyMembers()); err != nil {
			return nil, err
		}

		states[key] = CurrentReplicationState{
			Create:     replication,
			Settings:   *currentSettings,
			Key:        key,
			RemoteUUID: remoteCluster.UUID,
		}
	}

	return states, nil
}

// computeSettingsPatch computes a minimal patch containing only changed fields.
func (c *Cluster) computeSettingsPatch(desired, current *couchbaseutil.ReplicationSettings) couchbaseutil.ReplicationSettings {
	var patch couchbaseutil.ReplicationSettings

	// Use reflection to iterate over all fields and copy changed ones
	desiredVal := reflect.ValueOf(desired).Elem()
	currentVal := reflect.ValueOf(current).Elem()
	patchVal := reflect.ValueOf(&patch).Elem()

	for i := 0; i < desiredVal.NumField(); i++ {
		desiredField := desiredVal.Field(i)
		currentField := currentVal.Field(i)
		patchField := patchVal.Field(i)

		// Only process settable fields
		if !patchField.CanSet() {
			continue
		}

		// Check if the field has changed
		if !reflect.DeepEqual(desiredField.Interface(), currentField.Interface()) {
			patchField.Set(desiredField)
		}
	}

	return patch
}

// isEmptySettings checks if a settings struct has no non-nil fields.
func (c *Cluster) isEmptySettings(settings couchbaseutil.ReplicationSettings) bool {
	return reflect.DeepEqual(settings, couchbaseutil.ReplicationSettings{})
}

// diffReplicationStates compares desired vs current states and returns what needs to be done.
func (c *Cluster) diffReplicationStates(desired map[string]DesiredReplicationState, current map[string]CurrentReplicationState) (
	toCreate []DesiredReplicationState,
	toUpdate []DesiredReplicationState,
	toDelete []CurrentReplicationState,
) {
	// Find creates and updates
	for key, desiredState := range desired {
		if currentState, exists := current[key]; exists {
			// Exists - check if settings need updating
			// Note: Immutable field changes are prevented by validation layers
			if c.needsSettingsUpdate(desiredState.Settings, currentState.Settings) {
				toUpdate = append(toUpdate, desiredState)
			}
		} else {
			// Doesn't exist - needs creation
			toCreate = append(toCreate, desiredState)
		}
	}

	// Find deletions (replications that exist on server but not in desired state)
	for key, currentState := range current {
		if _, exists := desired[key]; !exists {
			toDelete = append(toDelete, currentState)
		}
	}

	return toCreate, toUpdate, toDelete
}

// needsSettingsUpdate determines if settings need updating.
func (c *Cluster) needsSettingsUpdate(desired, current couchbaseutil.ReplicationSettings) bool {
	// Compute patch and check if it's empty
	patch := c.computeSettingsPatch(&desired, &current)
	return !c.isEmptySettings(patch)
}

// reconcileXDCRGlobalSettings applies cluster-wide XDCR settings before handling replications.
func (c *Cluster) reconcileXDCRGlobalSettings() error {
	if !c.cluster.Spec.XDCR.Managed {
		return nil
	}

	spec := c.cluster.Spec.XDCR.GlobalSettings
	if spec == nil {
		return nil
	}

	desired := c.toXDCRGlobalSettings(spec)

	// Apply desired settings; urlencoding with omitempty will skip nil pointers
	if err := couchbaseutil.SetXDCRGlobalSettings(desired).On(c.api, c.readyMembers()); err != nil {
		return err
	}

	c.raiseEvent(k8sutil.ClusterSettingsEditedEvent("xdcr global settings", c.cluster))

	return nil
}

func (c *Cluster) toXDCRGlobalSettings(spec *couchbasev2.XDCRGlobalSettings) *couchbaseutil.XDCRGlobalSettings {
	if spec == nil {
		return nil
	}
	// Shallow pointer copy into util model; fields align by name
	out := &couchbaseutil.XDCRGlobalSettings{
		CheckpointInterval:             spec.CheckpointInterval,
		CollectionsOSOMode:             spec.CollectionsOSOMode,
		CompressionType:                spec.CompressionType,
		DesiredLatency:                 spec.DesiredLatency,
		DocBatchSizeKb:                 spec.DocBatchSizeKb,
		FailureRestartInterval:         spec.FailureRestartInterval,
		FilterBypassExpiry:             spec.FilterBypassExpiry,
		FilterBypassUncommittedTxn:     spec.FilterBypassUncommittedTxn,
		FilterDeletion:                 spec.FilterDeletion,
		FilterExpiration:               spec.FilterExpiration,
		JSFunctionTimeoutMs:            spec.JSFunctionTimeoutMs,
		LogLevel:                       spec.LogLevel,
		Mobile:                         spec.Mobile,
		NetworkUsageLimit:              spec.NetworkUsageLimit,
		OptimisticReplicationThreshold: spec.OptimisticReplicationThreshold,
		Priority:                       spec.Priority,
		RetryOnRemoteAuthErr:           spec.RetryOnRemoteAuthErr,
		RetryOnRemoteAuthErrMaxWaitSec: spec.RetryOnRemoteAuthErrMaxWaitSec,
		SourceNozzlePerNode:            spec.SourceNozzlePerNode,
		StatsInterval:                  spec.StatsInterval,
		TargetNozzlePerNode:            spec.TargetNozzlePerNode,
		WorkerBatchSize:                spec.WorkerBatchSize,
		GoGC:                           spec.GoGC,
		GoMaxProcs:                     spec.GoMaxProcs,
	}

	// Apply version-specific logic
	if isAtLeast76, err := c.cluster.IsAtLeastVersion("7.6.0"); err == nil && isAtLeast76 {
		out.HlvPruningWindowSec = nil
	} else {
		out.HlvPruningWindowSec = spec.HlvPruningWindowSec
	}

	return out
}

// checkXDCRTask checks the XDCR connections.
func (c *Cluster) checkXDCRTask(cluster *couchbaseutil.RemoteCluster, atLeast721 bool) error {
	if !atLeast721 || c.cluster.Spec.XDCR.DisablePrechecks {
		return nil
	}

	xdcrPreCheckResponse := couchbaseutil.XDCRConnectionPreCheckResponse{}

	if err := couchbaseutil.PreCheckXDCR(cluster, &xdcrPreCheckResponse).On(c.api, c.readyMembers()); err != nil {
		return err
	}

	err := retryutil.RetryFor(5*time.Minute, func() error {
		xdcrCheckResponse := couchbaseutil.XdcrConnectionCheckResponse{}

		if err := couchbaseutil.CheckXDCRCheckTask(xdcrPreCheckResponse, &xdcrCheckResponse).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		resultMessage := xdcrCheckResponse.Result

		for item := range resultMessage {
			resultMessageNode := resultMessage[item]
			for node := range resultMessageNode {
				resultString := resultMessageNode[node]
				if strings.Contains(resultString[0], "successful") || strings.HasPrefix(resultString[0], "Intra-cluster replication detected, skipping connection pre-check") {
					return nil
				}
			}
		}
		return ErrXDCRCheckFailed
	})

	return err
}

// reconcileXDCR creates and deletes XDCR connections dynamically.
func (c *Cluster) reconcileXDCR() error {
	if !c.cluster.Spec.XDCR.Managed {
		return nil
	}

	atLeast721, err := c.IsAtLeastVersion("7.2.1")
	if err != nil {
		return err
	}

	requestedClusters, err := c.generateXDCR()
	if err != nil {
		return err
	}

	// Note: requestedReplications are now generated via BuildDesiredReplicationStates() in updateCreateDeleteXDCRReplications()
	// This eliminates redundant CRD iteration and ensures consistency with the new architecture

	currentClusters, err := c.listRemoteClusters()
	if err != nil {
		return err
	}

	currentReplications, err := c.ListReplications()
	if err != nil {
		return err
	}

	// Delete stuff first to remove any non-managed remote-clusters that could conflict with managed ones
	// Note: We'll handle ALL replication operations (create/update/delete) after clusters are ready

	// Delete any orphaned clusters
	deletes := remoteClusterDeletions(currentClusters, requestedClusters)
	for i := range deletes {
		cluster := &deletes[i]

		log.Info("Deleting XDCR remote cluster", "cluster", c.namespacedName(), "remote", cluster.Name)

		if err := couchbaseutil.DeleteRemoteCluster(cluster).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		c.raiseEvent(k8sutil.RemoteClusterRemovedEvent(c.cluster, cluster.Name))

		if err := c.state.DeleteXDCR(cluster.Name); err != nil {
			return err
		}
	}

	// Create/update any new clusters...
	updates := c.remoteClusterUpdates(currentClusters, requestedClusters)
	for i := range updates {
		cluster := &updates[i]

		if err := c.checkXDCRTask(cluster, atLeast721); err != nil {
			return err
		}

		log.Info("Updating XDCR remote cluster", "cluster", c.namespacedName(), "remote", cluster.Name)

		if err := couchbaseutil.UpdateRemoteCluster(cluster).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		c.raiseEvent(k8sutil.RemoteClusterUpdatedEvent(c.cluster, cluster.Name))

		if err := c.updateXDCRPersistentState(cluster); err != nil {
			return err
		}
	}

	creates := remoteClusterCreations(currentClusters, requestedClusters)
	for i := range creates {
		cluster := &creates[i]

		if err := c.checkXDCRTask(cluster, atLeast721); err != nil {
			return err
		}

		log.Info("Creating XDCR remote cluster", "cluster", c.namespacedName(), "remote", cluster.Name)

		if err := couchbaseutil.CreateRemoteCluster(cluster).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		c.raiseEvent(k8sutil.RemoteClusterAddedEvent(c.cluster, cluster.Name))

		// Save any updatable parameters that will not be returned by a GET from the
		// API.  We will use these to detect and trigger updates.
		if err := c.updateXDCRPersistentState(cluster); err != nil {
			return err
		}
	}

	// Replications depend on remotes existing, so handle them AFTER clusters are ready
	return c.updateCreateDeleteXDCRReplications(currentReplications)
}

// convertColMappingRulesFromJSON converts a JSON string to ColMappingRules.
// This is used for migration replications where rules are generated as JSON strings.
func convertColMappingRulesFromJSON(jsonRules string) *couchbaseutil.ColMappingRules {
	if jsonRules == "" || jsonRules == "{}" {
		return nil
	}

	var rulesMap map[string]*string
	if err := json.Unmarshal([]byte(jsonRules), &rulesMap); err != nil {
		// If unmarshal fails, return nil (should not happen with valid generateMigrationMappingRules output)
		return nil
	}

	utilRules := make(couchbaseutil.ColMappingRules)
	for k, v := range rulesMap {
		// Directly assign the pointer, which can be nil
		utilRules[k] = v
	}

	return &utilRules
}

// convertMergeFunctionMappingRules converts API MergeFunctionMappingRules to util MergeFunctionMappingRules.
func convertMergeFunctionMappingRules(apiRules *couchbasev2.MergeFunctionMappingRules) *couchbaseutil.MergeFunctionMappingRules {
	if apiRules == nil {
		return nil
	}

	utilRules := make(couchbaseutil.MergeFunctionMappingRules)
	for k, v := range *apiRules {
		utilRules[k] = v
	}

	return &utilRules
}
