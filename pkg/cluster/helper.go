package cluster

import (
	"time"

	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// checks cluster version is above minimum version requirement.
func (c *Cluster) IsAtLeastVersion(v string) (bool, error) {
	return c.cluster.IsAtLeastVersion(v)
}

func (c *Cluster) VersionBefore(v string, requiredVersion string) (bool, error) {
	return couchbaseutil.VersionBefore(v, requiredVersion)
}

// checks all pods in a cluster are above a minimum version requirement.
func (c *Cluster) RunningVersionIsAtLeast(v string) (bool, error) {
	// we'll rely on spec version when cluster is not yet initialized.
	if len(c.members) == 0 {
		return c.IsAtLeastVersion(v)
	}

	for name := range c.members {
		actual, exists := c.k8s.Pods.Get(name)
		if !exists {
			continue
		}

		for _, con := range actual.Spec.Containers {
			if con.Name != constants.CouchbaseContainerName {
				continue
			}

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

func (config *Config) GetDeleteOptions() metav1.DeleteOptions {
	deleteDelayInSeconds := int64(config.PodDeleteDelay / time.Second)

	return *metav1.NewDeleteOptions(deleteDelayInSeconds)
}

func (config Config) GetPodReadinessConfig() k8sutil.PodReadinessConfig {
	return k8sutil.PodReadinessConfig{
		PodReadinessDelay:  config.PodReadinessDelay,
		PodReadinessPeriod: config.PodReadinessPeriod,
	}
}
