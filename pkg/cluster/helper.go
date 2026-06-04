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
	"fmt"
	"sort"
	"strings"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/client"
	"github.com/couchbase/couchbase-operator/pkg/metrics"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	vtypes "github.com/couchbase/couchbase-operator/pkg/validator/types"

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

func (c *Cluster) addOptionalLabelValues(existingLabels []string) []string {
	switch metrics.OptionalLabels {
	case metrics.UUIDonly:
		existingLabels = append(existingLabels, string(c.cluster.GetUID()))
	case metrics.UUIDandName:
		clusterName := c.cluster.Spec.ClusterSettings.ClusterName
		if clusterName == "" {
			clusterName = c.cluster.GetName()
		}

		existingLabels = append(existingLabels, string(c.cluster.GetUID()), clusterName)
	default:
		break
	}

	return existingLabels
}

func (c *Cluster) GetCouchbaseCluster() *couchbasev2.CouchbaseCluster {
	return c.cluster
}

func (c *Cluster) GetK8sClient() *client.Client {
	return c.k8s
}

func (c *Cluster) GetValidator() *vtypes.Validator {
	return c.validator
}

func (c *Cluster) UpdateFailedValidation(err error) error {
	c.cluster.Status.SetUnreconcilableCondition(err.Error())

	if err := c.updateCRStatus(); err != nil {
		return err
	}

	return nil
}

func (c *Cluster) GetRunningVersions() []*couchbaseutil.Version {
	versions := []*couchbaseutil.Version{}

	for _, member := range c.members {
		if member.Version() == "" || member.Version() == "unknown" {
			continue
		}

		version, err := couchbaseutil.NewVersion(member.Version())
		if err != nil {
			log.Error(err, "Failed to parse member version", "member", member.Name(), "version", member.Version())
			continue
		}

		versions = append(versions, version)
	}

	return versions
}

func (c *Cluster) CheckClusterCompatVersion(version string, compatAfter bool) (bool, error) {
	clusterInfo := &couchbaseutil.TerseClusterInfo{}
	if err := couchbaseutil.GetTerseClusterInfo(clusterInfo).On(c.api, c.readyMembers()); err != nil {
		return false, err
	}

	// CompatVersion returns only major.minor, without a maintenance version, so we need to add .0 so we can parse it with our version util.
	// Given this relies entirely on a contract w/ server..., calls to this method should have an appropriate fallback
	// in case that contract changes at any point.
	compatVersion := clusterInfo.ClusterCompatVersion
	if strings.Count(compatVersion, ".") == 1 {
		compatVersion = fmt.Sprintf("%s.0", compatVersion)
	}

	// When checking feature support, we want to check that the compat version is >= required version, meaning the cluster will support those features.
	if compatAfter {
		return couchbaseutil.VersionAfter(compatVersion, version)
	}

	// When checking whether we can create a pod on a previous version, we need to check the compat version <= required version. We can't just inverse the versionAfter result.
	before, err := couchbaseutil.VersionBefore(compatVersion, version)
	if err != nil {
		return before, err
	}

	after, err := couchbaseutil.VersionEqual(compatVersion, version)
	return before || after, err
}

func (c *Cluster) SupportsVersionFeatures(version string) bool {
	// Try comparing the compat version first. This only checks major.minor
	// and therefore should not be used when checking for new features
	// on maintenance releases.
	if versionAfter, err := c.CheckClusterCompatVersion(version, true); err == nil {
		return versionAfter
	}

	// Fallback to checking the lowest member version
	lowestVersion := c.GetLowestMemberVersion()

	if lowestVersion == "" {
		supports, err := c.IsAtLeastVersion(version)
		if err != nil {
			log.Error(err, "Failed to check cluster version for feature support", "version", version)
			return false
		}

		return supports
	}

	supports, err := couchbaseutil.VersionAfter(lowestVersion, version)
	if err != nil {
		log.Error(err, "Failed to check cluster version for feature support", "version", version)
		return false
	}

	return supports
}

func (c *Cluster) GetRunningImageForVersion(version string) string {
	// First check running pods
	runningPods, _ := c.getClusterPodsByPhase()

	for _, pod := range runningPods {
		if v, ok := pod.Annotations[constants.CouchbaseVersionAnnotationKey]; !ok || v != version {
			continue
		}

		containers := pod.Spec.Containers
		for _, con := range containers {
			if con.Name == constants.CouchbaseContainerName {
				return con.Image
			}
		}
	}

	// Fallback: check c.members (includes PVC-backed members) to handle deleted pods
	// This allows version preservation even when all pods of a version are deleted
	// but haven't been rebalanced out yet (PVCs still exist with version metadata)
	for _, member := range c.members {
		if member.Version() != version {
			continue
		}

		// Try to get image from PVC annotations
		for _, pvc := range c.k8s.PersistentVolumeClaims.List() {
			if name, ok := pvc.Labels[constants.LabelNode]; ok && name == member.Name() {
				if image, ok := pvc.Annotations[constants.PVCImageAnnotation]; ok {
					return image
				}
			}
		}
	}

	return ""
}

func (c *Cluster) GetLowestMemberVersion() string {
	versions := c.GetRunningVersions()

	sort.Slice(versions, func(i, j int) bool {
		return versions[i].Less(versions[j])
	})

	if len(versions) == 0 {
		return ""
	}

	return versions[0].Version()
}

func (c *Cluster) GetHighestMemberVersion() string {
	versions := c.GetRunningVersions()

	sort.Slice(versions, func(i, j int) bool {
		return versions[i].Less(versions[j])
	})

	if len(versions) == 0 {
		return ""
	}

	return versions[len(versions)-1].Version()
}

func (c *Cluster) GetEncryptionKeyFinalizer() string {
	return c.cluster.GetEncryptionKeyFinalizer()
}

func (c *Cluster) IsEncryptionAtRestManaged() bool {
	return c.SupportsVersionFeatures("8.0.0") && c.cluster.IsEncryptionAtRestManaged()
}

// DisableAutoFailover will disable auto-failover if it is currently enabled, and return a function that can be called to restore the previous setting.
func (c *Cluster) DisableAutoFailover() (func(), error) {
	failoverSettings := &couchbaseutil.AutoFailoverSettings{}
	if err := couchbaseutil.GetAutoFailoverSettings(failoverSettings).On(c.api, c.readyMembers()); err != nil {
		return nil, err
	}

	if failoverSettings.Enabled {
		newFailoverSettings := *failoverSettings
		newFailoverSettings.Enabled = false

		if err := couchbaseutil.SetAutoFailoverSettings(&newFailoverSettings).RetryFor(10*time.Second).On(c.api, c.readyMembers()); err != nil {
			return nil, err
		}

		return func() {
			_ = couchbaseutil.SetAutoFailoverSettings(failoverSettings).RetryFor(10*time.Second).On(c.api, c.readyMembers())
		}, nil
	}

	// Return a noop
	return func() {}, nil
}
