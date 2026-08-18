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
	"fmt"
	"strings"
	"sync"
	"testing"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
)

const testClusterName = "cb-example"

func bucketRef(name string) Ref {
	return Ref{Kind: couchbasev2.BucketCRDResourceKind, Name: name}
}

// TestBeginCycleClears covers the guarantee the whole design rests on. No
// judgement outlives a cycle, which means none outlives a restart either, and a
// fixed spec heals itself.
func TestBeginCycleClears(t *testing.T) {
	tracker := New(testClusterName)

	ref := bucketRef("cr")
	tracker.SetBucketRef("b", ref)
	tracker.Mark(ref, ReasonValidationFailed, "bad")

	if !tracker.IsSkipped(ref.Kind, ref.Name) {
		t.Fatal("resource should be skipped in the cycle it was marked in")
	}

	tracker.BeginCycle()

	if tracker.IsSkipped(ref.Kind, ref.Name) {
		t.Error("skip survived BeginCycle")
	}

	if _, found := tracker.Entry(ref); found {
		t.Error("entry survived BeginCycle")
	}

	if _, found := tracker.BucketRef("b"); found {
		t.Error("bucket ref index survived BeginCycle")
	}
}

// TestMarkBucketRegistersBothNames covers the dual-identity trap. A bucket
// whose spec.name differs from its metadata.name gets asked about under both
// names, so marking only one of them leaves the bucket skipped at some call
// sites and merrily reconciled at others.
func TestMarkBucketRegistersBothNames(t *testing.T) {
	tracker := New(testClusterName)

	const (
		crName     = "cr"
		bucketName = "b"
	)

	ref := bucketRef(crName)
	tracker.SetBucketRef(bucketName, ref)
	tracker.MarkBucket(bucketName, ReasonValidationFailed, "memoryQuota is too small")

	if !tracker.IsSkipped(couchbasev2.BucketCRDResourceKind, crName) {
		t.Error("bucket not skipped under its CR name")
	}

	if !tracker.IsSkipped(KindBucketName, bucketName) {
		t.Error("bucket not skipped under its Couchbase bucket name")
	}

	// There is one real resource here, so there is one condition to write, and
	// it belongs on the CR rather than on the alias.
	if _, found := tracker.Entry(ref); !found {
		t.Error("no condition recorded against the bucket's CR")
	}

	if _, found := tracker.Entry(Ref{Kind: KindBucketName, Name: bucketName}); found {
		t.Error("the bucket-name alias must not carry a condition of its own")
	}
}

// TestMarkBucketWithoutRefStillSkips is the fail-safe. Even with no CR to hang
// a condition off, control flow still has to suppress the bucket.
func TestMarkBucketWithoutRefStillSkips(t *testing.T) {
	tracker := New(testClusterName)

	tracker.MarkBucket("orphan", ReasonValidationFailed, "no CR for this bucket")

	if !tracker.IsSkipped(KindBucketName, "orphan") {
		t.Error("a bucket with no known CR must still be skipped")
	}
}

// TestDoubleMarkReportsBothFailures checks that a resource unlucky enough to
// fail two validators reports both complaints, rather than one message quietly
// overwriting the other.
func TestDoubleMarkReportsBothFailures(t *testing.T) {
	tracker := New(testClusterName)

	ref := bucketRef("cr")
	tracker.Mark(ref, ReasonValidationFailed, "first complaint")
	tracker.Mark(ref, ReasonImmutableFieldChanged, "second complaint")

	entry, found := tracker.Entry(ref)
	if !found {
		t.Fatal("no entry recorded")
	}

	for _, want := range []string{"first complaint", "second complaint"} {
		if !strings.Contains(entry.Message, want) {
			t.Errorf("message %q does not report %q", entry.Message, want)
		}
	}

	// The reason field holds a single value, so the validator that first caused
	// the skip is the one we report.
	if entry.Reason != ReasonValidationFailed {
		t.Errorf("reason = %q, want the first reason %q", entry.Reason, ReasonValidationFailed)
	}
}

// TestResourceInvalidMidLifetimeIsRemarked guards the behaviour change at the
// heart of all this.
//
// Constraint validation used to run once per Operator lifetime, and its skips
// only stuck around because the annotation was sticky. Now that the tracker is
// thrown away every cycle, a resource that goes bad after adoption has to be
// re-marked by the validation on the next cycle. The happy consequence is that
// a fixed resource comes unmarked by exactly the same mechanism, with nobody
// having to clean anything up.
func TestResourceInvalidMidLifetimeIsRemarked(t *testing.T) {
	tracker := New(testClusterName)

	ref := Ref{Kind: couchbasev2.UserCRDResourceKind, Name: "dave"}

	// One cycle's worth of validation, parameterised on whether the spec is good.
	cycle := func(specValid bool) {
		tracker.BeginCycle()

		if !specValid {
			tracker.Mark(ref, ReasonValidationFailed, "roles is required")
		}
	}

	cycle(true)

	if tracker.IsSkipped(ref.Kind, ref.Name) {
		t.Fatal("a valid resource must not be skipped")
	}

	cycle(false)

	if !tracker.IsSkipped(ref.Kind, ref.Name) {
		t.Error("a resource that goes invalid mid-lifetime must be skipped on the next cycle")
	}

	cycle(true)

	if tracker.IsSkipped(ref.Kind, ref.Name) {
		t.Error("a fixed resource must stop being skipped without any explicit clear")
	}
}

// TestConcurrentMarkAndRead leans on the mutex under -race, because buckets get
// gathered from the reconcile goroutine while the validation goroutine is busy
// marking them.
func TestConcurrentMarkAndRead(t *testing.T) {
	tracker := New(testClusterName)

	const goroutines = 16

	var wg sync.WaitGroup

	for i := range goroutines {
		wg.Add(3)

		name := fmt.Sprintf("cr-%d", i)
		bucket := fmt.Sprintf("bucket-%d", i)

		go func() {
			defer wg.Done()
			tracker.SetBucketRef(bucket, bucketRef(name))
		}()

		go func() {
			defer wg.Done()
			tracker.MarkBucket(bucket, ReasonValidationFailed, "concurrent")
		}()

		go func() {
			defer wg.Done()
			tracker.IsSkipped(couchbasev2.BucketCRDResourceKind, name)
		}()
	}

	wg.Wait()

	// Every bucket ends up suppressed under its Couchbase name, whether or not
	// its ref landed before the mark did.
	for i := range goroutines {
		bucket := fmt.Sprintf("bucket-%d", i)
		if !tracker.IsSkipped(KindBucketName, bucket) {
			t.Errorf("%s was not skipped", bucket)
		}
	}
}
