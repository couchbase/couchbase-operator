/*
Copyright 2017-Present Couchbase, Inc.

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

	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Tests creation of a 3 node cluster with 0 buckets
// 1. Create a 3 node Couchbase cluster with no buckets.
// 2. Check the events to make sure the operator took the correct actions.
// 3. Verifies that the cluster is balanced and all data is available.
func TestCreateCluster(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestCLIParametersCluster(t *testing.T) {
	// Copy the global framework so that we can control the parameters being passed to the operator
	// without modifying the values for other tests.
	globalFrameworkCopy := *framework.Global
	f := &globalFrameworkCopy

	f.PodReadinessDelay = 16 * time.Second
	f.PodReadinessPeriod = 26 * time.Second

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 1

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)

	// Check the Readiness probe matches the parameters we passed in
	cbPodName := couchbaseutil.CreateMemberName(cluster.Name, 0)
	pod, err := kubernetes.KubeClient.CoreV1().Pods(cluster.Namespace).Get(context.TODO(), cbPodName, metav1.GetOptions{})

	if err != nil {
		e2eutil.Die(t, fmt.Errorf("Pod not found"))
	}

	readinessProbe := pod.Spec.Containers[0].ReadinessProbe
	if podReadinessDelaySeconds := readinessProbe.InitialDelaySeconds; podReadinessDelaySeconds != int32(f.PodReadinessDelay.Seconds()) {
		e2eutil.Die(t, fmt.Errorf("Pod readiness probe delay (%v)s doesnt match passed in parameter (%v)", podReadinessDelaySeconds, f.PodReadinessDelay))
	}

	if podReadinessPeriod := readinessProbe.PeriodSeconds; podReadinessPeriod != int32(f.PodReadinessPeriod.Seconds()) {
		e2eutil.Die(t, fmt.Errorf("Pod readiness probe period (%v)s doesnt match passed in parameter (%v)", podReadinessPeriod, f.PodReadinessPeriod))
	}
}

// Tests creation of a 3 node cluster with 1 bucket
// 1. Create a 3 node Couchbase cluster with 1 bucket.
// 2. Check the events to make sure the operator took the correct actions.
// 3. Verifies that the cluster is balanced and all data is available.
func TestCreateBucketCluster(t *testing.T) {
	// Plaform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonBucketCreated},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}
