/*
Copyright 2019-Present Couchbase, Inc.

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

	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAutoCompactionUpdate(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 1
	thresholdPercent := 69
	thresholdSize := e2espec.NewResourceQuantityMi(69)
	thresholdSizeInternal := int64(69 * 1024 * 1024)

	purgeIntervalBase, err := time.ParseDuration("42h")
	if err != nil {
		e2eutil.Die(t, err)
	}

	purgeInterval := metav1.Duration{
		Duration: purgeIntervalBase,
	}
	purgeIntervalInternal := purgeIntervalBase.Hours() / 24.0

	// Create the cluster.
	couchbase := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)

	// Twiddle the knobs, and ensure the server settings are changed.
	couchbase = e2eutil.MustPatchCluster(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Replace("/spec/cluster/autoCompaction/timeWindow/start", "01:01").
		Replace("/spec/cluster/autoCompaction/timeWindow/end", "07:37"), time.Minute)
	e2eutil.MustPatchAutoCompactionSettings(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Test("/AutoCompactionSettings/AllowedTimePeriod/FromHour", 1), time.Minute)
	e2eutil.MustPatchAutoCompactionSettings(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Test("/AutoCompactionSettings/AllowedTimePeriod/FromMinute", 1), time.Minute)
	e2eutil.MustPatchAutoCompactionSettings(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Test("/AutoCompactionSettings/AllowedTimePeriod/ToHour", 7), time.Minute)
	e2eutil.MustPatchAutoCompactionSettings(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Test("/AutoCompactionSettings/AllowedTimePeriod/ToMinute", 37), time.Minute)

	couchbase = e2eutil.MustPatchCluster(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Replace("/spec/cluster/autoCompaction/databaseFragmentationThreshold/percent", &thresholdPercent), time.Minute)
	e2eutil.MustPatchAutoCompactionSettings(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Test("/AutoCompactionSettings/DatabaseFragmentationThreshold/Percentage", thresholdPercent), time.Minute)

	couchbase = e2eutil.MustPatchCluster(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Replace("/spec/cluster/autoCompaction/databaseFragmentationThreshold/size", thresholdSize), time.Minute)
	e2eutil.MustPatchAutoCompactionSettings(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Test("/AutoCompactionSettings/DatabaseFragmentationThreshold/Size", thresholdSizeInternal), time.Minute)

	couchbase = e2eutil.MustPatchCluster(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Replace("/spec/cluster/autoCompaction/viewFragmentationThreshold/percent", &thresholdPercent), time.Minute)
	e2eutil.MustPatchAutoCompactionSettings(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Test("/AutoCompactionSettings/ViewFragmentationThreshold/Percentage", thresholdPercent), time.Minute)

	couchbase = e2eutil.MustPatchCluster(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Replace("/spec/cluster/autoCompaction/viewFragmentationThreshold/size", thresholdSize), time.Minute)
	e2eutil.MustPatchAutoCompactionSettings(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Test("/AutoCompactionSettings/ViewFragmentationThreshold/Size", thresholdSizeInternal), time.Minute)

	couchbase = e2eutil.MustPatchCluster(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Replace("/spec/cluster/autoCompaction/parallelCompaction", true), time.Minute)
	e2eutil.MustPatchAutoCompactionSettings(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Test("/AutoCompactionSettings/ParallelDBAndViewCompaction", true), time.Minute)

	couchbase = e2eutil.MustPatchCluster(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Replace("/spec/cluster/autoCompaction/timeWindow/abortCompactionOutsideWindow", true), time.Minute)
	e2eutil.MustPatchAutoCompactionSettings(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Test("/AutoCompactionSettings/AllowedTimePeriod/AbortOutside", true), time.Minute)

	couchbase = e2eutil.MustPatchCluster(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Replace("/spec/cluster/autoCompaction/tombstonePurgeInterval", &purgeInterval), time.Minute)
	e2eutil.MustPatchAutoCompactionSettings(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Test("/PurgeInterval", purgeIntervalInternal), time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Settings edited N times
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Repeat{Times: 8, Validator: eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited}},
	}

	ValidateEvents(t, kubernetes, couchbase, expectedEvents)
}
