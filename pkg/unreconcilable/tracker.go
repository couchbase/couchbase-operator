/*
Copyright 2026-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

// Package unreconcilable decides, and then reports, whether the Operator
// considers a dependent resource unreconcilable and is therefore leaving it
// well alone.
//
// Skip decisions live in memory and are rebuilt every reconcile cycle, so
// nothing in the reconcile path ever reads persisted state back to decide a
// skip. They are then projected onto each resource's Unreconcilable status
// condition, which is purely advisory. If every status write in the world
// failed, reconcile behaviour would carry on exactly as before.
package unreconcilable

import (
	"context"
	"fmt"
	"sync"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/client"

	"github.com/go-logr/logr"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Reason is the CamelCase reason recorded on an Unreconcilable condition.
type Reason string

const (
	// ReasonValidationFailed means the spec failed constraint or
	// change-constraint validation.
	ReasonValidationFailed Reason = "ValidationFailed"

	// ReasonImmutableFieldChanged means a field that cannot be changed after
	// creation was modified.
	ReasonImmutableFieldChanged Reason = "ImmutableFieldChanged"

	// ReasonDependencyMissing means something the resource refers to no longer
	// exists.
	ReasonDependencyMissing Reason = "DependencyMissing"

	// ReasonValidated is the reason on the False condition.
	ReasonValidated Reason = "Validated"
)

// KindBucketName is a pseudo-kind for the Couchbase bucket-name key space.
//
// A bucket gets asked about under two different names. Call sites holding a CR
// ask by metadata.name. Call sites holding a couchbaseutil.Bucket only know the
// Couchbase-side name, so they ask by spec.name. Whenever spec.name is set,
// those two names disagree, and a bucket marked under one of them needs to stay
// marked under the other.
const KindBucketName = "couchbase.com/bucketName"

const (
	// validatedDetail is the message detail on the False condition, which
	// metav1.Condition requires to be non-empty.
	validatedDetail = "resource validated"

	// logInterval is how often a repeating status-write failure is logged after
	// the first occurrence.
	logInterval = 5 * time.Minute
)

// Ref identifies a resource, both as the thing carrying a status condition and
// as the thing a reconcile call site asks about.
//
// There is no namespace here, because every dependent resource lives in the
// owning CouchbaseCluster's namespace. A kind and a name are enough.
type Ref struct {
	// Kind is an API kind, or the KindBucketName pseudo-kind.
	Kind string

	// Name is metadata.name, or the Couchbase bucket name for KindBucketName.
	Name string
}

// Entry is what to report on a marked resource's status condition.
type Entry struct {
	Reason  Reason
	Message string
}

// Tracker remembers which resources one CouchbaseCluster considers
// unreconcilable during a reconcile cycle, and writes those judgements out to
// their status conditions.
type Tracker struct {
	mu sync.RWMutex

	clusterName string
	entries     map[Ref]Entry
	skipped     map[Ref]struct{}
	bucketRefs  map[string]Ref

	// lastLogged rate-limits repeated status-write failures by kind and cause.
	// It is the only field that outlives a cycle.
	lastLogged map[string]time.Time

	// now is overridden by tests.
	now func() metav1.Time
}

// New returns an empty tracker owned by the named CouchbaseCluster.
func New(clusterName string) *Tracker {
	tracker := &Tracker{
		clusterName: clusterName,
		lastLogged:  map[string]time.Time{},
		now:         metav1.Now,
	}

	tracker.BeginCycle()

	return tracker
}

// ClusterName returns the CouchbaseCluster this tracker belongs to.
func (t *Tracker) ClusterName() string {
	return t.clusterName
}

// BeginCycle throws away every judgement from the previous cycle. It is called
// once per reconcile, before validation runs.
func (t *Tracker) BeginCycle() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.entries = map[Ref]Entry{}
	t.skipped = map[Ref]struct{}{}
	t.bucketRefs = map[string]Ref{}
}

// Mark records that a resource is unreconcilable and suppresses it for the rest
// of the cycle. Marking the same resource twice keeps the first reason and
// appends the second message, so a resource that upsets two validators gets to
// report both complaints.
func (t *Tracker) Mark(ref Ref, reason Reason, message string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if existing, found := t.entries[ref]; found {
		existing.Message = fmt.Sprintf("%s; %s", existing.Message, message)
		t.entries[ref] = existing
	} else {
		t.entries[ref] = Entry{Reason: reason, Message: message}
	}

	t.skipped[ref] = struct{}{}
}

// MarkBucket marks a bucket under both of the names it is asked about,
// resolving the CR from the index built during gather.
func (t *Tracker) MarkBucket(bucketName string, reason Reason, message string) {
	if ref, found := t.BucketRef(bucketName); found {
		t.Mark(ref, reason, message)
	}

	// With no CR there is nothing to report against, but the skip still has to
	// take effect.
	t.mu.Lock()
	defer t.mu.Unlock()

	t.skipped[Ref{Kind: KindBucketName, Name: bucketName}] = struct{}{}
}

// IsSkipped reports whether a resource is suppressed for this cycle.
func (t *Tracker) IsSkipped(kind, name string) bool {
	// Unit tests build bare Cluster literals that have no tracker at all.
	if t == nil {
		return false
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	_, skipped := t.skipped[Ref{Kind: kind, Name: name}]

	return skipped
}

// Entry returns what to report against a resource, if it was marked.
func (t *Tracker) Entry(ref Ref) (Entry, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	entry, found := t.entries[ref]

	return entry, found
}

// SetBucketRef records which CR declares a Couchbase bucket name.
func (t *Tracker) SetBucketRef(bucketName string, ref Ref) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.bucketRefs[bucketName] = ref
}

// BucketRef returns the CR that declares a Couchbase bucket name.
func (t *Tracker) BucketRef(bucketName string) (Ref, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	ref, found := t.bucketRefs[bucketName]

	return ref, found
}

// verdict returns the condition this cluster wants on a resource this cycle.
func (t *Tracker) verdict(kind, name string) (metav1.ConditionStatus, Reason, string) {
	if entry, marked := t.Entry(Ref{Kind: kind, Name: name}); marked {
		return metav1.ConditionTrue, entry.Reason, entry.Message
	}

	return metav1.ConditionFalse, ReasonValidated, validatedDetail
}

// needsStatusUpdateFunc reports whether a resource is missing the condition the flush
// wants on it.
type needsStatusUpdateFunc func(object couchbasev2.UnreconcilableAware) bool

// needsStatusUpdate returns a predicate reporting whether a resource of this kind is
// missing the verdict this cycle reached.
func (t *Tracker) needsStatusUpdate(kind string) needsStatusUpdateFunc {
	return func(object couchbasev2.UnreconcilableAware) bool {
		status, reason, detail := t.verdict(kind, object.GetName())

		return !couchbasev2.IsUnreconcilableConditionUpToDate(object, t.clusterName, status, string(reason), detail,
			object.GetGeneration())
	}
}

// Flush brings every in-scope resource's Unreconcilable condition into line
// with the tracker. Marked resources get True and the recorded reason, and
// anything this cluster has no complaint about gets False. Failures are logged
// and never returned, because reporting must not break reconciling.
func (t *Tracker) Flush(ctx context.Context, k8s *client.Client, log logr.Logger) {
	for _, kindAdapter := range registry {
		for _, object := range kindAdapter.list(k8s, t.needsStatusUpdate(kindAdapter.kind)) {
			t.flushObject(ctx, k8s, log, kindAdapter, object)
		}
	}
}

// flushObject brings one resource's condition up to date.
func (t *Tracker) flushObject(ctx context.Context, k8s *client.Client, log logr.Logger,
	kindAdapter adapter, object couchbasev2.UnreconcilableAware,
) {
	status, reason, detail := t.verdict(kindAdapter.kind, object.GetName())

	if !couchbasev2.SetUnreconcilable(object, t.clusterName, status, string(reason), detail,
		object.GetGeneration(), t.now()) {
		return
	}

	err := kindAdapter.updateStatus(ctx, k8s, object)

	switch {
	case err == nil:
	case k8serrors.IsNotFound(err):
		// The CRD has no status subresource, so the Operator is running ahead
		// of the CRDs. Patching the main resource instead would be worse than
		// staying quiet, because the admission controller does intercept that,
		// and these are by definition resources that just failed validation.
		t.logRateLimited(log, kindAdapter.kind, "NotFound", err,
			"Cannot write status condition: the CRD has no status subresource. Apply the current CRDs.")
	case k8serrors.IsForbidden(err):
		t.logRateLimited(log, kindAdapter.kind, "Forbidden", err,
			fmt.Sprintf("Cannot write status condition: the Operator's role is missing update on %s/status.", kindAdapter.kind))
	default:
		t.logRateLimited(log, kindAdapter.kind, "Error", err, "Failed to write status condition.")
	}
}

// logRateLimited logs the first occurrence of a given kind and cause, and after
// that at most one every logInterval, so a partial upgrade cannot turn into a
// log line per resource per requeue.
func (t *Tracker) logRateLimited(log logr.Logger, kind, cause string, err error, message string) {
	key := fmt.Sprintf("%s/%s", kind, cause)

	t.mu.Lock()
	last, seen := t.lastLogged[key]
	suppress := seen && time.Since(last) < logInterval

	if !suppress {
		t.lastLogged[key] = time.Now()
	}
	t.mu.Unlock()

	if !suppress {
		log.Error(err, message, "kind", kind)
	}
}
