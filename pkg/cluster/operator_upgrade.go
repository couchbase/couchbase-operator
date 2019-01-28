package cluster

import (
	"reflect"

	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/validator"
	"github.com/couchbase/couchbase-operator/pkg/version"
)

// upgradeRange specifies the range of versions to apply an upgrade action to.
type upgradeRange struct {
	// From allows an upgrade action to occur if a reosurce is greater than or equal to this version.
	from string
	// To allows an upgrade action to occur if a resource less than this version.
	to string
}

// actionable performs the generic logic to determine whether or not to perform an
// upgrade action.
func (u upgradeRange) actionable(v string) (bool, error) {
	from, err := couchbaseutil.NewVersion(u.from)
	if err != nil {
		return false, err
	}

	to, err := couchbaseutil.NewVersion(u.to)
	if err != nil {
		return false, err
	}

	version, err := couchbaseutil.NewVersion(v)
	if err != nil {
		return false, err
	}

	// We only consider running the upgrade if the current version is greater
	// than or equal to the lower bound, and less than the upper bound.
	return version.GreaterEqual(from) && version.Less(to), nil

}

// upgradableResource is an abstraction around an upgradable resource type.
type upgradableResource interface {
	// kind is the kind of resource e.g. pod, service.
	kind() string
	// name is the name of the requested resource.
	name(item int) string
	// fetch fetches the resource list from the Kubernetes API.
	fetch() error
	// lenItems returns the number of resource items found by a fetch.
	lenItems() int
	// itemVersion returns the operator version number of the resource (defaulting to 0.0.0).
	itemVersion(item int) string
	// lenActions returns the number of upgrade actions to try performing.
	lenActions() int
	// actionVersionRange is the actionable version range to apply the upgrade to.
	actionVersionRange(action int) upgradeRange
	// perform performs the upgrade action on the resource item.
	perform(item, action int) error
	// commit updates the specified resource via the Kubernetes API.
	commit(item int) error
}

// operatorUpgrade is the generic upgrade algorithm.  For each registered resource type
// it iterates through each discoverred item attempting to apply upgrade actions.  Once
// complete it will commit upgraded items back to Kubernetes to persist the upgrades.
func (c *Cluster) operatorUpgrade() error {
	// Check first whether any new defaults need applying to the existing cluster.
	cluster := c.cluster.DeepCopy()
	validator.ApplyDefaults(c.cluster)
	if !reflect.DeepEqual(c.cluster, cluster) {
		c.logger.Infof("upgrading %s/%s to version %s", cluster.Kind, cluster.Name, version.Version)
		cluster, err := c.config.CouchbaseCRCli.CouchbaseV1().CouchbaseClusters(c.cluster.Namespace).Update(cluster)
		if err != nil {
			return err
		}
		c.cluster = cluster
	}

	resources := []upgradableResource{
		newPodUpgradableResource(c),
		newPVCUpgradableResource(c),
	}
	for _, resource := range resources {
		if err := resource.fetch(); err != nil {
			return err
		}
		upgraded := map[int]interface{}{}
		for item := 0; item < resource.lenItems(); item++ {
			for action := 0; action < resource.lenActions(); action++ {
				versionRange := resource.actionVersionRange(action)
				isActionable, err := versionRange.actionable(resource.itemVersion(item))
				if err != nil {
					return err
				}
				if !isActionable {
					continue
				}
				c.logger.Infof("upgrading %s/%s to version %s", resource.kind(), resource.name(item), versionRange.to)
				if err := resource.perform(item, action); err != nil {
					return err
				}
				upgraded[item] = nil
			}
		}
		for item, _ := range upgraded {
			if err := resource.commit(item); err != nil {
				return err
			}
		}
	}
	return nil
}
