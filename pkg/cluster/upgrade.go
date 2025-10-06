package cluster

import (
	"encoding/json"
	"slices"
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
)

var DefaultServicesOrder = []string{"data", "query", "index", "search", "analytics", "eventing"}

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

func (c *Cluster) filterCandidatesByUpgradeOrder(candidates couchbaseutil.MemberSet, numToUpgrade int) (couchbaseutil.MemberSet, error) {
	if c.cluster.Spec.Upgrade == nil {
		return candidates, nil
	}

	upgradeOrderType := c.cluster.Spec.Upgrade.UpgradeOrderType

	// Default to one at a time
	maxUpgradable, err := c.cluster.GetMaxUpgradable()
	if err != nil {
		return nil, err
	}

	switch upgradeOrderType {
	case couchbasev2.UpgradeOrderTypeNodes:
		return c.selectCandidatesByNodesOrder(candidates, maxUpgradable, numToUpgrade), nil
	case couchbasev2.UpgradeOrderTypeServerGroups:
		return c.selectCandidatesByServerGroupsOrder(candidates)
	case couchbasev2.UpgradeOrderTypeServerClasses:
		return c.selectCandidatesByServerClassesOrder(candidates), nil
	case couchbasev2.UpgradeOrderTypeServices:
		return c.selectCandidatesByServicesOrder(candidates), nil
	}

	return candidates, nil
}

func (c *Cluster) selectCandidatesByNodesOrder(candidates couchbaseutil.MemberSet, maxUpgradable int, numToUpgrade int) couchbaseutil.MemberSet {
	selectionSize := min(maxUpgradable, numToUpgrade)
	nodeOrder := c.cluster.Spec.Upgrade.UpgradeOrder
	finalCandidates := couchbaseutil.NewMemberSet()

	for _, node := range nodeOrder {
		if finalCandidates.Size() >= selectionSize {
			return finalCandidates
		}

		if candidates.Contains(node) {
			finalCandidates.Add(candidates[node])
		}
	}

	if finalCandidates.Size() == selectionSize {
		return finalCandidates
	}

	// If the orchestrator is in the candidates list, then separate it from the rest and add it at the end
	var orchestrator couchbaseutil.Member

	clusterInfo := &couchbaseutil.TerseClusterInfo{}
	if err := couchbaseutil.GetTerseClusterInfo(clusterInfo).On(c.api, c.readyMembers()); err != nil {
		log.Error(err, "failed to get cluster info")
	} else {
		orchestratorName := clusterInfo.Orchestrator
		candidates, orchestrator = separateCandidatesAndOrchestrator(candidates, orchestratorName)
	}

	// Go through the rest in alphabetical order
	sortedNodes := candidates.Names()
	sort.Strings(sortedNodes)

	for _, node := range sortedNodes {
		if finalCandidates.Size() >= selectionSize {
			return finalCandidates
		}

		if !finalCandidates.Contains(node) {
			finalCandidates.Add(candidates[node])
		}
	}

	if orchestrator != nil && finalCandidates.Size() < selectionSize {
		finalCandidates.Add(orchestrator)
	}

	return finalCandidates
}

func (c *Cluster) selectCandidatesByServerGroupsOrder(candidates couchbaseutil.MemberSet) (couchbaseutil.MemberSet, error) {
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
			log.Error(err, "failed to get server group for candidate", "candidate", candidate.Name())
			continue
		}

		if _, ok := groupedCandidates[scheduledServerGroup]; !ok {
			groupedCandidates[scheduledServerGroup] = couchbaseutil.NewMemberSet(candidate)
		} else {
			groupedCandidates[scheduledServerGroup].Add(candidate)
		}
	}

	for _, group := range serverGroupOrder {
		if members, ok := groupedCandidates[group]; ok {
			if members.Size() > 0 {
				return members, nil
			}
		}
	}

	// If there are remaining candidates not in a server group then they go last
	return candidates, nil
}
func (c *Cluster) selectCandidatesByServicesOrder(candidates couchbaseutil.MemberSet) couchbaseutil.MemberSet {
	servicesOrder := c.cluster.Spec.Upgrade.UpgradeOrder
	groupedCandidates := candidates.GroupByServerConfigs()
	filteredCandidates := couchbaseutil.MemberSet{}

	// Add the default services order to the end of the services order, to
	// ensure we upgrade services that the user didn't include.
	servicesOrder = append(servicesOrder, DefaultServicesOrder...)

	for _, service := range servicesOrder {
		for configName, members := range groupedCandidates {
			serverClass := c.cluster.Spec.GetServerConfigByName(configName)
			if serverClass != nil && slices.Contains(serverClass.Services, couchbasev2.Service(service)) {
				filteredCandidates.Merge(members)
			}
		}

		if filteredCandidates.Size() > 0 {
			return filteredCandidates
		}
	}

	return candidates
}

func (c *Cluster) selectCandidatesByServerClassesOrder(candidates couchbaseutil.MemberSet) couchbaseutil.MemberSet {
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
				return members
			}
		}
	}

	// We should never get here, but just in case
	return candidates
}

func (c *Cluster) getUpgradeCandidates() (couchbaseutil.MemberSet, error) {
	allCandidates, err := c.needsUpgrade()
	if err != nil {
		return nil, err
	}

	if c.cluster.GetUpgradeStrategy() == couchbasev2.ImmediateUpgrade {
		return allCandidates, nil
	}

	numOldPods := allCandidates.Size()

	numToUpgrade := numOldPods

	if c.cluster.Spec.Upgrade != nil {
		numToUpgrade = numOldPods - c.cluster.Spec.Upgrade.PreviousVersionPodCount
	}

	filteredCandidates, err := c.filterCandidatesByUpgradeOrder(allCandidates, numToUpgrade)
	if err != nil {
		return nil, err
	}

	finalCandidates := couchbaseutil.MemberSet{}

	for _, member := range filteredCandidates {
		if finalCandidates.Size() >= numToUpgrade {
			break
		}

		finalCandidates.Add(member)

		log.Info("Final pod upgrade candidate", "cluster", c.namespacedName(), "name", member.Name(), "version", member.Version())
	}

	return finalCandidates, nil
}

// needsUpgrade does an ordered walk down the list of members, if a member is not
// the correct version then return it as an upgrade canididate  It also returns the
// counts of members in the various versions.
//
//nolint:gocognit
func (c *Cluster) needsUpgrade() (couchbaseutil.MemberSet, error) {
	candidates := couchbaseutil.MemberSet{}

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
			return nil, err
		}

		requested, err := c.regeneratePod(member, actual, serverClass, pvcState, moves)
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

		// Ignore this field so that we don't force upgrades because we changed it.
		requestedSpec.TerminationGracePeriodSeconds = nil
		actualSpec.TerminationGracePeriodSeconds = nil

		// We ignore ports as they aren't configurable, this also prevents a
		// forced upgrade cycle of the cluster when upgrading the Operator
		// from 2.4 -> 2.5+
		requestedSpec.Containers[0].Ports = []v1.ContainerPort{}
		actualSpec.Containers[0].Ports = []v1.ContainerPort{}

		// Don't force upgrades when we switch from migration mode to normal
		// reconciliation.
		if !c.cluster.Spec.Networking.ImprovedHostNetwork && !c.cluster.Spec.Networking.InitPodsWithNodeHostname {
			ignoreMigratedHostnameAlias(actual, requestedSpec)
		}

		// Ignore metrics container readiness probes as that has changed but is obsolete in 2.9
		requestedSpec.Containers = removeMetricsContainer(requestedSpec.Containers)
		actualSpec.Containers = removeMetricsContainer(actualSpec.Containers)

		podsEqual, _ := c.resourcesEqual(actualSpec, requestedSpec)

		pvcsEqual := pvcState == nil || !pvcState.NeedsUpdate()

		if is80, err := couchbaseutil.VersionAfter(member.Version(), "8.0.0"); err != nil {
			return nil, err
		} else if is80 {
			// Ignore key shadow secret volume mount if it is not NEEDED
			if keyShadowSecretNeeded, err := c.needsKeyShadowSecret(); err != nil {
				return nil, err
			} else if !keyShadowSecretNeeded {
				removeKeyShadowSecretVolumeMount(requestedSpec)
				removeKeyShadowSecretVolumeMount(actualSpec)
			}
		}

		// Nothing to do, carry on...
		if podsEqual && pvcsEqual {
			continue
		}

		prettyDiff := diff.PrettyDiff(actualSpec, requestedSpec)

		if !pvcsEqual {
			prettyDiff += pvcState.Diff()
		}

		log.V(1).Info("Pod upgrade candidate", "cluster", c.namespacedName(), "name", name, "diff", prettyDiff)

		candidates.Add(member)
	}

	return candidates, nil
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
	if upgrading, err := c.isUpgrading(); err != nil || !upgrading {
		// There is no condition, we weren't upgrading, do nothing
		return err
	}

	// Check to see if there are any more upgrade candidates.
	// If there are then we are still upgrading.
	candidates, err := c.getUpgradeCandidates()
	if err != nil {
		return err
	}

	if !candidates.Empty() {
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
