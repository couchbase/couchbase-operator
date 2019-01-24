package cluster

import (
	"encoding/json"
	"io/ioutil"
	"testing"
	"time"

	couchbasev1 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"

	"github.com/sirupsen/logrus"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	// logger is used to black hole output from the janitor.
	logger = logrus.Logger{
		Out: ioutil.Discard,
	}

	// clusterFixture is a basic cluster with a 1 minute log retention time
	// and a maximum retention of 3 logs.
	clusterFixture = &Cluster{
		cluster: &couchbasev1.CouchbaseCluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
			},
			Spec: couchbasev1.ClusterSpec{
				LogRetentionTime:  "1m",
				LogRetentionCount: 3,
			},
		},
		logger: logrus.NewEntry(&logger),
	}
)

// janitorAbstractionInterfaceTestImpl implments the logPVCInterface interface
// for unit tests. For each test it allows the pvcListing of a single invariant set
// of PVCs, so it doesn't support tests which behave differently based on pvcUpdates
// to the PVC pvcList.  It does however accept multiple pvcUpdates and pvcDeletions, each
// of which is individually verified, bascially a mock which checks the parameters
// fo each call.
type janitorAbstractionInterfaceTestImpl struct {
	// t is the testing object.
	t *testing.T
	// pvcList contains the result of a call to List.
	pvcList []*corev1.PersistentVolumeClaim
	// pvcUpdate contains an expected pvcUpdated PVC from a call to Update.
	pvcUpdates []*corev1.PersistentVolumeClaim
	// pvcUpdate is a counter used to podExistsIndex the current pvcUpdate.
	pvcUpdate int
	// pvcDeletions contains an expected deleted PVC from a call to Delete.
	pvcDeletions []string
	// pvcDeletion is a counter used to podExistsIndex the current pvcDeletion.
	pvcDeletion int
	// podExists is a pvcList of whether a pod podExists or not.
	podExists []bool
	// podExistsIndex is the current podExistsIndex into the podExists slice.
	podExistsIndex int
}

// LogPVCList returns the pvcList of PVCs.
func (j *janitorAbstractionInterfaceTestImpl) LogPVCList() ([]*corev1.PersistentVolumeClaim, error) {
	return j.pvcList, nil
}

// LogPVCUpdate checks the pvcUpdate against the current expected podExistsIndex, raising an error on mismatch
// or array overflow.
func (j *janitorAbstractionInterfaceTestImpl) LogPVCUpdate(pvc *corev1.PersistentVolumeClaim) error {
	if j.pvcUpdate >= len(j.pvcUpdates) {
		j.t.Fatalf("Update() overflow")
	}
	// Oddly reflect doesn't work here, probably nil vs empty slice
	a, _ := json.Marshal(j.pvcUpdates[j.pvcUpdate])
	b, _ := json.Marshal(pvc)
	if string(a) != string(b) {
		j.t.Fatalf("Update() mismatch\nexpected: %s\nactual: %s", string(a), string(b))
	}
	j.pvcUpdate++
	return nil
}

// LogPVCDelete checks the pvcDeletion against the current expected podExistsIndex, raising an error on mismatch
// or array overflow.
func (j *janitorAbstractionInterfaceTestImpl) LogPVCDelete(name string) error {
	if j.pvcDeletion >= len(j.pvcDeletions) {
		j.t.Fatalf("Delete() overflow")
	}
	if j.pvcDeletions[j.pvcDeletion] != name {
		j.t.Fatalf("Delete() mismatch\nexpected: %s\nactual: %s", j.pvcDeletions[j.pvcDeletion], name)
	}
	j.pvcDeletion++
	return nil
}

// Exists returns a configurable boolean to indicate whether a pod has been seen.
// It is assumed podExists will be called once for each PVC returned by the List
// method of the logPVCInterface.
func (j *janitorAbstractionInterfaceTestImpl) PodExists(name string) (bool, error) {
	if j.podExistsIndex >= len(j.podExists) {
		j.t.Fatalf("Exists() overflow")
	}
	e := j.podExists[j.podExistsIndex]
	j.podExistsIndex++
	return e, nil
}

// mustValidate checks the observed number of pvcUpdates and pvcDeletions was matched by reality.
func (j *janitorAbstractionInterfaceTestImpl) mustValidate() {
	if len(j.pvcUpdates) != j.pvcUpdate {
		j.t.Fatalf("expected pvcUpdate(s) not all seen\nexpected: %d\nactual: %d", len(j.pvcUpdates), j.pvcUpdate)
	}
	if len(j.pvcDeletions) != j.pvcDeletion {
		j.t.Fatalf("expected pvcDeletion(s) not all seen\nexpected: %d\nactual: %d", len(j.pvcDeletions), j.pvcDeletion)
	}
}

// mustCleanPeriodic checks that the cleanPeriodic method of the janitor type
// completes without error.
func (j *janitor) mustCleanPeriodic(t *testing.T) {
	if err := j.cleanPeriodic(); err != nil {
		t.Fatal(err)
	}
}

// pcvFixture returns a new persistent volume claim with necessary fields.
func pcvFixture(name, pod string, detached *time.Time) *corev1.PersistentVolumeClaim {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				constants.LabelNode: pod,
			},
		},
	}
	if detached != nil {
		pvc.Annotations = map[string]string{
			constants.VolumeDetachedAnnotation: detached.Format(time.RFC3339),
		}
	}
	return pvc
}

// TestAnnotateLogVolume ensures a PVC whose pod does not exist is annotated
// with a detached timestamp.
func TestAnnotateLogVolume(t *testing.T) {
	// This is potentially racy, but I've never seen it happen...
	d := time.Now()
	a := &janitorAbstractionInterfaceTestImpl{
		t: t,
		pvcList: []*corev1.PersistentVolumeClaim{
			pcvFixture("test", "test", nil),
		},
		pvcUpdates: []*corev1.PersistentVolumeClaim{
			pcvFixture("test", "test", &d),
		},
		podExists: []bool{
			false,
		},
	}
	j := &janitor{
		cluster:     clusterFixture,
		abstraction: a,
	}

	j.mustCleanPeriodic(t)
	a.mustValidate()
}

// TestUnannotateLogVolume ensures a PVC whose pod podExists and is annotated with
// a detached timestamp has it removed.
func TestUnannotateLogVolume(t *testing.T) {
	d := time.Now()
	a := &janitorAbstractionInterfaceTestImpl{
		t: t,
		pvcList: []*corev1.PersistentVolumeClaim{
			pcvFixture("test", "test", &d),
		},
		pvcUpdates: []*corev1.PersistentVolumeClaim{
			pcvFixture("test", "test", nil),
		},
		podExists: []bool{
			true,
		},
	}
	j := &janitor{
		cluster:     clusterFixture,
		abstraction: a,
	}

	j.mustCleanPeriodic(t)
	a.mustValidate()
}

// TestDeleteLogVolumeTimeExpiry ensures a PVC whose detached annotation is
// 10 minutes before the retention time is deleted.
func TestDeleteLogVolumeTimeExpiry(t *testing.T) {
	d := time.Now().Add(-10 * time.Minute)
	a := &janitorAbstractionInterfaceTestImpl{
		t: t,
		pvcList: []*corev1.PersistentVolumeClaim{
			pcvFixture("test", "test", &d),
		},
		pvcDeletions: []string{
			"test",
		},
		podExists: []bool{
			false,
		},
	}
	j := &janitor{
		cluster:     clusterFixture,
		abstraction: a,
	}

	j.mustCleanPeriodic(t)
	a.mustValidate()
}

// TestDeleteLogVolumeCount ensures PVCs above the count threshold are deleted
// earliest first.
func TestDeleteLogVolumeCount(t *testing.T) {
	d1 := time.Now().Add(8 * time.Second)
	d2 := time.Now().Add(2 * time.Second) // <--- second oldest
	d3 := time.Now().Add(9 * time.Second)
	d4 := time.Now().Add(1 * time.Second) // <--- oldest
	d5 := time.Now().Add(3 * time.Second)
	a := &janitorAbstractionInterfaceTestImpl{
		t: t,
		pvcList: []*corev1.PersistentVolumeClaim{
			pcvFixture("test1", "test1", &d1),
			pcvFixture("test2", "test2", &d2),
			pcvFixture("test3", "test3", &d3),
			pcvFixture("test4", "test4", &d4),
			pcvFixture("test5", "test5", &d5),
		},
		pvcDeletions: []string{
			"test4",
			"test2",
		},
		podExists: []bool{
			false,
			false,
			false,
			false,
			false,
		},
	}
	j := &janitor{
		cluster:     clusterFixture,
		abstraction: a,
	}

	j.mustCleanPeriodic(t)
	a.mustValidate()
}
