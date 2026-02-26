package cluster

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/cluster/persistence"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/metrics"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/diff"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/scheduler"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var DefaultServicesOrder = []string{"data", "query", "index", "search", "analytics", "eventing", "arbiter"}

func (c *Cluster) needsMove() couchbaseutil.MemberSet {
	candidates := couchbaseutil.MemberSet{}

	for name, member := range c.members {
		actual, exists := c.k8s.Pods.Get(name)
		if !exists {
			continue
		}

		shouldMove, ok := actual.Annotations[constants.AnnotationReschedule]
		if !ok {
			continue
		} else if shouldMove == "true" {
			candidates.Add(member)
		}
	}

	return candidates
}

// filterCandidatesByUpgradeOrder returns the upgrade candidates ordered by the upgrade order specified in the cluster spec.
// This method does not apply any limits to the number of candidates that can be upgraded at once.
func (c *Cluster) filterCandidatesByUpgradeOrder(candidates couchbaseutil.MemberSet) (couchbaseutil.MemberList, error) {
	if c.cluster.Spec.Upgrade == nil {
		return candidates.ToList(), nil
	}

	upgradeOrderType := c.cluster.Spec.Upgrade.UpgradeOrderType

	switch upgradeOrderType {
	case couchbasev2.UpgradeOrderTypeNodes:
		return c.selectCandidatesByNodesOrder(candidates), nil
	case couchbasev2.UpgradeOrderTypeServerGroups:
		return c.selectCandidatesByServerGroupsOrder(candidates)
	case couchbasev2.UpgradeOrderTypeServerClasses:
		return c.selectCandidatesByServerClassesOrder(candidates), nil
	case couchbasev2.UpgradeOrderTypeServices:
		return c.selectCandidatesByServicesOrder(candidates), nil
	}

	return candidates.ToList(), nil
}

func (c *Cluster) selectCandidatesByNodesOrder(candidates couchbaseutil.MemberSet) couchbaseutil.MemberList {
	nodeOrder := c.cluster.Spec.Upgrade.UpgradeOrder
	finalCandidates := couchbaseutil.MemberList{}

	for _, node := range nodeOrder {
		if candidates.Contains(node) {
			finalCandidates = append(finalCandidates, candidates[node])
		}
	}

	// If the orchestrator is not specified in the upgrade order, but it is in the candidates list, move it to the end of the list to upgrade last.
	var orchestrator couchbaseutil.Member

	clusterInfo := &couchbaseutil.TerseClusterInfo{}
	if err := couchbaseutil.GetTerseClusterInfo(clusterInfo).On(c.api, c.readyMembers()); err != nil {
		log.Error(err, "failed to get cluster info", "cluster", c.namespacedName())
	} else {
		orchestratorName := clusterInfo.Orchestrator
		if !finalCandidates.Contains(orchestratorName) {
			candidates, orchestrator = separateCandidatesAndOrchestrator(candidates, orchestratorName)
		}
	}

	// Go through the rest in alphabetical order
	sortedNodes := candidates.Names()
	sort.Strings(sortedNodes)

	for _, node := range sortedNodes {
		if !finalCandidates.Contains(node) {
			finalCandidates = append(finalCandidates, candidates[node])
		}
	}

	if orchestrator != nil {
		finalCandidates = append(finalCandidates, orchestrator)
	}

	return finalCandidates
}

func (c *Cluster) selectCandidatesByServerGroupsOrder(candidates couchbaseutil.MemberSet) (couchbaseutil.MemberList, error) {
	serverGroupOrder := append([]string(nil), c.cluster.Spec.Upgrade.UpgradeOrder...)

	// Append all the server groups to the end of the order, to ensure we upgrade
	// all server groups.
	allServerGroups := c.cluster.Spec.GetAllServerGroups()

	sort.Strings(allServerGroups)
	serverGroupOrder = append(serverGroupOrder, allServerGroups...)

	// Group the external members by server group
	groupedCandidates := map[string]couchbaseutil.MemberSet{}

	for _, candidate := range candidates {
		// The pod is likely down, so just skip it
		scheduledServerGroup, err := k8sutil.GetServerGroup(c.k8s, candidate.Name())
		if err != nil {
			log.Error(err, "failed to get server group for candidate", "cluster", c.namespacedName(), "candidate", candidate.Name())
			continue
		}

		if _, ok := groupedCandidates[scheduledServerGroup]; !ok {
			groupedCandidates[scheduledServerGroup] = couchbaseutil.NewMemberSet(candidate)
		} else {
			groupedCandidates[scheduledServerGroup].Add(candidate)
		}
	}

	// Only nodes from one server group can be upgraded at a time.
	// We'll move arbiter nodes to the end of the upgrade list so
	// they will be upgraded last.
	for _, group := range serverGroupOrder {
		if members, ok := groupedCandidates[group]; ok && members.Size() > 0 {
			filteredMembers := couchbaseutil.MemberList{}
			arbiterMembers := couchbaseutil.MemberList{}

			for _, member := range members {
				serverClass := c.cluster.Spec.GetServerConfigByName(member.Config())
				if serverClass != nil && serverClass.HasService("arbiter") {
					arbiterMembers = append(arbiterMembers, member)
					continue
				}

				filteredMembers = append(filteredMembers, member)
			}

			filteredMembers = append(filteredMembers, arbiterMembers...)
			return filteredMembers, nil
		}
	}

	// If there are remaining candidates not in a server group then they go last
	return candidates.ToList(), nil
}
func (c *Cluster) selectCandidatesByServicesOrder(candidates couchbaseutil.MemberSet) couchbaseutil.MemberList {
	servicesOrder := c.cluster.Spec.Upgrade.UpgradeOrder
	groupedCandidates := candidates.GroupByServerConfigs()
	filteredCandidates := couchbaseutil.MemberSet{}

	// Add the default services order to the end of the services order, to
	// ensure we upgrade services that the user didn't include.
	servicesOrder = append(servicesOrder, DefaultServicesOrder...)

	for _, service := range servicesOrder {
		for configName, members := range groupedCandidates {
			serverClass := c.cluster.Spec.GetServerConfigByName(configName)
			if serverClass != nil && serverClass.HasService(couchbasev2.Service(service)) {
				filteredCandidates.Merge(members)
			}
		}

		if filteredCandidates.Size() > 0 {
			return filteredCandidates.ToList()
		}
	}

	return candidates.ToList()
}

func (c *Cluster) selectCandidatesByServerClassesOrder(candidates couchbaseutil.MemberSet) couchbaseutil.MemberList {
	serverClassOrder := c.cluster.Spec.Upgrade.UpgradeOrder

	// Add all the server classes to the end of the order, to ensure we upgrade
	// all server classes.
	for _, sc := range c.cluster.Spec.Servers {
		serverClassOrder = append(serverClassOrder, sc.Name)
	}

	groupedCandidates := candidates.GroupByServerConfigs()

	for _, sc := range serverClassOrder {
		if members, ok := groupedCandidates[sc]; ok {
			if members.Size() > 0 {
				return members.ToList()
			}
		}
	}

	// We should never get here, but just in case
	return candidates.ToList()
}

// getRescheduleMoves determines and executes the appropriate reschedule strategy
// based on server group configuration.
func (c *Cluster) getRescheduleMoves() ([]scheduler.Move, error) {
	var moves []scheduler.Move

	if c.cluster.Spec.ServerGroupsEnabled() && !c.cluster.Spec.RescheduleDifferentServerGroup {
		// Stable mode: Full A* reschedule for systematic rebalancing
		m, err := c.scheduler.Reschedule()
		if err != nil {
			return nil, err
		}

		for _, move := range m {
			log.V(1).Info("rescheduled member", "cluster", c.namespacedName(), "name", move.Name, "from", move.From, "to", move.To)
		}

		moves = m
	} else if c.cluster.Spec.ServerGroupsEnabled() && c.cluster.Spec.RescheduleDifferentServerGroup {
		// Unstable mode: Simplified reschedule only for pods in removed server groups
		m, err := c.scheduler.RescheduleUnschedulableOnly()
		if err != nil {
			return nil, err
		}

		for _, move := range m {
			log.V(1).Info("rescheduled unschedulable member", "cluster", c.namespacedName(), "name", move.Name, "from", move.From, "to", move.To)
		}

		moves = m
	}

	return moves, nil
}

// normalizePodSpecsForComparison applies ignore rules to pod specs to avoid
// unnecessary upgrades for fields that shouldn't trigger an upgrade.
func (c *Cluster) normalizePodSpecsForComparison(actualSpec, requestedSpec *v1.PodSpec, actual *v1.Pod) error {
	// Ignore this field so that we don't force upgrades because we changed it.
	requestedSpec.TerminationGracePeriodSeconds = nil
	actualSpec.TerminationGracePeriodSeconds = nil

	// We ignore ports as they aren't configurable, this also prevents a
	// forced upgrade cycle of the cluster when upgrading the Operator
	// from 2.4 -> 2.5+
	requestedSpec.Containers[0].Ports = []v1.ContainerPort{}
	actualSpec.Containers[0].Ports = []v1.ContainerPort{}

	// Ignore readiness probe port changes
	// changed the default port from 8091 to 18091 (K8S-3828). New pods
	// will get 18091, existing pods keep their current probe.
	requestedSpec.Containers[0].ReadinessProbe.ProbeHandler.TCPSocket.Port = intstr.FromInt(18091)
	actualSpec.Containers[0].ReadinessProbe.ProbeHandler.TCPSocket.Port = intstr.FromInt(18091)

	// Don't force upgrades when we switch from migration mode to normal
	// reconciliation.
	if !c.cluster.Spec.Networking.ImprovedHostNetwork && !c.cluster.Spec.Networking.InitPodsWithNodeHostname {
		ignoreMigratedHostnameAlias(actual, requestedSpec)
	}

	// Ignore metrics container readiness probes as that has changed but is obsolete in 2.9
	requestedSpec.Containers = removeMetricsContainer(requestedSpec.Containers)
	actualSpec.Containers = removeMetricsContainer(actualSpec.Containers)

	// Ignore key shadow secret volume mount if it is not needed. But if it already exists there's no point in removing it so we do this before the comparison.
	if keyShadowSecretNeeded, err := c.needsKeyShadowSecret(); err != nil {
		return err
	} else if !keyShadowSecretNeeded {
		removeKeyShadowSecretVolumeMount(requestedSpec)
		removeKeyShadowSecretVolumeMount(actualSpec)
	}

	return nil
}

// detectZoneChange checks if the pod's server group zone has changed.
func detectZoneChange(actualSpec, requestedSpec *v1.PodSpec, pvcState *k8sutil.PersistentVolumeClaimState) bool {
	if pvcState == nil {
		return false
	}

	actualZone := ""
	requestedZone := ""
	if actualSpec.NodeSelector != nil {
		actualZone = actualSpec.NodeSelector[constants.ServerGroupLabel]
	}
	if requestedSpec.NodeSelector != nil {
		requestedZone = requestedSpec.NodeSelector[constants.ServerGroupLabel]
	}

	return actualZone != "" && requestedZone != "" && actualZone != requestedZone
}

func (c *Cluster) getUpgradeCandidates() (couchbaseutil.MemberList, map[string]couchbaseutil.Member, error) {
	allCandidates, zoneChangeDetected, err := c.needsUpgrade()
	if err != nil {
		return nil, nil, err
	}

	if c.cluster.GetUpgradeStrategy() == couchbasev2.ImmediateUpgrade {
		return allCandidates.ToList(), zoneChangeDetected, nil
	}

	numOldPods := allCandidates.Size()

	numToUpgrade := numOldPods

	if c.cluster.Spec.Upgrade != nil {
		// Check if this is a rollback (target version == baseline version)
		targetVersion, err := k8sutil.CouchbaseVersion(c.cluster.Spec.CouchbaseImage())
		if err != nil {
			return nil, nil, err
		}
		baselineVersion, err := c.state.Get(persistence.Version)
		if err != nil {
			return nil, nil, err
		}
		isRollback := targetVersion == baselineVersion

		// Only apply PreviousVersionPodCount for forward upgrades, not rollbacks
		if !isRollback {
			// Ensure we don't go negative - at minimum, we should upgrade 0 pods
			numToUpgrade = max(numOldPods-c.cluster.Spec.Upgrade.PreviousVersionPodCount, 0)
		}
		// For rollbacks, numToUpgrade remains numOldPods (upgrade all candidates)
	}

	finalCandidates := couchbaseutil.MemberList{}

	if numToUpgrade == 0 {
		return finalCandidates, nil, nil
	}

	orderedCandidates, err := c.filterCandidatesByUpgradeOrder(allCandidates)
	if err != nil {
		return nil, nil, err
	}

	// Trim the candidates to keep previousVersionPodCount.
	for _, member := range orderedCandidates {
		if len(finalCandidates) >= numToUpgrade {
			break
		}

		finalCandidates = append(finalCandidates, member)
	}

	return finalCandidates, zoneChangeDetected, nil
}

// needsUpgrade does an ordered walk down the list of members, if a member is not
// the correct version then return it as an upgrade canididate  It also returns the
// counts of members in the various versions.
func (c *Cluster) needsUpgrade() (couchbaseutil.MemberSet, map[string]couchbaseutil.Member, error) {
	candidates := couchbaseutil.MemberSet{}
	changedZones := make(map[string]couchbaseutil.Member)

	moves, err := c.getRescheduleMoves()
	if err != nil {
		return nil, nil, err
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

		pvcState, err := k8sutil.GetPodVolumes(c.k8s, member, c.cluster, *serverClass)
		if err != nil {
			return nil, nil, err
		}

		requested, err := c.regeneratePod(member, actual, serverClass, pvcState, moves)
		if err != nil {
			return nil, nil, err
		}

		// Check the specification at creation with the ones that are requested
		// currently.  If they differ then something has changed and we need to
		// "upgrade".  Otherwise accumulate the number of pods at the correct
		// target configuration.  Do this with reflection as the spec may contain
		// maps (e.g. NodeSelector)
		actualSpec := &v1.PodSpec{}

		if annotation, ok := actual.Annotations[constants.PodSpecAnnotation]; ok {
			if err := json.Unmarshal([]byte(annotation), actualSpec); err != nil {
				return nil, nil, errors.NewStackTracedError(err)
			}
		}

		requestedSpec := &v1.PodSpec{}
		if err := json.Unmarshal([]byte(requested.Annotations[constants.PodSpecAnnotation]), requestedSpec); err != nil {
			return nil, nil, errors.NewStackTracedError(err)
		}

		// Apply normalization rules to both specs
		if err := c.normalizePodSpecsForComparison(actualSpec, requestedSpec, actual); err != nil {
			return nil, nil, err
		}

		podsEqual, _ := c.resourcesEqual(actualSpec, requestedSpec)

		pvcsEqual := pvcState == nil || !pvcState.NeedsUpdate()

		// Nothing to do, carry on...
		if podsEqual && pvcsEqual {
			continue
		}

		prettyDiff := diff.PrettyDiff(actualSpec, requestedSpec)

		if !pvcsEqual {
			prettyDiff += pvcState.Diff()
		}

		log.V(1).Info("Pod upgrade candidate", "cluster", c.namespacedName(), "name", name, "diff", prettyDiff)

		// Check if zone change is needed by comparing NodeSelector
		if detectZoneChange(actualSpec, requestedSpec, pvcState) {
			changedZones[name] = member
		}

		candidates.Add(member)
	}

	return candidates, changedZones, nil
}

func removeMetricsContainer(initial []v1.Container) []v1.Container {
	containers := []v1.Container{}

	for _, c := range initial {
		if c.Name != k8sutil.MetricsContainerName {
			containers = append(containers, c)
		}
	}

	return containers
}

func removeKeyShadowSecretVolumeMount(podSpec *v1.PodSpec) {
	container, err := k8sutil.GetCouchbaseContainerFromSpec(podSpec)
	if err != nil {
		return
	}

	filterMounts := func(initialMounts []v1.VolumeMount) []v1.VolumeMount {
		volumeMounts := []v1.VolumeMount{}

		for _, v := range initialMounts {
			if v.Name != constants.CouchbaseKeyShadowVolumeName {
				volumeMounts = append(volumeMounts, v)
			}
		}

		return volumeMounts
	}

	container.VolumeMounts = filterMounts(container.VolumeMounts)

	// Also remove the volume itself from podSpec.Volumes
	filterVolumes := func(initialVolumes []v1.Volume) []v1.Volume {
		volumes := []v1.Volume{}

		for _, v := range initialVolumes {
			if v.Name != constants.CouchbaseKeyShadowVolumeName {
				volumes = append(volumes, v)
			}
		}

		return volumes
	}

	podSpec.Volumes = filterVolumes(podSpec.Volumes)
}

func ignoreMigratedHostnameAlias(actual *v1.Pod, requested *v1.PodSpec) {
	hostname, ok := actual.Annotations[constants.CouchbaseHostnameAnnotation]
	if !ok {
		return
	}

	requested.HostAliases = append(requested.HostAliases, v1.HostAlias{
		IP: "127.0.0.1",
		Hostnames: []string{
			hostname,
		},
	})
}

// reportUpgrade looks at the current state and any existing upgrade status
// condition, makes condition updates and raises events.
func (c *Cluster) reportUpgrade(status *couchbasev2.UpgradeStatus) error {
	// Look for an existing upgrading condition in the persistent storage.
	upgrading, err := c.isUpgrading()
	if err != nil {
		return err
	}

	// No existing condition, we are guaranteed to be upgrading, as opposed to rolling back.
	// Set the persistent flag and raise an event.
	if !upgrading {
		if err := c.state.Update(persistence.Upgrading, string(persistence.UpgradeActive)); err != nil {
			return err
		}

		startTime := time.Now().String()
		startTimeNoMono, _, found := strings.Cut(startTime, " m")

		if !found {
			return err
		}

		if err := c.state.Insert(persistence.UpgradeTime, startTimeNoMono); err != nil {
			return err
		}

		c.raiseEvent(k8sutil.UpgradeStartedEvent(c.cluster))
	}

	// All reports update the condition to reflect the current progress.
	c.cluster.Status.SetUpgradingCondition(status)

	return c.updateCRStatus()
}

func (c *Cluster) reportMixedMode() error {
	if c.GetLowestMemberVersion() != c.GetHighestMemberVersion() {
		c.cluster.Status.SetMixedModeCondition()
	} else {
		c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionMixedMode)
	}

	return nil
}

// reportUpgradeComplete is called unconditionally when the reconcile is complete.
// If there was an unpgrade condition and the cluster no longer needs an upgrade clear
// the condition and raise any necessary events.
func (c *Cluster) reportUpgradeComplete() error {
	upgrading, err := c.isUpgrading()
	if err != nil {
		return err
	}

	// If we're not upgrading, let's ensure the version is set to the lowest member version.
	if !upgrading {
		lowestImageVer := c.GetLowestMemberVersion()
		if lowestImageVer == "" {
			return nil
		}

		return c.state.Update(persistence.Version, lowestImageVer)
	}

	// Check to see if there are any more upgrade candidates.
	// If there are then we are still upgrading.
	candidates, _, err := c.getUpgradeCandidates()
	if err != nil {
		return err
	}

	if len(candidates) > 0 {
		return nil
	}

	// Upgrade has completed, raise and event, remove the cluster condition
	// update the current cluster version and clear the upgrading flag in
	// persistent storage.
	lowestImageVer := c.GetLowestMemberVersion()

	if err := c.state.Update(persistence.Version, lowestImageVer); err != nil {
		return err
	}

	if err := c.state.Update(persistence.Upgrading, string(persistence.UpgradeInactive)); err != nil {
		return err
	}

	c.raiseEvent(k8sutil.UpgradeFinishedEvent(c.cluster))

	c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionUpgrading)
	c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionWaitingBetweenUpgrades)

	upgradeStartTime, err := c.state.Get(persistence.UpgradeTime)
	if err != nil {
		return err
	}

	parsedStartTime, err := time.Parse("2006-01-02 15:04:05.999999999 -0700 MST", upgradeStartTime)
	if err != nil {
		return err
	}

	metrics.UpgradeDurationMSMetric.WithLabelValues(c.addOptionalLabelValues([]string{c.cluster.Name})...).Set(float64(time.Since(parsedStartTime)))

	return c.updateCRStatus()
}

// isUpgrading checks for upgrade status in state which may return inactive
// status or by some impossible means an empty string, in either case
// the upgrade is not running when we do not get 'UpgradeActive'.
func (c *Cluster) isUpgrading() (bool, error) {
	upgradeStatus, err := c.state.Get(persistence.Upgrading)

	// we'll get an error if somehow the upgrading state was unset or cleared
	if err != nil {
		return false, c.state.Insert(persistence.Upgrading, string(persistence.UpgradeInactive))
	}

	// explicitly check for active status
	return upgradeStatus == string(persistence.UpgradeActive), nil
}

// checkClusterUpgradePrerequisites checks if the cluster is ready to upgrade.
// Currently the only prerequisite is that clusters going from < 8.0.0 to 8.0.0,
// need to not have any memcached buckets.
func (c *Cluster) getUpgradeBlockers() ([]string, error) {
	startVersion, err := c.state.Get(persistence.Version)
	if err != nil {
		return nil, err
	}

	targetVersion, err := k8sutil.CouchbaseVersion(c.cluster.Spec.CouchbaseImage())
	if err != nil {
		return nil, err
	}

	if startBefore8, err := couchbaseutil.VersionBefore(startVersion, "8.0.0"); err != nil {
		return nil, err
	} else if !startBefore8 {
		return nil, nil
	}

	if targetAfter8, err := couchbaseutil.VersionAfter(targetVersion, "8.0.0"); err != nil {
		return nil, err
	} else if !targetAfter8 {
		return nil, nil
	}

	buckets := couchbaseutil.BucketStatusList{}
	if err = couchbaseutil.ListBucketStatuses(&buckets).On(c.api, c.readyMembers()); err != nil {
		return nil, err
	}

	for _, bucket := range buckets {
		if bucket.BucketType == couchbasev2.BucketTypeMemcached {
			return []string{"cluster has memcached buckets, please remove them before upgrading"}, nil
		}
	}

	return nil, nil
}

// applyPreviousVersionToNewPods modifies the additions slice in-place to set the old version
// image on new pods that should be created with the previous version during a mixed-mode
// scale-up. This is called from handleAddNode when previousVersionPodCount requires that
// some new nodes come up on the old version.
//
// The logic:
//  1. Check if we are in a forward upgrade with previousVersionPodCount > 0
//  2. Count how many existing pods are on the old (baseline) version
//  3. If previousVersionPodCount > existingOldPods, the difference tells us how many
//     new pods should use the old version
//  4. Set the Image field on the appropriate ServerConfig copies to the old version image
func (c *Cluster) applyPreviousVersionToNewPods(additions []couchbasev2.ServerConfig) error {
	if c.cluster.Spec.Upgrade == nil || c.cluster.Spec.Upgrade.PreviousVersionPodCount == 0 {
		return nil
	}

	// Get the target version from spec.image
	targetVersion, err := k8sutil.CouchbaseVersion(c.cluster.Spec.CouchbaseImage())
	if err != nil {
		return err
	}

	// Get the baseline (old) version from persistence
	baselineVersion, err := c.state.Get(persistence.Version)
	if err != nil {
		return err
	}

	// If this is a rollback (target == baseline), don't apply old version logic
	if targetVersion == baselineVersion {
		return nil
	}

	// If the cluster isn't in mixed mode, there's nothing to do
	if c.GetLowestMemberVersion() == c.GetHighestMemberVersion() {
		return nil
	}

	// Count how many existing pods are running the old (baseline) version
	existingOldPods := 0
	for _, member := range c.members {
		if member.Version() == baselineVersion {
			existingOldPods++
		}
	}

	// Determine how many new pods should use the old version
	previousVersionPodCount := c.cluster.Spec.Upgrade.PreviousVersionPodCount
	newPodsOnOldVersion := previousVersionPodCount - existingOldPods
	if newPodsOnOldVersion <= 0 {
		return nil
	}

	// Cap at the number of new pods being added
	// Since newPodsOnOldVersion is calculated as previousVersionPodCount - existingOldPods, existingOldPods may not be the the stable state number since this can happen during an update too.
	// So if this happens during an upgrade we can expect existingOldPods to settle at previousVersionPodCount.
	if newPodsOnOldVersion > len(additions) {
		// return error as calculated new pods to be added on old version is greater than the number of new pods to be added
		return errors.ErrNewPodsExceedAdditions
	}

	// Construct the old version image - What if the image to produce this version is a hash? We
	oldImage := c.GetRunningImageForVersion(baselineVersion)

	if oldImage == "" {
		return errors.ErrOldImageNotFound
	}

	log.Info("Applying previous version image to new pods during scale-up",
		"cluster", c.namespacedName(),
		"oldImage", oldImage,
		"newPodsOnOldVersion", newPodsOnOldVersion,
		"totalNewPods", len(additions),
		"previousVersionPodCount", previousVersionPodCount,
		"existingOldPods", existingOldPods)

	// Set the old version image on the first newPodsOnOldVersion additions
	for i := 0; i < newPodsOnOldVersion; i++ {
		additions[i].Image = oldImage
	}

	return nil
}
