/*
Copyright 2020-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2e

import (
	"testing"
	"time"

	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
)

// TestCouchbaseMetricsDocumentCount checks that we can query Server 7.0's prometheus endpoints for document counts.
func TestCouchbaseMetricsDocumentCount(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.0")

	// Static configuration.
	clusterSize := 1
	scopeName := "pinky"
	collectionName := "brain"

	// Create scope & collection in a bucket.
	collection := e2eutil.NewCollection(collectionName).MustCreate(t, kubernetes)
	scope := e2eutil.NewScope(scopeName).WithCollections(collection).MustCreate(t, kubernetes)
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.LinkBucketToScopesExplicit(bucket, scope)
	bucket = e2eutil.MustNewBucket(t, kubernetes, bucket)

	// Create cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Check scope & collection have been created.
	expected := e2eutil.NewExpectedScopesAndCollections().WithIgnoreSystemScope().WithDefaultScopeAndCollection()
	expected.WithScope(scopeName).WithCollections(collectionName)
	e2eutil.MustWaitForScopesAndCollections(t, kubernetes, cluster, bucket, expected, time.Minute)

	// Create documents; one set in the default collection, and another in the created one.
	e2eutil.NewDocumentSet(bucket.GetName(), f.DocsCount).MustCreate(t, kubernetes, cluster)
	e2eutil.NewDocumentSet(bucket.GetName(), f.DocsCount).IntoScopeAndCollection(scopeName, collectionName).MustCreate(t, kubernetes, cluster)

	// Check document count - the former should get the count from both the default and custom collections using the /pools/default/buckets,
	// while the latter should just get the count from the created collection using the prometheus endpoints.
	e2eutil.MustVerifyDocCountInBucket(t, kubernetes, cluster, bucket.GetName(), 2*f.DocsCount, time.Minute)
	e2eutil.MustVerifyDocCountInCollection(t, kubernetes, cluster, bucket.GetName(), scopeName, collectionName, f.DocsCount, time.Minute)
}
