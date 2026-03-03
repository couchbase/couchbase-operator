/*
Copyright 2021-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package cluster

import (
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
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
