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
	"context"
	"errors"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/client"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// errWrongKind is returned when an adapter is handed an object of another kind.
var errWrongKind = errors.New("object is not of the kind expected by this adapter")

// adapter is everything the flush needs to know about one API kind: how to
// enumerate its resources, and how to write one back.
type adapter struct {
	kind string

	// list returns a deep copy of every cached resource of this kind which needs status update
	list func(k8s *client.Client, needsWrite needsStatusUpdateFunc) []couchbasev2.UnreconcilableAware

	// updateStatus writes the resource through its /status subresource.
	updateStatus func(ctx context.Context, k8s *client.Client, object couchbasev2.UnreconcilableAware) error
}

// registry is the dispatch table the flush walks, one entry per API kind that
// carries an Unreconcilable condition.
//
// The flush needs to do the same handful of things to fourteen kinds that Go's
// type system considers completely unrelated, and two of those things simply
// cannot be written generically:
//
//   - Enumerating resources. The flush visits every resource of every kind, not
//     just the ones the tracker marked, because it is the unmarked ones that
//     earn the False condition. Only the typed informer caches can produce that
//     list, and each of them is a distinct type with its own List method.
//
//   - Writing one back. client-gen emits a separate client interface per kind,
//     so CouchbaseBuckets(ns).UpdateStatus and CouchbaseUsers(ns).UpdateStatus
//     have no shared supertype to call through. Each entry closes over the
//     right one. Each also pins the write to the /status subresource, which the
//     admission controller leaves alone, so a status write can never be
//     rejected by the very validation that produced the condition.
//
// In short, couchbasev2.UnreconcilableAware gives the flush a common object
// type, and the registry gives it a common client call. Spelling the kinds out
// by hand instead of reflecting over them is deliberate too. Add a kind to the
// API and forget it here, and it shows up as a missing entry that the tests
// will happily point at.
var registry = []adapter{
	adapterFor(couchbasev2.BucketCRDResourceKind,
		func(k8s *client.Client) []*couchbasev2.CouchbaseBucket { return k8s.CouchbaseBuckets.List() },
		func(ctx context.Context, k8s *client.Client, resource *couchbasev2.CouchbaseBucket) error {
			_, err := k8s.CouchbaseClient.CouchbaseV2().CouchbaseBuckets(resource.Namespace).
				UpdateStatus(ctx, resource, metav1.UpdateOptions{})

			return err
		}),
	adapterFor(couchbasev2.EphemeralBucketCRDResourceKind,
		func(k8s *client.Client) []*couchbasev2.CouchbaseEphemeralBucket {
			return k8s.CouchbaseEphemeralBuckets.List()
		},
		func(ctx context.Context, k8s *client.Client, resource *couchbasev2.CouchbaseEphemeralBucket) error {
			_, err := k8s.CouchbaseClient.CouchbaseV2().CouchbaseEphemeralBuckets(resource.Namespace).
				UpdateStatus(ctx, resource, metav1.UpdateOptions{})

			return err
		}),
	adapterFor(couchbasev2.MemcachedBucketCRDResourceKind,
		func(k8s *client.Client) []*couchbasev2.CouchbaseMemcachedBucket {
			return k8s.CouchbaseMemcachedBuckets.List()
		},
		func(ctx context.Context, k8s *client.Client, resource *couchbasev2.CouchbaseMemcachedBucket) error {
			_, err := k8s.CouchbaseClient.CouchbaseV2().CouchbaseMemcachedBuckets(resource.Namespace).
				UpdateStatus(ctx, resource, metav1.UpdateOptions{})

			return err
		}),
	adapterFor(couchbasev2.ScopeCRDResourceKind,
		func(k8s *client.Client) []*couchbasev2.CouchbaseScope { return k8s.CouchbaseScopes.List() },
		func(ctx context.Context, k8s *client.Client, resource *couchbasev2.CouchbaseScope) error {
			_, err := k8s.CouchbaseClient.CouchbaseV2().CouchbaseScopes(resource.Namespace).
				UpdateStatus(ctx, resource, metav1.UpdateOptions{})

			return err
		}),
	adapterFor(couchbasev2.ScopeGroupCRDResourceKind,
		func(k8s *client.Client) []*couchbasev2.CouchbaseScopeGroup { return k8s.CouchbaseScopeGroups.List() },
		func(ctx context.Context, k8s *client.Client, resource *couchbasev2.CouchbaseScopeGroup) error {
			_, err := k8s.CouchbaseClient.CouchbaseV2().CouchbaseScopeGroups(resource.Namespace).
				UpdateStatus(ctx, resource, metav1.UpdateOptions{})

			return err
		}),
	adapterFor(couchbasev2.CollectionCRDResourceKind,
		func(k8s *client.Client) []*couchbasev2.CouchbaseCollection { return k8s.CouchbaseCollections.List() },
		func(ctx context.Context, k8s *client.Client, resource *couchbasev2.CouchbaseCollection) error {
			_, err := k8s.CouchbaseClient.CouchbaseV2().CouchbaseCollections(resource.Namespace).
				UpdateStatus(ctx, resource, metav1.UpdateOptions{})

			return err
		}),
	adapterFor(couchbasev2.CollectionGroupCRDResourceKind,
		func(k8s *client.Client) []*couchbasev2.CouchbaseCollectionGroup {
			return k8s.CouchbaseCollectionGroups.List()
		},
		func(ctx context.Context, k8s *client.Client, resource *couchbasev2.CouchbaseCollectionGroup) error {
			_, err := k8s.CouchbaseClient.CouchbaseV2().CouchbaseCollectionGroups(resource.Namespace).
				UpdateStatus(ctx, resource, metav1.UpdateOptions{})

			return err
		}),
	adapterFor(couchbasev2.UserCRDResourceKind,
		func(k8s *client.Client) []*couchbasev2.CouchbaseUser { return k8s.CouchbaseUsers.List() },
		func(ctx context.Context, k8s *client.Client, resource *couchbasev2.CouchbaseUser) error {
			_, err := k8s.CouchbaseClient.CouchbaseV2().CouchbaseUsers(resource.Namespace).
				UpdateStatus(ctx, resource, metav1.UpdateOptions{})

			return err
		}),
	adapterFor(couchbasev2.GroupCRDResourceKind,
		func(k8s *client.Client) []*couchbasev2.CouchbaseGroup { return k8s.CouchbaseGroups.List() },
		func(ctx context.Context, k8s *client.Client, resource *couchbasev2.CouchbaseGroup) error {
			_, err := k8s.CouchbaseClient.CouchbaseV2().CouchbaseGroups(resource.Namespace).
				UpdateStatus(ctx, resource, metav1.UpdateOptions{})

			return err
		}),
	adapterFor(couchbasev2.ReplicationCRDResourceKind,
		func(k8s *client.Client) []*couchbasev2.CouchbaseReplication { return k8s.CouchbaseReplications.List() },
		func(ctx context.Context, k8s *client.Client, resource *couchbasev2.CouchbaseReplication) error {
			_, err := k8s.CouchbaseClient.CouchbaseV2().CouchbaseReplications(resource.Namespace).
				UpdateStatus(ctx, resource, metav1.UpdateOptions{})

			return err
		}),
	adapterFor(couchbasev2.MigrationReplicationCRDResourceKind,
		func(k8s *client.Client) []*couchbasev2.CouchbaseMigrationReplication {
			return k8s.CouchbaseMigrationReplications.List()
		},
		func(ctx context.Context, k8s *client.Client, resource *couchbasev2.CouchbaseMigrationReplication) error {
			_, err := k8s.CouchbaseClient.CouchbaseV2().CouchbaseMigrationReplications(resource.Namespace).
				UpdateStatus(ctx, resource, metav1.UpdateOptions{})

			return err
		}),
	adapterFor(couchbasev2.BackupCRDResourceKind,
		func(k8s *client.Client) []*couchbasev2.CouchbaseBackup { return k8s.CouchbaseBackups.List() },
		func(ctx context.Context, k8s *client.Client, resource *couchbasev2.CouchbaseBackup) error {
			_, err := k8s.CouchbaseClient.CouchbaseV2().CouchbaseBackups(resource.Namespace).
				UpdateStatus(ctx, resource, metav1.UpdateOptions{})

			return err
		}),
	adapterFor(couchbasev2.BackupRestoreCRDResourceKind,
		func(k8s *client.Client) []*couchbasev2.CouchbaseBackupRestore {
			return k8s.CouchbaseBackupRestores.List()
		},
		func(ctx context.Context, k8s *client.Client, resource *couchbasev2.CouchbaseBackupRestore) error {
			_, err := k8s.CouchbaseClient.CouchbaseV2().CouchbaseBackupRestores(resource.Namespace).
				UpdateStatus(ctx, resource, metav1.UpdateOptions{})

			return err
		}),
	adapterFor(couchbasev2.AutoscalerCRDResourceKind,
		func(k8s *client.Client) []*couchbasev2.CouchbaseAutoscaler { return k8s.CouchbaseAutoscalers.List() },
		func(ctx context.Context, k8s *client.Client, resource *couchbasev2.CouchbaseAutoscaler) error {
			_, err := k8s.CouchbaseClient.CouchbaseV2().CouchbaseAutoscalers(resource.Namespace).
				UpdateStatus(ctx, resource, metav1.UpdateOptions{})

			return err
		}),
}

// adapterFor builds the adapter for one kind out of that kind's typed cache
// accessor and typed status writer, so the filter, the deep copy and the type
// assertion get written once instead of fourteen times.
func adapterFor[T couchbasev2.UnreconcilableAware](
	kind string,
	list func(k8s *client.Client) []T,
	updateStatus func(ctx context.Context, k8s *client.Client, resource T) error,
) adapter {
	return adapter{
		kind: kind,
		list: func(k8s *client.Client, needsStatusUpdate needsStatusUpdateFunc) []couchbasev2.UnreconcilableAware {
			var aware []couchbasev2.UnreconcilableAware

			for _, resource := range list(k8s) {
				if !needsStatusUpdate(resource) {
					continue
				}

				// DeepCopyObject lives on the interface, so unlike the
				// generated DeepCopy it needs no per-kind plumbing.
				copied, ok := resource.DeepCopyObject().(couchbasev2.UnreconcilableAware)
				if !ok {
					continue
				}

				aware = append(aware, copied)
			}

			return aware
		},
		updateStatus: func(ctx context.Context, k8s *client.Client, object couchbasev2.UnreconcilableAware) error {
			resource, ok := object.(T)
			if !ok {
				return errWrongKind
			}

			return updateStatus(ctx, k8s, resource)
		},
	}
}
