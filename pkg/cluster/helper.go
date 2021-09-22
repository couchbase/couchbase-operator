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
