package cluster

import (
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
)

// checks cluster version is above minimum version requirement.
func (c *Cluster) IsAtLeastVersion(v string) (bool, error) {
	tag, err := k8sutil.CouchbaseVersion(c.cluster.Spec.Image)
	if err != nil {
		return false, err
	}

	available, err := couchbaseutil.VersionAfter(tag, v)
	if err != nil {
		return false, err
	}

	return available, nil
}

// checks all pods in a cluster are above a minimum version requirement.
func (c *Cluster) RunningVersionIsAtLeast(v string) (bool, error) {
	for name := range c.members {
		actual, exists := c.k8s.Pods.Get(name)
		if !exists {
			continue
		}

		for _, con := range actual.Spec.Containers {
			tag, err := k8sutil.CouchbaseVersion(con.Image)
			if err != nil {
				return false, err
			}

			available, err := couchbaseutil.VersionAfter(tag, v)
			if err != nil {
				return false, err
			}

			if !available {
				return false, nil
			}
		}
	}

	return true, nil
}
