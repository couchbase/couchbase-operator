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
	kubernetes := f.GetCluster(0)

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
	couchbase := e2eutil.MustNewClusterBasic(t, kubernetes, kubernetes.Namespace, clusterSize)

	// Twiddle the knobs, and ensure the server settings are changed.
	couchbase = e2eutil.MustPatchCluster(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Replace("/Spec/ClusterSettings/AutoCompaction/DatabaseFragmentationThreshold/Percent", &thresholdPercent), time.Minute)
	e2eutil.MustPatchAutoCompactionSettings(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Test("/AutoCompactionSettings/DatabaseFragmentationThreshold/Percentage", thresholdPercent), time.Minute)

	couchbase = e2eutil.MustPatchCluster(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Replace("/Spec/ClusterSettings/AutoCompaction/DatabaseFragmentationThreshold/Size", thresholdSize), time.Minute)
	e2eutil.MustPatchAutoCompactionSettings(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Test("/AutoCompactionSettings/DatabaseFragmentationThreshold/Size", thresholdSizeInternal), time.Minute)

	couchbase = e2eutil.MustPatchCluster(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Replace("/Spec/ClusterSettings/AutoCompaction/ViewFragmentationThreshold/Percent", &thresholdPercent), time.Minute)
	e2eutil.MustPatchAutoCompactionSettings(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Test("/AutoCompactionSettings/ViewFragmentationThreshold/Percentage", thresholdPercent), time.Minute)

	couchbase = e2eutil.MustPatchCluster(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Replace("/Spec/ClusterSettings/AutoCompaction/ViewFragmentationThreshold/Size", thresholdSize), time.Minute)
	e2eutil.MustPatchAutoCompactionSettings(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Test("/AutoCompactionSettings/ViewFragmentationThreshold/Size", thresholdSizeInternal), time.Minute)

	couchbase = e2eutil.MustPatchCluster(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Replace("/Spec/ClusterSettings/AutoCompaction/ParallelCompaction", true), time.Minute)
	e2eutil.MustPatchAutoCompactionSettings(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Test("/AutoCompactionSettings/ParallelDBAndViewCompaction", true), time.Minute)

	couchbase = e2eutil.MustPatchCluster(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Replace("/Spec/ClusterSettings/AutoCompaction/TimeWindow/Start", "01:01"), time.Minute)
	e2eutil.MustPatchAutoCompactionSettings(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Test("/AutoCompactionSettings/IndexCircularCompaction/Interval/FromHour", 1), time.Minute)
	e2eutil.MustPatchAutoCompactionSettings(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Test("/AutoCompactionSettings/IndexCircularCompaction/Interval/FromMinute", 1), time.Minute)

	couchbase = e2eutil.MustPatchCluster(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Replace("/Spec/ClusterSettings/AutoCompaction/TimeWindow/End", "07:37"), time.Minute)
	e2eutil.MustPatchAutoCompactionSettings(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Test("/AutoCompactionSettings/IndexCircularCompaction/Interval/ToHour", 7), time.Minute)
	e2eutil.MustPatchAutoCompactionSettings(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Test("/AutoCompactionSettings/IndexCircularCompaction/Interval/ToMinute", 37), time.Minute)

	couchbase = e2eutil.MustPatchCluster(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Replace("/Spec/ClusterSettings/AutoCompaction/TimeWindow/AbortCompactionOutsideWindow", true), time.Minute)
	e2eutil.MustPatchAutoCompactionSettings(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Test("/AutoCompactionSettings/IndexCircularCompaction/Interval/AbortOutside", true), time.Minute)

	couchbase = e2eutil.MustPatchCluster(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Replace("/Spec/ClusterSettings/AutoCompaction/TombstonePurgeInterval", &purgeInterval), time.Minute)
	e2eutil.MustPatchAutoCompactionSettings(t, kubernetes, couchbase, jsonpatch.NewPatchSet().Test("/PurgeInterval", purgeIntervalInternal), time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Settings edited N times
	expectedEvents := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Repeat{Times: 9, Validator: eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited}},
	}

	ValidateEvents(t, kubernetes, couchbase, expectedEvents)
}
