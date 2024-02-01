package cluster

import (
	"encoding/json"

	"github.com/couchbase/couchbase-operator/pkg/cluster/persistence"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/scheduler"
	v1 "k8s.io/api/core/v1"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
)

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

		podsEqual, d := c.resourcesEqual(actualSpec, requestedSpec)

		pvcsEqual := pvcState == nil || !pvcState.NeedsUpdate()

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

		c.raiseEvent(k8sutil.UpgradeStartedEvent(c.cluster))
	}

	// All reports update the condition to reflect the current progress.
	c.cluster.Status.SetUpgradingCondition(status)

	return c.updateCRStatus()
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
	lowestImageVer, err := c.cluster.Spec.LowestInUseCouchbaseVersionImage()
	if err != nil {
		return err
	}

	version, err := k8sutil.CouchbaseVersion(lowestImageVer)
	if err != nil {
		return err
	}

	if err := c.state.Update(persistence.Version, version); err != nil {
		return err
	}

	if err := c.state.Update(persistence.Upgrading, string(persistence.UpgradeInactive)); err != nil {
		return err
	}

	c.raiseEvent(k8sutil.UpgradeFinishedEvent(c.cluster))

	c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionUpgrading)

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
