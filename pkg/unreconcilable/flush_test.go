/*
Copyright 2026-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package unreconcilable

import (
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/client"
	cbfake "github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned/fake"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stesting "k8s.io/client-go/testing"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const testNamespace = "default"

var testLog = logf.Log.WithName("test")

// bucketAdapter is the CouchbaseBucket entry from the registry, which every
// test below drives directly. The cache-backed list function wants informers,
// but the write path is happy with nothing more than a clientset.
func bucketAdapter(t *testing.T) adapter {
	t.Helper()

	for _, candidate := range registry {
		if candidate.kind == couchbasev2.BucketCRDResourceKind {
			return candidate
		}
	}

	t.Fatal("no adapter for CouchbaseBucket")

	return adapter{}
}

func newTestTracker(objects ...runtime.Object) (*Tracker, *client.Client, *cbfake.Clientset) {
	cbClient := cbfake.NewSimpleClientset(objects...)
	tracker := New(testClusterName)

	// A fixed clock, so LastTransitionTime comparisons actually mean something.
	tracker.now = func() metav1.Time { return metav1.NewTime(time.Unix(1000, 0).UTC()) }

	return tracker, &client.Client{CouchbaseClient: cbClient}, cbClient
}

func newBucket(name string, conditions ...metav1.Condition) *couchbasev2.CouchbaseBucket {
	return &couchbasev2.CouchbaseBucket{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace, Generation: 3},
		Status:     couchbasev2.CouchbaseBucketStatus{Conditions: conditions},
	}
}

// countStatusUpdates records how many writes reach the status subresource.
func countStatusUpdates(cbClient *cbfake.Clientset, counter *int) {
	cbClient.PrependReactor("update", "couchbasebuckets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() == "status" {
			*counter++
		}

		return false, nil, nil
	})
}

// TestFlushWritesThroughStatusSubresource is the happy path. A marked resource
// gets True, and it lands on /status rather than on the resource itself.
func TestFlushWritesThroughStatusSubresource(t *testing.T) {
	bucket := newBucket("cr")
	tracker, k8s, cbClient := newTestTracker(bucket)

	var statusUpdates int

	countStatusUpdates(cbClient, &statusUpdates)

	tracker.Mark(bucketRef("cr"), ReasonValidationFailed, "memoryQuota is too small")

	tracker.flushObject(t.Context(), k8s, testLog, bucketAdapter(t), bucket)

	if statusUpdates != 1 {
		t.Fatalf("status updates = %d, want 1", statusUpdates)
	}

	stored, err := cbClient.CouchbaseV2().CouchbaseBuckets(testNamespace).Get(t.Context(), "cr", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to read back the bucket: %v", err)
	}

	condition, found := couchbasev2.GetUnreconcilable(stored, testClusterName)
	if !found {
		t.Fatal("no Unreconcilable condition was written")
	}

	if condition.Status != metav1.ConditionTrue {
		t.Errorf("status = %q, want True", condition.Status)
	}

	if condition.Reason != string(ReasonValidationFailed) {
		t.Errorf("reason = %q, want %q", condition.Reason, ReasonValidationFailed)
	}

	if condition.ObservedGeneration != 3 {
		t.Errorf("observedGeneration = %d, want the resource's generation 3", condition.ObservedGeneration)
	}

	if got := couchbasev2.UnreconcilableDetail(condition); got != "memoryQuota is too small" {
		t.Errorf("detail = %q, want the marked message", got)
	}
}

// TestFlushIsSilentInSteadyState is the load-bearing guard. The cluster requeues
// every ten seconds across fourteen kinds, so a cycle with nothing new to say
// had better say nothing at all.
func TestFlushIsSilentInSteadyState(t *testing.T) {
	bucket := newBucket("cr")
	tracker, k8s, cbClient := newTestTracker(bucket)

	// The first pass establishes the condition.
	tracker.flushObject(t.Context(), k8s, testLog, bucketAdapter(t), bucket)

	var statusUpdates int

	countStatusUpdates(cbClient, &statusUpdates)

	// The second and third passes have nothing new to say.
	tracker.flushObject(t.Context(), k8s, testLog, bucketAdapter(t), bucket)
	tracker.flushObject(t.Context(), k8s, testLog, bucketAdapter(t), bucket)

	if statusUpdates != 0 {
		t.Errorf("steady-state cycles issued %d writes, want 0", statusUpdates)
	}
}

// TestNeedsWriteRejectsSettledResources is what keeps the flush cheap. A
// namespace can hold hundreds of these, the cluster requeues every ten seconds,
// and the adapter copies only what this predicate accepts. If it ever starts
// accepting settled resources, every cycle deep copies the whole namespace
// again.
func TestNeedsWriteRejectsSettledResources(t *testing.T) {
	tracker, _, _ := newTestTracker()

	needsWrite := tracker.needsStatusUpdate(couchbasev2.BucketCRDResourceKind)

	// Never judged before, so the condition has to be established.
	fresh := newBucket("fresh")
	if !needsWrite(fresh) {
		t.Error("a bucket with no condition at all does need a write")
	}

	// Settle it, exactly as the flush would.
	tracker.flushObject(t.Context(), &client.Client{CouchbaseClient: cbfake.NewSimpleClientset(fresh)},
		testLog, bucketAdapter(t), fresh)

	if needsWrite(fresh) {
		t.Error("a bucket already carrying this cluster's verdict must not be copied again")
	}

	// A generation bump means the verdict is stale, even though it is unchanged.
	bumped := fresh.DeepCopy()
	bumped.Generation++

	if !needsWrite(bumped) {
		t.Error("a bucket whose generation moved on needs its observedGeneration refreshed")
	}

	// And so does one this cycle has just marked.
	tracker.Mark(bucketRef("fresh"), ReasonValidationFailed, "memoryQuota is too small")

	if !needsWrite(fresh) {
		t.Error("a newly marked bucket needs its True condition written")
	}
}

// TestFlushToleratesMissingSubresource covers the Operator being upgraded ahead
// of the CRDs. The write comes back 404, which we log and shrug off, and we
// never fall back to writing the main resource.
func TestFlushToleratesMissingSubresource(t *testing.T) {
	bucket := newBucket("cr")
	tracker, k8s, cbClient := newTestTracker(bucket)

	var mainResourceWrites int

	cbClient.PrependReactor("update", "couchbasebuckets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() == "status" {
			return true, nil, apierrors.NewNotFound(
				schema.GroupResource{Group: couchbasev2.GroupName, Resource: couchbasev2.BucketCRDResourcePlural}, "cr")
		}

		mainResourceWrites++

		return false, nil, nil
	})

	cbClient.PrependReactor("patch", "couchbasebuckets", func(k8stesting.Action) (bool, runtime.Object, error) {
		mainResourceWrites++

		return false, nil, nil
	})

	tracker.Mark(bucketRef("cr"), ReasonValidationFailed, "bad")

	// This must neither panic nor escalate.
	tracker.flushObject(t.Context(), k8s, testLog, bucketAdapter(t), bucket)

	if mainResourceWrites != 0 {
		t.Errorf("fell back to %d writes against the main resource, want 0", mainResourceWrites)
	}
}

// TestFlushToleratesForbidden covers the subresource existing while RBAC does
// not. Same contract as above. Log it, do not fall back, do not fail.
func TestFlushToleratesForbidden(t *testing.T) {
	bucket := newBucket("cr")
	tracker, k8s, cbClient := newTestTracker(bucket)

	var mainResourceWrites int

	cbClient.PrependReactor("update", "couchbasebuckets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() == "status" {
			return true, nil, apierrors.NewForbidden(
				schema.GroupResource{Group: couchbasev2.GroupName, Resource: couchbasev2.BucketCRDResourcePlural}, "cr", nil)
		}

		mainResourceWrites++

		return false, nil, nil
	})

	tracker.Mark(bucketRef("cr"), ReasonValidationFailed, "bad")

	tracker.flushObject(t.Context(), k8s, testLog, bucketAdapter(t), bucket)

	if mainResourceWrites != 0 {
		t.Errorf("fell back to %d writes against the main resource, want 0", mainResourceWrites)
	}
}

// TestFlushClearsWhenNoLongerMarked checks that a fixed resource heals itself.
// True becomes False with reason Validated, and the transition time moves
// because the status genuinely did.
func TestFlushClearsWhenNoLongerMarked(t *testing.T) {
	stale := metav1.Condition{
		Type:               couchbasev2.ConditionTypeUnreconcilable,
		Status:             metav1.ConditionTrue,
		Reason:             string(ReasonValidationFailed),
		Message:            couchbasev2.UnreconcilableMessage(testClusterName, "was broken"),
		LastTransitionTime: metav1.NewTime(time.Unix(1, 0).UTC()),
		ObservedGeneration: 2,
	}

	bucket := newBucket("cr", stale)
	tracker, k8s, cbClient := newTestTracker(bucket)

	// Nothing is marked this time, because the spec was fixed.
	tracker.flushObject(t.Context(), k8s, testLog, bucketAdapter(t), bucket)

	stored, err := cbClient.CouchbaseV2().CouchbaseBuckets(testNamespace).Get(t.Context(), "cr", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to read back the bucket: %v", err)
	}

	condition, found := couchbasev2.GetUnreconcilable(stored, testClusterName)
	if !found {
		t.Fatal("the condition was removed, but it must be written False so kubectl wait can match it")
	}

	if condition.Status != metav1.ConditionFalse {
		t.Errorf("status = %q, want False", condition.Status)
	}

	if condition.Reason != string(ReasonValidated) {
		t.Errorf("reason = %q, want %q", condition.Reason, ReasonValidated)
	}

	if condition.LastTransitionTime.Equal(&stale.LastTransitionTime) {
		t.Error("status changed but LastTransitionTime did not move")
	}
}

// TestFlushKeepsTransitionTimeWhenOnlyMessageChanges is the other half of the
// transition contract. A resource that stays True but gains a new message keeps
// the transition time it already had.
func TestFlushKeepsTransitionTimeWhenOnlyMessageChanges(t *testing.T) {
	original := metav1.NewTime(time.Unix(1, 0).UTC())
	bucket := newBucket("cr", metav1.Condition{
		Type:               couchbasev2.ConditionTypeUnreconcilable,
		Status:             metav1.ConditionTrue,
		Reason:             string(ReasonValidationFailed),
		Message:            couchbasev2.UnreconcilableMessage(testClusterName, "first complaint"),
		LastTransitionTime: original,
		ObservedGeneration: 3,
	})

	tracker, k8s, cbClient := newTestTracker(bucket)

	tracker.Mark(bucketRef("cr"), ReasonValidationFailed, "a different complaint")

	tracker.flushObject(t.Context(), k8s, testLog, bucketAdapter(t), bucket)

	stored, err := cbClient.CouchbaseV2().CouchbaseBuckets(testNamespace).Get(t.Context(), "cr", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to read back the bucket: %v", err)
	}

	condition, _ := couchbasev2.GetUnreconcilable(stored, testClusterName)

	if !condition.LastTransitionTime.Equal(&original) {
		t.Errorf("LastTransitionTime moved to %v on a message-only change, want %v", condition.LastTransitionTime, original)
	}

	if got := couchbasev2.UnreconcilableDetail(condition); got != "a different complaint" {
		t.Errorf("detail = %q, want the new message", got)
	}
}

// TestFlushLeavesOtherClustersEntriesAlone is the multi-cluster guarantee. One
// bucket selected by two clusters carries two independent verdicts, and because
// the list is atomic this cluster rewrites the whole thing. It had better hand
// the other cluster's entry back untouched.
func TestFlushLeavesOtherClustersEntriesAlone(t *testing.T) {
	const otherCluster = "other-cluster"

	otherEntry := metav1.Condition{
		Type:               couchbasev2.ConditionTypeUnreconcilable,
		Status:             metav1.ConditionTrue,
		Reason:             string(ReasonImmutableFieldChanged),
		Message:            couchbasev2.UnreconcilableMessage(otherCluster, "the other cluster's complaint"),
		LastTransitionTime: metav1.NewTime(time.Unix(1, 0).UTC()),
		ObservedGeneration: 1,
	}

	bucket := newBucket("cr", otherEntry)
	tracker, k8s, cbClient := newTestTracker(bucket)

	tracker.Mark(bucketRef("cr"), ReasonValidationFailed, "this cluster's complaint")

	tracker.flushObject(t.Context(), k8s, testLog, bucketAdapter(t), bucket)

	stored, err := cbClient.CouchbaseV2().CouchbaseBuckets(testNamespace).Get(t.Context(), "cr", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to read back the bucket: %v", err)
	}

	if len(stored.Status.Conditions) != 2 {
		t.Fatalf("conditions = %d, want one per cluster", len(stored.Status.Conditions))
	}

	survivor, found := couchbasev2.GetUnreconcilable(stored, otherCluster)
	if !found {
		t.Fatal("the other cluster's entry was lost")
	}

	if *survivor != otherEntry {
		t.Errorf("the other cluster's entry changed:\n got %+v\nwant %+v", *survivor, otherEntry)
	}

	mine, found := couchbasev2.GetUnreconcilable(stored, testClusterName)
	if !found {
		t.Fatal("this cluster's entry was not written")
	}

	if got := couchbasev2.UnreconcilableDetail(mine); got != "this cluster's complaint" {
		t.Errorf("detail = %q, want this cluster's message", got)
	}
}

// TestLogRateLimiting checks that a partial upgrade does not turn into one log
// line per resource per requeue.
func TestLogRateLimiting(t *testing.T) {
	tracker, _, _ := newTestTracker()

	key := couchbasev2.BucketCRDResourceKind + "/NotFound"

	tracker.logRateLimited(testLog, couchbasev2.BucketCRDResourceKind, "NotFound", apierrors.NewNotFound(schema.GroupResource{}, "a"), "first")

	first, seen := tracker.lastLogged[key]
	if !seen {
		t.Fatal("the first occurrence was not recorded")
	}

	tracker.logRateLimited(testLog, couchbasev2.BucketCRDResourceKind, "NotFound", apierrors.NewNotFound(schema.GroupResource{}, "b"), "second")

	if tracker.lastLogged[key] != first {
		t.Error("a repeat within the interval reset the rate limiter")
	}

	// A different cause is a different key, and gets logged in its own right.
	tracker.logRateLimited(testLog, couchbasev2.BucketCRDResourceKind, "Forbidden", apierrors.NewNotFound(schema.GroupResource{}, "c"), "other")

	if _, seen := tracker.lastLogged[couchbasev2.BucketCRDResourceKind+"/Forbidden"]; !seen {
		t.Error("a distinct cause was suppressed by another cause's rate limit")
	}
}

// TestAdaptersCoverEveryInScopeKind guards the dispatch table against someone
// adding a kind to the API and forgetting all about this file.
func TestAdaptersCoverEveryInScopeKind(t *testing.T) {
	want := []string{
		couchbasev2.BucketCRDResourceKind,
		couchbasev2.EphemeralBucketCRDResourceKind,
		couchbasev2.MemcachedBucketCRDResourceKind,
		couchbasev2.ScopeCRDResourceKind,
		couchbasev2.ScopeGroupCRDResourceKind,
		couchbasev2.CollectionCRDResourceKind,
		couchbasev2.CollectionGroupCRDResourceKind,
		couchbasev2.UserCRDResourceKind,
		couchbasev2.GroupCRDResourceKind,
		couchbasev2.ReplicationCRDResourceKind,
		couchbasev2.MigrationReplicationCRDResourceKind,
		couchbasev2.BackupCRDResourceKind,
		couchbasev2.BackupRestoreCRDResourceKind,
		couchbasev2.AutoscalerCRDResourceKind,
	}

	byKind := map[string]adapter{}

	for _, candidate := range registry {
		if _, duplicate := byKind[candidate.kind]; duplicate {
			t.Errorf("duplicate adapter for kind %q", candidate.kind)
		}

		byKind[candidate.kind] = candidate
	}

	if len(byKind) != len(want) {
		t.Fatalf("registry has %d adapters, want %d", len(byKind), len(want))
	}

	for _, kind := range want {
		found, ok := byKind[kind]
		if !ok {
			t.Errorf("no adapter for kind %q", kind)
			continue
		}

		if found.list == nil || found.updateStatus == nil {
			t.Errorf("adapter for %q is incomplete", kind)
		}
	}

	// These two are out of scope by decision, because neither one is validated
	// in the reconcile loop.
	for _, kind := range []string{couchbasev2.RoleBindingCRDResourceKind, couchbasev2.EncryptionKeyCRDResourceKind} {
		if _, ok := byKind[kind]; ok {
			t.Errorf("kind %q is out of scope but has an adapter", kind)
		}
	}
}

// TestUpdateStatusRejectsWrongKind covers the assertion inside adapterFor.
func TestUpdateStatusRejectsWrongKind(t *testing.T) {
	_, k8s, _ := newTestTracker()

	user := &couchbasev2.CouchbaseUser{ObjectMeta: metav1.ObjectMeta{Name: "dave", Namespace: testNamespace}}

	if err := bucketAdapter(t).updateStatus(t.Context(), k8s, user); err == nil {
		t.Error("the bucket adapter accepted a CouchbaseUser")
	}
}
