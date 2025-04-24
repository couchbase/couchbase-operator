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
	defaultMobileReplicationValue      string = "Off"
)

// replicationKey returns a unique identifier per replication.
func replicationKey(r couchbaseutil.Replication) string {
	return fmt.Sprintf("%s/%s/%s", r.ToCluster, r.FromBucket, r.ToBucket)
}

func (c *Cluster) listReplications() (couchbaseutil.ReplicationList, error) {
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

		// Lookup the settings to recover the compression type and any mappings - this must be done per-replication
		settings := &couchbaseutil.ReplicationSettings{}
		if err := couchbaseutil.GetReplicationSettings(settings, uuid, task.Source, to).On(c.api, c.readyMembers()); err != nil {
			return nil, err
		}

		// By now your eyeballs will be dry from all the rolling they are doing.
		newReplication := couchbaseutil.Replication{
			FromBucket:       task.Source,
			ToCluster:        cluster.Name,
			ToBucket:         to,
			Type:             task.ReplicationType,
			ReplicationType:  couchbaseutil.ReplicationReplicationTypeContinuous,
			CompressionType:  settings.CompressionType,
			FilterExpression: task.FilterExpression,
			PauseRequested:   settings.PauseRequested,
			Mobile:           settings.Mobile,
		}

		// Deal with any additional mappings for scopes and collections
		if mappingsPossible {
			newReplication.ExplicitMapping = settings.ExplicitMapping
			newReplication.MigrationMapping = settings.MigrationMapping

			newReplication.MappingRules, err = couchbaseutil.MappingRulesToStr(settings.MappingRules)
			if err != nil {
				return nil, err
			}
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

// generateXDCRReplications uses the remote's label selector to pick all the replications
// to create for this remote cluster connection.
//
//nolint:gocognit
func (c *Cluster) generateXDCRReplications(remoteCluster couchbasev2.RemoteCluster) ([]couchbaseutil.Replication, error) {
	var replications []couchbaseutil.Replication

	selector := labels.Everything()

	if remoteCluster.Replications.Selector != nil {
		var err error

		if selector, err = metav1.LabelSelectorAsSelector(remoteCluster.Replications.Selector); err != nil {
			return nil, err
		}
	}

	xdcrScopesAndCollectionsSupported, err := c.isScopesAndCollectionsSupported()
	if err != nil {
		return nil, err
	}

	duplicateChecks := make(map[string]bool)

	if xdcrScopesAndCollectionsSupported {
		apiMigrations := c.k8s.CouchbaseMigrationReplications.List()

		for _, migration := range apiMigrations {
			if !selector.Matches(labels.Set(migration.Labels)) {
				continue
			}

			err := annotations.Populate(&migration.Spec, migration.Annotations)
			if err != nil {
				// we failed but its not worth stopping. log the error and continue
				log.Error(err, "failed to populate migration with annotation")
			}

			newMigration := couchbaseutil.Replication{
				FromBucket:       string(migration.Spec.Bucket),
				ToCluster:        remoteCluster.Name,
				ToBucket:         string(migration.Spec.RemoteBucket),
				Type:             couchbaseutil.ReplicationTypeXMEM,
				ReplicationType:  couchbaseutil.ReplicationReplicationTypeContinuous,
				CompressionType:  string(migration.Spec.CompressionType),
				FilterExpression: migration.Spec.FilterExpression,
				PauseRequested:   migration.Spec.Paused,
				MigrationMapping: true,
			}

			if isAtleast76, err := c.cluster.IsAtLeastVersion("7.6.0"); err != nil {
				log.Error(err, "Failed to check server version for mobile replication")
				return nil, err
			} else if isAtleast76 {
				newMigration.Mobile = string(migration.Spec.Mobile)
				// Use default value if not set
				if newMigration.Mobile == "" {
					newMigration.Mobile = defaultMobileReplicationValue
				}
			}

			replicationKey := replicationKey(newMigration)

			_, exists := duplicateChecks[replicationKey]
			if exists {
				return nil, fmt.Errorf("%w: duplicate migration replication for %s", errors.NewStackTracedError(ErrXDCRDuplicateReplication), replicationKey)
			}

			rules, err := generateMigrationMappingRules(migration)
			if err != nil {
				return nil, fmt.Errorf("%w: invalid migration replication for %s", errors.NewStackTracedError(err), replicationKey)
			}

			newMigration.MappingRules = rules

			replications = append(replications, newMigration)
			duplicateChecks[replicationKey] = true
		}
	}

	apiReplications := c.k8s.CouchbaseReplications.List()
	for _, replication := range apiReplications {
		if !selector.Matches(labels.Set(replication.Labels)) {
			continue
		}

		err := annotations.Populate(&replication.Spec, replication.Annotations)
		if err != nil {
			// we failed but its not worth stopping. log the error and continue
			log.Error(err, "failed to populate replication with annotation")
		}

		newReplication := couchbaseutil.Replication{
			FromBucket:       string(replication.Spec.Bucket),
			ToCluster:        remoteCluster.Name,
			ToBucket:         string(replication.Spec.RemoteBucket),
			Type:             couchbaseutil.ReplicationTypeXMEM,
			ReplicationType:  couchbaseutil.ReplicationReplicationTypeContinuous,
			CompressionType:  string(replication.Spec.CompressionType),
			FilterExpression: replication.Spec.FilterExpression,
			PauseRequested:   replication.Spec.Paused,
		}

		if isAtleast76, err := c.cluster.IsAtLeastVersion("7.6.0"); err != nil {
			log.Error(err, "Failed to check server version for mobile replication")
			return nil, err
		} else if isAtleast76 {
			newReplication.Mobile = string(replication.Spec.Mobile)
			if newReplication.Mobile == "" {
				newReplication.Mobile = "Off"
			}
		}

		replicationKey := replicationKey(newReplication)

		_, exists := duplicateChecks[replicationKey]
		if exists {
			return nil, fmt.Errorf("%w: duplicate replication for %s", errors.NewStackTracedError(ErrXDCRDuplicateReplication), replicationKey)
		}

		if xdcrScopesAndCollectionsSupported {
			rules, err := generateExplicitMappingRules(replication)
			if err != nil {
				return nil, fmt.Errorf("%w: invalid replication for %s", errors.NewStackTracedError(err), replicationKey)
			}

			if rules != "{}" {
				newReplication.ExplicitMapping = true
				newReplication.MappingRules = rules
			} else {
				newReplication.ExplicitMapping = false
			}
		}

		replications = append(replications, newReplication)
		duplicateChecks[replicationKey] = true
	}

	return replications, nil
}

// generateXDCR combines API and secret data to construct an idealized form
// of XDCR primitives that need to exist.
func (c *Cluster) generateXDCR() ([]couchbaseutil.RemoteCluster, []couchbaseutil.Replication, error) {
	var clusters []couchbaseutil.RemoteCluster

	var replications []couchbaseutil.Replication

	for _, remoteCluster := range c.cluster.Spec.XDCR.RemoteClusters {
		hostname, network, err := getXDCRHostnameAndNetwork(remoteCluster)
		if err != nil {
			return nil, nil, err
		}

		// renaming c.cluster.spec.xdcr.remoteClusters.name
		// any remoteCluster created/added via Operator must have this unique suffix
		remoteCluster.Name += RemoteClusterOperatorManagedSuffix

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
				return nil, nil, fmt.Errorf("%w: unable to get remote cluster authentication secret %s", errors.NewStackTracedError(errors.ErrResourceRequired), *remoteCluster.AuthenticationSecret)
			}

			requested.Username = string(secret.Data["username"])
			requested.Password = string(secret.Data["password"])
		}

		if remoteCluster.TLS != nil && remoteCluster.TLS.Secret != nil {
			secret, found := c.k8s.Secrets.Get(*remoteCluster.TLS.Secret)
			if !found {
				return nil, nil, fmt.Errorf("%w: unable to get remote cluster TLS secret %s", errors.NewStackTracedError(errors.ErrResourceRequired), *remoteCluster.TLS.Secret)
			}

			if _, ok := secret.Data[couchbasev2.RemoteClusterTLSCA]; !ok {
				return nil, nil, fmt.Errorf("%w: CA certificate is required for TLS encryption", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
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

		clusterReplications, err := c.generateXDCRReplications(remoteCluster)
		if err != nil {
			return nil, nil, err
		}

		replications = append(replications, clusterReplications...)
	}

	return clusters, replications, nil
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

// replicationCreations is a generator that returns replications that need creating.
func replicationCreations(current, requested couchbaseutil.ReplicationList) couchbaseutil.ReplicationList {
	var replications couchbaseutil.ReplicationList

Next:
	for _, req := range requested {
		for _, cur := range current {
			if replicationKey(req) == replicationKey(cur) {
				continue Next
			}
		}

		replications = append(replications, req)
	}

	return replications
}

// replicationUpdates is a generator that returns replications that need updating.
func replicationUpdates(current, requested couchbaseutil.ReplicationList) couchbaseutil.ReplicationList {
	var replications couchbaseutil.ReplicationList

Next:
	for _, req := range requested {
		for _, cur := range current {
			if replicationKey(req) != replicationKey(cur) {
				continue
			}

			if !reflect.DeepEqual(req, cur) {
				replications = append(replications, req)
			}

			continue Next
		}
	}

	return replications
}

// replicationDeletions is a generator that returns replications that need deleting.
func replicationDeletions(current, requested couchbaseutil.ReplicationList) couchbaseutil.ReplicationList {
	var replications couchbaseutil.ReplicationList

Next:
	for _, cur := range current {
		for _, req := range requested {
			if replicationKey(cur) == replicationKey(req) {
				continue Next
			}
		}

		replications = append(replications, cur)
	}

	return replications
}

func (c *Cluster) deleteXDCRReplications(requestedReplications, currentReplications couchbaseutil.ReplicationList) error {
	// Delete any orphaned replications...
	replicationDeletes := replicationDeletions(currentReplications, requestedReplications)
	for i := range replicationDeletes {
		replication := replicationDeletes[i]

		log.Info("Deleting XDCR replication", "cluster", c.namespacedName(), "replication", replicationKey(replication))

		cluster, err := c.getRemoteClusterByName(replication.ToCluster)
		if err != nil {
			return err
		}

		if err := couchbaseutil.DeleteReplication(cluster.UUID, replication.FromBucket, replication.ToBucket).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		c.raiseEvent(k8sutil.ReplicationRemovedEvent(c.cluster, replicationKey(replication)))
	}

	return nil
}

// updateCreateXDCRReplications handles the creation, update and removal of replications.
// This must be called after new remotes are added, and before old remotes are removed.
func (c *Cluster) updateCreateXDCRReplications(requestedReplications, currentReplications couchbaseutil.ReplicationList) error {
	// We deal with migrations first, the assumption being that these would be one-offs and they should be done
	// first.
	// Create/update any replications...
	replicationUpdates := replicationUpdates(currentReplications, requestedReplications)
	for i := range replicationUpdates {
		replication := replicationUpdates[i]

		log.Info("Updating XDCR replication", "cluster", c.namespacedName(), "replication", replicationKey(replication))

		cluster, err := c.getRemoteClusterByName(replication.ToCluster)
		if err != nil {
			return err
		}

		if err := couchbaseutil.UpdateReplication(&replication, cluster.UUID, replication.FromBucket, replication.ToBucket).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		c.raiseEvent(k8sutil.ClusterSettingsEditedEvent("xdcr replication", c.cluster))
	}

	replicationCreates := replicationCreations(currentReplications, requestedReplications)
	for i := range replicationCreates {
		replication := replicationCreates[i]

		log.Info("Creating XDCR replication", "cluster", c.namespacedName(), "replication", replicationKey(replication))

		if err := couchbaseutil.CreateReplication(&replication).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		c.raiseEvent(k8sutil.ReplicationAddedEvent(c.cluster, replicationKey(replication)))
	}

	return nil
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
				if strings.Contains(resultString[0], "successful") {
					return nil
				}
			}
		}
		return ErrXDCRCheckFailed
	})

	return err
}

func (c *Cluster) GatherReplicationChanges() (map[couchbaseutil.Replication]couchbaseutil.Replication, error) {
	var updates = make(map[couchbaseutil.Replication]couchbaseutil.Replication)

	current, err := c.listReplications()
	if err != nil {
		return nil, err
	}

	_, requested, err := c.generateXDCR()
	if err != nil {
		return nil, err
	}

	for _, c := range current {
		for _, r := range requested {
			if replicationKey(c) == replicationKey(r) {
				if !reflect.DeepEqual(r, c) {
					updates[c] = r
				}
			}
		}
	}

	return updates, nil
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

	requestedClusters, requestedReplications, err := c.generateXDCR()
	if err != nil {
		return err
	}

	currentClusters, err := c.listRemoteClusters()
	if err != nil {
		return err
	}

	currentReplications, err := c.listReplications()
	if err != nil {
		return err
	}

	// Delete stuff first to remove any non-managed remote-clusters that could conflict with managed ones
	// Deleting replications first because replications need to be deleted before remote clusters
	if err = c.deleteXDCRReplications(requestedReplications, currentReplications); err != nil {
		return err
	}

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

	// Replications depend on remotes existing, and also need to be removed before
	// the remote they depend on, so perform it here.
	return c.updateCreateXDCRReplications(requestedReplications, currentReplications)
}
