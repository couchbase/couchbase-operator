/*
Copyright 2026-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	opconstants "github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// The Operator requeues every ten seconds, so anything meant to happen "on the
// next cycle" has comfortably happened inside a minute.
const (
	unreconcilableTimeout = 2 * time.Minute

	// requeueInterval is the cluster's own reconcile period. Tests that need to
	// span several cycles count in these.
	requeueInterval = 10 * time.Second
)

const (
	leiaBucket = "leia"

	jarJarBucket = "jar-jar"

	lukeBucket = "luke"

	bactaTankBucket = "bacta-tank"

	hothBucket = "hoth"

	alderaanBucket = "alderaan"

	kesselRunReplication = "kessel-run"
)

// skipDAC marks a resource so the admission controller will admit a spec it
// would normally turn away at the door. Every test here needs it, because the
// whole point is to sneak an invalid resource past the webhook and put it in
// front of the Operator, which is the only way the Unreconcilable condition
// ever gets exercised.
//
// It does not switch off the Operator's own validation. See validatable() in
// pkg/validationrunner for why not.
func skipDAC() map[string]string {
	return map[string]string{opconstants.AnnotationDisableAdmissionController: "true"}
}

// bucketCondition fetches a bucket's Unreconcilable condition as written by the
// named cluster.
//
// It uses the typed client rather than e2eutil's unstructured condition helpers
// because those return on the first entry of a matching type, and a resource
// selected by two clusters quite legitimately carries two entries of type
// Unreconcilable, one per cluster.
func bucketCondition(k8s *types.Cluster, name, clusterName string) (*metav1.Condition, error) {
	bucket, err := k8s.CRClient.CouchbaseV2().CouchbaseBuckets(k8s.Namespace).
		Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	condition, found := couchbasev2.GetUnreconcilable(bucket, clusterName)
	if !found {
		return nil, fmt.Errorf("bucket %s has no Unreconcilable condition for cluster %s", name, clusterName)
	}

	return condition, nil
}

// userCondition is bucketCondition for CouchbaseUser.
func userCondition(k8s *types.Cluster, name, clusterName string) (*metav1.Condition, error) {
	user, err := k8s.CRClient.CouchbaseV2().CouchbaseUsers(k8s.Namespace).
		Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	condition, found := couchbasev2.GetUnreconcilable(user, clusterName)
	if !found {
		return nil, fmt.Errorf("user %s has no Unreconcilable condition for cluster %s", name, clusterName)
	}

	return condition, nil
}

// replicationCondition is bucketCondition for CouchbaseReplication.
func replicationCondition(k8s *types.Cluster, name, clusterName string) (*metav1.Condition, error) {
	replication, err := k8s.CRClient.CouchbaseV2().CouchbaseReplications(k8s.Namespace).
		Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	condition, found := couchbasev2.GetUnreconcilable(replication, clusterName)
	if !found {
		return nil, fmt.Errorf("replication %s has no Unreconcilable condition for cluster %s", name, clusterName)
	}

	return condition, nil
}

// conditionIs builds a check, for use with MustAssertFor, that a condition has
// reached the status and reason we were hoping for.
func conditionIs(get func() (*metav1.Condition, error), status metav1.ConditionStatus, reason string) func() error {
	return func() error {
		condition, err := get()
		if err != nil {
			return err
		}

		if condition.Status != status {
			return fmt.Errorf("condition status is %v, want %v (reason %q, message %q)",
				condition.Status, status, condition.Reason, condition.Message)
		}

		if reason != "" && condition.Reason != reason {
			return fmt.Errorf("condition reason is %q, want %q", condition.Reason, reason)
		}

		return nil
	}
}

// newTestBucket builds a minimal, perfectly valid bucket.
func newTestBucket(name string) *couchbasev2.CouchbaseBucket {
	return &couchbasev2.CouchbaseBucket{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: skipDAC(),
		},
		Spec: couchbasev2.CouchbaseBucketSpec{
			MemoryQuota:        e2espec.NewResourceQuantityMi(128),
			Replicas:           0,
			IoPriority:         couchbasev2.CouchbaseBucketIOPriorityHigh,
			EvictionPolicy:     couchbasev2.CouchbaseBucketEvictionPolicyFullEviction,
			ConflictResolution: couchbasev2.CouchbaseBucketConflictResolutionSequenceNumber,
		},
	}
}

func TestUnreconcilableHealthyResourcesReportFalse(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	cluster := e2eutil.MustNewClusterFromSpec(t, kubernetes, clusterOptions().WithEphemeralTopology(1).Generate(kubernetes))

	// Leia behaves impeccably throughout.
	leia := e2eutil.MustNewBucket(t, kubernetes, newTestBucket(leiaBucket))
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, leia, unreconcilableTimeout)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, unreconcilableTimeout)

	e2eutil.MustAssertFor(t, unreconcilableTimeout, conditionIs(
		func() (*metav1.Condition, error) { return bucketCondition(kubernetes, leiaBucket, cluster.Name) },
		metav1.ConditionFalse, "Validated"))

	// The condition names the cluster that reached the verdict, since one
	// resource can be selected by more than one of them.
	condition, err := bucketCondition(kubernetes, leiaBucket, cluster.Name)
	if err != nil {
		e2eutil.Die(t, err)
	}

	if got := couchbasev2.UnreconcilableClusterName(condition); got != cluster.Name {
		e2eutil.Die(t, fmt.Errorf("condition is scoped to cluster %q, want %q", got, cluster.Name))
	}
}

// TestUnreconcilableImmutableFieldChange covers the headline case. One bad
// resource gets skipped and reported, while its neighbours and the cluster
// carry on entirely unbothered.
func TestUnreconcilableImmutableFieldChange(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	cluster := e2eutil.MustNewClusterFromSpec(t, kubernetes, clusterOptions().WithEphemeralTopology(1).Generate(kubernetes))

	// Jar Jar is about to ruin his own spec. Luke, next door, has done nothing
	// to deserve any of this.
	jarJar := e2eutil.MustNewBucket(t, kubernetes, newTestBucket(jarJarBucket))
	luke := e2eutil.MustNewBucket(t, kubernetes, newTestBucket(lukeBucket))

	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, jarJar, unreconcilableTimeout)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, luke, unreconcilableTimeout)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, unreconcilableTimeout)

	// spec.conflictResolution cannot be changed after creation.
	e2eutil.MustPatchBucket(t, kubernetes, jarJar,
		jsonpatch.NewPatchSet().Replace("/spec/conflictResolution", couchbasev2.CouchbaseBucketConflictResolutionTimestamp),
		time.Minute)

	jarJarIsMarked := conditionIs(
		func() (*metav1.Condition, error) { return bucketCondition(kubernetes, jarJarBucket, cluster.Name) },
		metav1.ConditionTrue, "ImmutableFieldChanged")

	e2eutil.MustAssertFor(t, unreconcilableTimeout, jarJarIsMarked)

	// Ensuring the resource is ignored rather than deleted
	e2eutil.MustPatchBucketInfo(t, kubernetes, cluster, jarJarBucket,
		jsonpatch.NewPatchSet().Test("/ConflictResolution",
			string(couchbasev2.CouchbaseBucketConflictResolutionSequenceNumber)),
		time.Minute)

	e2eutil.MustHoldFor(t, 3*requeueInterval, jarJarIsMarked)

	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, jarJar, unreconcilableTimeout)

	// The blast radius is exactly one bucket. Luke is fine.
	e2eutil.MustAssertFor(t, unreconcilableTimeout, conditionIs(
		func() (*metav1.Condition, error) { return bucketCondition(kubernetes, lukeBucket, cluster.Name) },
		metav1.ConditionFalse, "Validated"))

	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, luke, unreconcilableTimeout)
}

// TestUnreconcilableSelfHealsWhenSpecFixed checks there is no sticky state left
// to clear. Fixing the spec is the whole remedy, and it takes effect on the
// next cycle.
func TestUnreconcilableSelfHealsWhenSpecFixed(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	cluster := e2eutil.MustNewClusterFromSpec(t, kubernetes, clusterOptions().WithEphemeralTopology(1).Generate(kubernetes))

	bacta := e2eutil.MustNewBucket(t, kubernetes, newTestBucket(bactaTankBucket))
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bacta, unreconcilableTimeout)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, unreconcilableTimeout)

	bacta = e2eutil.MustPatchBucket(t, kubernetes, bacta,
		jsonpatch.NewPatchSet().Replace("/spec/conflictResolution", couchbasev2.CouchbaseBucketConflictResolutionTimestamp),
		time.Minute)

	e2eutil.MustAssertFor(t, unreconcilableTimeout, conditionIs(
		func() (*metav1.Condition, error) { return bucketCondition(kubernetes, bactaTankBucket, cluster.Name) },
		metav1.ConditionTrue, "ImmutableFieldChanged"))

	// Put it back. No annotation to remove, no CR to recreate, nothing to tidy.
	e2eutil.MustPatchBucket(t, kubernetes, bacta,
		jsonpatch.NewPatchSet().Replace("/spec/conflictResolution", couchbasev2.CouchbaseBucketConflictResolutionSequenceNumber),
		time.Minute)

	e2eutil.MustAssertFor(t, unreconcilableTimeout, conditionIs(
		func() (*metav1.Condition, error) { return bucketCondition(kubernetes, bactaTankBucket, cluster.Name) },
		metav1.ConditionFalse, "Validated"))

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, unreconcilableTimeout)
}

// TestUnreconcilableUserGoesInvalidMidLifetime guards a behaviour that simply
// did not exist before this change.
//
// Constraint validation for users, groups, backups, scopes and collections used
// to run exactly once, when the controller first adopted the cluster. A
// resource that turned bad after that was never looked at again, so nothing
// marked it and nothing skipped it. It now has to be caught on the next cycle.
func TestUnreconcilableUserGoesInvalidMidLifetime(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	cluster := e2eutil.MustNewClusterFromSpec(t, kubernetes, clusterOptions().WithEphemeralTopology(1).Generate(kubernetes))
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, unreconcilableTimeout)

	// A valid user, adopted while everything is still going well.
	user := e2espec.NewDefaultUser()
	user.Annotations = skipDAC()
	user = e2eutil.MustNewUser(t, kubernetes, user)

	e2eutil.MustAssertFor(t, unreconcilableTimeout, conditionIs(
		func() (*metav1.Condition, error) { return userCondition(kubernetes, user.Name, cluster.Name) },
		metav1.ConditionFalse, "Validated"))

	e2eutil.MustPatchUser(t, kubernetes, user,
		jsonpatch.NewPatchSet().Replace("/spec/authSecret", ""), time.Minute)

	e2eutil.MustAssertFor(t, unreconcilableTimeout, conditionIs(
		func() (*metav1.Condition, error) { return userCondition(kubernetes, user.Name, cluster.Name) },
		metav1.ConditionTrue, "ValidationFailed"))

	// The cluster is unharmed. One invalid user is not a cluster-level failure.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, unreconcilableTimeout)
}

// TestUnreconcilableStaysSkippedAcrossCycles is the other half of that guard,
// and the one most likely to catch this change going wrong. A tracker that is
// emptied every cycle but never refilled would drop the mark on cycle two, and
// the Operator would cheerfully start pushing the invalid resource at Couchbase
// Server.
func TestUnreconcilableStaysSkippedAcrossCycles(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	cluster := e2eutil.MustNewClusterFromSpec(t, kubernetes, clusterOptions().WithEphemeralTopology(1).Generate(kubernetes))
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, unreconcilableTimeout)

	user := e2espec.NewDefaultUser()
	user.Annotations = skipDAC()
	user.Spec.AuthSecret = ""
	user = e2eutil.MustNewUser(t, kubernetes, user)

	check := conditionIs(
		func() (*metav1.Condition, error) { return userCondition(kubernetes, user.Name, cluster.Name) },
		metav1.ConditionTrue, "ValidationFailed")

	e2eutil.MustAssertFor(t, unreconcilableTimeout, check)

	// Hold across at least five requeues.
	e2eutil.MustHoldFor(t, 6*requeueInterval, check)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, unreconcilableTimeout)
}

// TestUnreconcilableCausesNoGitOpsChurn checks the defect that motivated the
// whole change. The Operator used to record its verdict by writing an
// annotation through a whole-object Update of a cached resource, which made it
// the last writer of spec and all of metadata on every single judgement. ArgoCD
// and Flux read that as drift and never stop reporting it.
//
// Conditions now go through the status subresource, so a healthy resource's
// metadata must not budge at all. Hence Hoth.
func TestUnreconcilableCausesNoGitOpsChurn(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	cluster := e2eutil.MustNewClusterFromSpec(t, kubernetes, clusterOptions().WithEphemeralTopology(1).Generate(kubernetes))

	hoth := e2eutil.MustNewBucket(t, kubernetes, newTestBucket(hothBucket))
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, hoth, unreconcilableTimeout)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, unreconcilableTimeout)

	// Let the condition settle first, since that write is entirely expected.
	e2eutil.MustAssertFor(t, unreconcilableTimeout, conditionIs(
		func() (*metav1.Condition, error) { return bucketCondition(kubernetes, hothBucket, cluster.Name) },
		metav1.ConditionFalse, "Validated"))

	settled, err := kubernetes.CRClient.CouchbaseV2().CouchbaseBuckets(kubernetes.Namespace).
		Get(context.Background(), hothBucket, metav1.GetOptions{})
	if err != nil {
		e2eutil.Die(t, err)
	}

	// Over the next ten reconciles nothing about this object may move. Not the
	// annotations, not the spec, and not even the resourceVersion, because the
	// condition is already correct and the writer's no-op guard suppresses
	// redundant writes.
	e2eutil.MustHoldFor(t, 10*requeueInterval, func() error {
		current, err := kubernetes.CRClient.CouchbaseV2().CouchbaseBuckets(kubernetes.Namespace).
			Get(context.Background(), hothBucket, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if current.ResourceVersion != settled.ResourceVersion {
			return fmt.Errorf("resourceVersion moved from %s to %s: the Operator is still writing every cycle",
				settled.ResourceVersion, current.ResourceVersion)
		}

		if len(current.Annotations) != len(settled.Annotations) {
			return fmt.Errorf("annotations changed from %v to %v", settled.Annotations, current.Annotations)
		}

		for key, want := range settled.Annotations {
			if got := current.Annotations[key]; got != want {
				return fmt.Errorf("annotation %q changed from %q to %q", key, want, got)
			}
		}

		return nil
	})
}

func TestUnreconcilableXDCRReplicationDependencyMissing(t *testing.T) {
	kubernetes, cleanup := framework.Global.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).CouchbaseBucket()

	// This establishes both clusters, the bucket, and a working replication,
	// which is what turns on spec.xdcr.managed and registers the remote cluster
	// whose selector matches the doomed replication below.
	sourceCluster, _, bucket := createXDCRClusters(t, kubernetes, kubernetes, nil, nil, nil, 1)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, sourceCluster, unreconcilableTimeout)

	// And now one pointed at a source bucket that does not exist, and never
	// will, on account of Alderaan.
	doomed := e2espec.GetReplication(alderaanBucket, bucket.GetName())
	doomed.GenerateName = ""
	doomed.Name = kesselRunReplication
	doomed.Annotations = skipDAC()

	if _, err := kubernetes.CRClient.CouchbaseV2().CouchbaseReplications(kubernetes.Namespace).
		Create(context.Background(), doomed, metav1.CreateOptions{}); err != nil {
		e2eutil.Die(t, err)
	}

	e2eutil.MustAssertFor(t, unreconcilableTimeout, conditionIs(
		func() (*metav1.Condition, error) {
			return replicationCondition(kubernetes, doomed.Name, sourceCluster.Name)
		},
		metav1.ConditionTrue, "DependencyMissing"))

	// Every other replication, meaning the working one the fixture created, is
	// left completely alone. The blast radius is one resource.
	e2eutil.MustAssertFor(t, unreconcilableTimeout, func() error {
		replications, err := kubernetes.CRClient.CouchbaseV2().CouchbaseReplications(kubernetes.Namespace).
			List(context.Background(), metav1.ListOptions{})
		if err != nil {
			return err
		}

		for i := range replications.Items {
			replication := &replications.Items[i]
			if replication.Name == doomed.Name {
				continue
			}

			condition, found := couchbasev2.GetUnreconcilable(replication, sourceCluster.Name)
			if !found {
				return fmt.Errorf("replication %s has no Unreconcilable condition yet", replication.Name)
			}

			if condition.Status != metav1.ConditionFalse {
				return fmt.Errorf("healthy replication %s is %v, want False (%q)",
					replication.Name, condition.Status, condition.Message)
			}
		}

		return nil
	})

	// And unlike the immutable-field case, XDCR errors are log-only. One broken
	// replication must not drag the cluster into an error state.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, sourceCluster, unreconcilableTimeout)
}
