/*
Copyright 2021-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2e

import (
	"fmt"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	v1 "k8s.io/api/core/v1"
)

var (
	// defaultLabelSelector is used to select scope and collection
	// resources for implicit referencing.
	defaultLabelSelector = map[string]string{
		"chicken": "boo",
	}
)

// TestScopeCreateExplicit tests scope selectors in buckets can reference
// scopes and scope groups explicitly.
func TestScopeCreateExplicit(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	clusterSize := 1
	scopeName := "pinky"
	scopeGroupName := "brain"
	scopeGroupNames := []string{"buttons", "mindy"}

	// Create a scope and scope group.
	scope := e2eutil.NewScope(scopeName).MustCreate(t, kubernetes)
	scopeGroup := e2eutil.NewScopeGroup(scopeGroupName, scopeGroupNames...).MustCreate(t, kubernetes)

	// Link to a bucket and create that.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope, scopeGroup)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Wait for all scopes to be created as expected.
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection()
	expected.WithScopes(scopeName, scopeGroupNames)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// Expect there to be a scopes & collections updated event.
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated, FuzzyMessage: bucket.GetName()},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestScopeCreateImplicit tests scope selectors in buckets can select
// scopes and scope groups with labels.
func TestScopeCreateImplicit(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	clusterSize := 1
	scopeName := "pinky"
	scopeGroupName := "brain"
	scopeGroupNames := []string{"buttons", "mindy"}

	// Create a scope and scope group.
	e2eutil.NewScope(scopeName).WithLabels(defaultLabelSelector).MustCreate(t, kubernetes)
	e2eutil.NewScopeGroup(scopeGroupName, scopeGroupNames...).WithLabels(defaultLabelSelector).MustCreate(t, kubernetes)

	// Link to a bucket and create that.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesImplicit(bucket, defaultLabelSelector)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Wait for all scopes to be created as expected.
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection()
	expected.WithScopes(scopeName, scopeGroupNames)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// Expect there to be a scopes & collections updated event.
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated, FuzzyMessage: bucket.GetName()},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestScopeCreateMixed tests explicit and implicit scope selection at
// the same time.
func TestScopeCreateMixed(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	clusterSize := 1
	scopeName := "pinky"
	scopeGroupName := "brain"
	scopeGroupNames := []string{"buttons", "mindy"}

	// Create a scope and scope group.
	scope := e2eutil.NewScope(scopeName).MustCreate(t, kubernetes)
	e2eutil.NewScopeGroup(scopeGroupName, scopeGroupNames...).WithLabels(defaultLabelSelector).MustCreate(t, kubernetes)

	// Link to a bucket and create that.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	e2eutil.LinkBucketToScopesImplicit(bucket, defaultLabelSelector)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Wait for all scopes to be created as expected.
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection()
	expected.WithScopes(scopeName, scopeGroupNames)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// Expect there to be a scopes & collections updated event.
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated, FuzzyMessage: bucket.GetName()},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestScopeDelete tests scopes are deleted when they are asked to do so,
// we know by now that the Operator can see the correct set, so there's no
// need to test this extensively.
func TestScopeDelete(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	clusterSize := 1
	scopeName := "pinky"

	// Create a scope and scope group.
	scope := e2eutil.NewScope(scopeName).MustCreate(t, kubernetes)

	// Link to a bucket and create that.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Wait for all scopes to be created as expected.
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection().WithScopes(scopeName)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// Delete the scope and wait for it to disappear.
	e2eutil.MustDeleteScope(t, kubernetes, scopeName)

	expected = e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection()
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// Expect there to be a scopes & collections updated event for creation
	// and one for deletion.
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Repeat{
			Times:     2,
			Validator: eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated, FuzzyMessage: bucket.GetName()},
		},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestScopeUnmanaged tests that when scopes are unmanaged for a bucket,
// the Operator doesn't go around deleting manually created scopes.
func TestScopeUnmanaged(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	clusterSize := 1
	scopeName := "pinky"

	// Create an unmanaged bucket.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	e2eutil.MustWaitUntilBucketExists(t, kubernetes, cluster, bucket, time.Minute)

	// Manually create a scope.
	e2eutil.MustCreateScopeManually(t, kubernetes, cluster, bucket.GetName(), scopeName)

	// Assert scopes are created as expected, and stay that way.
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection().WithScopes(scopeName)
	e2eutil.MustAssertScopesAndCollectionsFor(t, kubernetes, cluster, bucket, expected, time.Minute)

	// Expect there to be no scopes and collections updated event.
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestCollectionCreateExplicit tests collection selectors in scopes can
// reference collections and collection groups explicitly.
func TestCollectionCreateExplicit(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	clusterSize := 1
	scopeName := "pinky"
	collectionName := "brain"
	collectionGroupName := "group1"
	collectionGroupNames := []string{"buttons", "mindy"}

	// Create a collection and collection group.
	collection := e2eutil.NewCollection(collectionName).MustCreate(t, kubernetes)
	collectionGroup := e2eutil.NewCollectionGroup(collectionGroupName, collectionGroupNames...).MustCreate(t, kubernetes)

	// Create a scope.
	scope := e2eutil.NewScope(scopeName).WithCollections(collection, collectionGroup).MustCreate(t, kubernetes)

	// Link to a bucket and create that.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Wait for all scopes to be created as expected.
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollections(collectionName, collectionGroupNames)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// Expect there to be a scopes & collections updated event.
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated, FuzzyMessage: bucket.GetName()},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestCollectionCreateImplicit tests collection selectors in scopes can
// select collections and collection groups with labels.
func TestCollectionCreateImplicit(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	clusterSize := 1
	scopeName := "pinky"
	collectionName := "brain"
	collectionGroupName := "group1"
	collectionGroupNames := []string{"buttons", "mindy"}

	// Create a collection and collection group.
	e2eutil.NewCollection(collectionName).WithLabels(defaultLabelSelector).MustCreate(t, kubernetes)
	e2eutil.NewCollectionGroup(collectionGroupName, collectionGroupNames...).WithLabels(defaultLabelSelector).MustCreate(t, kubernetes)

	// Create a scope.
	scope := e2eutil.NewScope(scopeName).WithLabelSelector(defaultLabelSelector).MustCreate(t, kubernetes)

	// Link to a bucket and create that.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Wait for all scopes to be created as expected.
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollections(collectionName, collectionGroupNames)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// Expect there to be a scopes & collections updated event.
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated, FuzzyMessage: bucket.GetName()},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestCollectionCreateMixed tests explicit and implicit collection selection
// at the same time.
func TestCollectionCreateMixed(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	clusterSize := 1
	scopeName := "pinky"
	collectionName := "brain"
	collectionGroupName := "group1"
	collectionGroupNames := []string{"buttons", "mindy"}

	// Create a collection and collection group.
	collection := e2eutil.NewCollection(collectionName).MustCreate(t, kubernetes)
	e2eutil.NewCollectionGroup(collectionGroupName, collectionGroupNames...).WithLabels(defaultLabelSelector).MustCreate(t, kubernetes)

	// Create a scope.
	scope := e2eutil.NewScope(scopeName).WithCollections(collection).WithLabelSelector(defaultLabelSelector).MustCreate(t, kubernetes)

	// Link to a bucket and create that.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Wait for all scopes to be created as expected.
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollections(collectionName, collectionGroupNames)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// Expect there to be a scopes & collections updated event.
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated, FuzzyMessage: bucket.GetName()},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestCollectionDelete tests collections are deleted when they are asked to
// do so, we know by now that the Operator can see the correct set, so there's
// no need to test this extensively.
func TestCollectionDelete(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	clusterSize := 1
	scopeName := "pinky"
	collectionName := "brain"

	// Create a collection and collection group.
	collection := e2eutil.NewCollection(collectionName).MustCreate(t, kubernetes)

	// Create a scope.
	scope := e2eutil.NewScope(scopeName).WithCollections(collection).MustCreate(t, kubernetes)

	// Link to a bucket and create that.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Wait for all scopes to be created as expected.
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollection(collectionName)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// Delete the collection and wait for it to disappear.
	e2eutil.MustDeleteCollection(t, kubernetes, collectionName)

	expected = e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection().WithScope(scopeName)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// Expect there to be a scopes & collections updated event.
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Repeat{
			Times:     2,
			Validator: eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated, FuzzyMessage: bucket.GetName()},
		},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestCollectionUnmanaged tests that manually created collections are not deleted
// in a managed scope, where the collections are unmanaged.
func TestCollectionUnmanaged(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	clusterSize := 1
	scopeName := "pinky"
	collectionName := "brain"

	// Create a scope.
	scope := e2eutil.NewScope(scopeName).MustCreate(t, kubernetes)

	// Link to a bucket and create that.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Wait for the scope to appear.
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection().WithScope(scopeName)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// Manually create a collection.
	e2eutil.MustCreateCollectionManually(t, kubernetes, cluster, bucket.GetName(), scopeName, collectionName)

	// Assert scopes and collections are created as expected, and stay that way.
	expected = e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollection(collectionName)
	e2eutil.MustAssertScopesAndCollectionsFor(t, kubernetes, cluster, bucket, expected, time.Minute)

	// Expect there to be a scopes & collections updated event.
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated, FuzzyMessage: bucket.GetName()},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestDefaultCollectionDeletion tests that the default scope can be defined
// and understood by the Operator, and that the default collection can be
// deleted if not kept alive.
func TestDefaultCollectionDeletion(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	clusterSize := 1
	scopeName := "pinky"

	// Create a scope.
	scope := e2eutil.NewScope(scopeName).IsDefault().WithManagedCollections().MustCreate(t, kubernetes)

	// Link to a bucket and create that.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Wait for the default collection to be deleted.
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScope()
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// Expect there to be no scopes and collections updated event.
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated, FuzzyMessage: bucket.GetName()},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestScopesAndCollectionsSharedScopeTopology checks that you can replicate
// a tree of scopes and collections across multiple buckets.
func TestScopesAndCollectionsSharedScopeTopology(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	clusterSize := 1
	bucketName1 := "wakko"
	bucketName2 := "dot"
	scopeName := "pinky"
	collectionGroupName := "group1"
	collectionGroupNames := []string{"buttons", "mindy"}

	// Create a collection group.
	collectionGroup := e2eutil.NewCollectionGroup(collectionGroupName, collectionGroupNames...).MustCreate(t, kubernetes)

	// Create a scope.
	scope := e2eutil.NewScope(scopeName).WithCollections(collectionGroup).MustCreate(t, kubernetes)

	// Link to two buckets and create them.
	bucket1 := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	bucket1.SetName(bucketName1)
	e2eutil.LinkBucketToScopesExplicit(bucket1, scope)
	bucket1 = e2eutil.MustNewBucket(t, kubernetes, bucket1)

	bucket2 := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	bucket2.SetName(bucketName2)
	e2eutil.LinkBucketToScopesExplicit(bucket2, scope)
	bucket2 = e2eutil.MustNewBucket(t, kubernetes, bucket2)

	// Create the cluster.
	// TODO: for some reason the default buckets are unnecessarily huge and fill up
	// the data service with just one, needing horrid hacks like these.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.ClusterSettings.DataServiceMemQuota = e2espec.NewResourceQuantityGi(1)
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Wait for all scopes to be created as expected.
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollections(collectionGroupNames)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket1, expected, time.Minute)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket2, expected, time.Minute)

	// Expect there to be a scopes & collections updated event.
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Repeat{
			Times:     2,
			Validator: eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		},
		eventschema.Set{
			Validators: []eventschema.Validatable{
				eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated, FuzzyMessage: bucket1.GetName()},
				eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated, FuzzyMessage: bucket2.GetName()},
			},
		},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestScopesAndCollectionsSharedCollectionTopology checks that you can replicate
// a set of collections across multiple scopes.
func TestScopesAndCollectionsSharedCollectionTopology(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	clusterSize := 1
	scopeName1 := "pinky"
	scopeName2 := "brain"
	collectionGroupName := "group1"
	collectionGroupNames := []string{"buttons", "mindy"}

	// Create a collection and collection group.
	collectionGroup := e2eutil.NewCollectionGroup(collectionGroupName, collectionGroupNames...).MustCreate(t, kubernetes)

	// Create two scopes.
	scope1 := e2eutil.NewScope(scopeName1).WithCollections(collectionGroup).MustCreate(t, kubernetes)
	scope2 := e2eutil.NewScope(scopeName2).WithCollections(collectionGroup).MustCreate(t, kubernetes)

	// Link to a bucket and create that.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope1, scope2)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Wait for all scopes to be created as expected.
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName1).WithCollections(collectionGroupNames)
	expected.WithScope(scopeName2).WithCollections(collectionGroupNames)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// Expect there to be a scopes & collections updated event.
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated, FuzzyMessage: bucket.GetName()},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestScopesAndCollectionsCascadingScopeDeletion tests that the deletion of a scope with
// nested collections is handled gracefully.  As we're just deleting the scope, it also
// checks that dead links in the bucket are handled gracefully.
func TestScopesAndCollectionsCascadingScopeDeletion(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	clusterSize := 1
	scopeName := "pinky"
	collectionName := "brain"

	// Create the scope and collection.
	collection := e2eutil.NewCollection(collectionName).MustCreate(t, kubernetes)
	scope := e2eutil.NewScope(scopeName).WithCollections(collection).MustCreate(t, kubernetes)

	// Link to a bucket and create that.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Wait for all scopes to be created as expected.
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollections(collectionName)

	// Delete the scope and wait for the scopes and collections to be updated.
	e2eutil.MustDeleteScope(t, kubernetes, scopeName)

	expected = e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection()
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// Expect there to be a scopes & collections updated event for creation and deletion.
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
		eventschema.Repeat{
			Times:     2,
			Validator: eventschema.Event{Reason: k8sutil.EventScopesAndCollectionsUpdated, FuzzyMessage: bucket.GetName()},
		},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestScopeOverflow checks for the behaviour of Couchbase being consistent over time.
// The docs say 1000, reality says 1200.  This just checks our understanding is correct
// we're not lying in our docs, and there is at least a cluster condition raised as
// computing this in the DAC is too computationally intensive.
func TestScopeOverflow(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	clusterSize := 1
	scopeGroupName := "brain"
	scopeClusterLimit := 1200

	// Create a chunky scope group.
	scopeGroup := e2eutil.NewScopeGroupN(scopeGroupName, "pinky-", scopeClusterLimit).MustCreate(t, kubernetes)

	// Link to a bucket and create that.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scopeGroup)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// We should get a successful event at the limit.  This isn't fast, but it doesn't take
	// forever either.  The cluster will be "creating" while the scopes are created, so the
	// above call will block, and we'd have missed the event, just check that it has happened
	// in the past.
	e2eutil.MustObserveClusterEvent(t, kubernetes, cluster, k8sutil.ScopesAndCollectionsUpdated(cluster, bucket.GetName()), 10*time.Minute)

	// If we tip the balance, then we get an error... I love you lady, bye bye!
	scopeGroup.Spec.Names = append(scopeGroup.Spec.Names, "mindy")
	e2eutil.MustUpdateScopeGroup(t, kubernetes, scopeGroup)

	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionError, v1.ConditionTrue, cluster, time.Minute)
}

// TestCollectionOverflow checks for the behaviour of Couchbase being consistent over time.
// The docs say 1000, reality says 1200.  This just checks our understanding is correct
// we're not lying in our docs, and there is at least a cluster condition raised as
// computing this in the DAC is too computationally intensive.
func TestCollectionOverflow(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	clusterSize := 1
	scopeName := "pinky"
	collectionGroupName := "brain"
	collectionClusterLimit := 1200

	// Create a collection scope group.
	collectionGroup := e2eutil.NewCollectionGroupN(collectionGroupName, "buttons-", collectionClusterLimit).MustCreate(t, kubernetes)

	// Lin to a scope.
	scope := e2eutil.NewScope(scopeName).WithCollections(collectionGroup).MustCreate(t, kubernetes)

	// Link to a bucket and create that.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// We should get a successful event at the limit.  This isn't fast, but it doesn't take
	// forever either.  The cluster will be "creating" while the collections are created, so the
	// above call will block, and we'd have missed the event, just check that it has happened
	// in the past.
	e2eutil.MustObserveClusterEvent(t, kubernetes, cluster, k8sutil.ScopesAndCollectionsUpdated(cluster, bucket.GetName()), 10*time.Minute)

	// If we tip the balance, then we get an error... I love you lady, bye bye!
	collectionGroup.Spec.Names = append(collectionGroup.Spec.Names, "mindy")
	e2eutil.MustUpdateCollectionGroup(t, kubernetes, collectionGroup)

	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionError, v1.ConditionTrue, cluster, time.Minute)
}

func TestGSIWithCollections(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0").CouchbaseBucket()

	clusterSize := 1
	numOfDocs := f.DocsCount

	// Create a bucket.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Wait for all scopes to be created as expected.
	expected := e2eutil.NewExpectedScopesAndCollections().WithDefaultScopeAndCollection()
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// insert docs to backup
	e2eutil.NewDocumentSet(bucket.GetName(), numOfDocs).MustCreate(t, kubernetes, cluster)
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), numOfDocs, time.Minute)

	// Couchbase Cluster Instance.
	host, sdkCleanup := e2eutil.MustGetCouchbaseClientSDK(t, kubernetes, cluster)
	defer sdkCleanup()

	// Instance to manage Indexes.
	queryManager := host.QueryIndexes()

	// Create Secondary Index against default collection.
	e2eutil.MustCreateSecondaryIndex(t, queryManager, bucket.GetName())

	// Drop default collection.
	query := "DROP COLLECTION `default`._default._default;"

	// Run N1QL Query against Collection.
	e2eutil.MustExecuteN1qlQuery(t, host, query)

	// wait for Index to disappear.
	time.Sleep(30 * time.Second)

	indexes := e2eutil.MustGetAllIndexes(t, queryManager, bucket.GetName())

	// Check dropping collection dropped all Indexes as well.
	if len(indexes) != 0 {
		e2eutil.Die(t, fmt.Errorf("GSI %s against Collection was not dropped after dropping collection", indexes[0].Name))
	}

	// Expect there to be a scopes & collections updated event for creation and deletion.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}
