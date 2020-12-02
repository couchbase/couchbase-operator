package scheduler

import (
	"testing"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// Default pod names.
	podName1 = "alice"
	podName2 = "bob"
	podName3 = "charlie"
	podName4 = "david"
	podName5 = "eve"
	podName6 = "faythe"
	podName7 = "grace"

	// Default cluster name to use.
	clusterName = "dragon"

	// Default namespace to use.
	namespace = "unicorn"

	// Server group labels.  For all create and delete events where there
	// is contention we pick the 'smallest' e.g. alice < bob.
	serverGroup1 = "alice"
	serverGroup2 = "bob"
	serverGroup3 = "charlie"

	// Server classes to test configuration.
	serverClass1 = "mickey"
	serverClass2 = "minnie"
)

var (
	// Defines a cluster with a global server group definition
	// and an override for serverClass2.  Randomize the order of
	// the groups to ensure they are picked in order.
	fixtureCluster = &couchbasev2.CouchbaseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
		},
		Spec: couchbasev2.ClusterSpec{
			ServerGroups: []string{
				serverGroup3,
				serverGroup1,
				serverGroup2,
			},
			Servers: []couchbasev2.ServerConfig{
				{
					Name: serverClass1,
				},
				{
					Name: serverClass2,
					ServerGroups: []string{
						serverGroup2,
						serverGroup1,
					},
				},
			},
		},
	}

	// Defines a cluster with invalid configuration; serverClass2 enables
	// scheduling however there is no global default for serverClass1.
	fixtureClusterInvalid = &couchbasev2.CouchbaseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
		},
		Spec: couchbasev2.ClusterSpec{
			Servers: []couchbasev2.ServerConfig{
				{
					Name: serverClass1,
				},
				{
					Name: serverClass2,
					ServerGroups: []string{
						serverGroup1,
						serverGroup2,
					},
				},
			},
		},
	}

	// An empty set of pods.
	fixturePodsEmpty = []*v1.Pod{}

	// A set of pods N-1 allocated per server class
	// Used to ensure correct insertion with pre existing pods.
	fixturePodsCreate = []*v1.Pod{
		podFixture(podName1, serverClass1, serverGroup1),
		podFixture(podName2, serverClass1, serverGroup2),
		podFixture(podName3, serverClass2, serverGroup1),
	}

	// A set of pods with multiple servers per class.
	// Used to test correct deletion from server groups.
	fixturePodsDelete = []*v1.Pod{
		podFixture(podName1, serverClass1, serverGroup1),
		podFixture(podName2, serverClass1, serverGroup2),
		podFixture(podName3, serverClass1, serverGroup3),
		podFixture(podName4, serverClass1, serverGroup1),
		podFixture(podName5, serverClass1, serverGroup2),
		podFixture(podName6, serverClass1, serverGroup3),
	}

	// A set of pods where one server has failed.
	fixturePodsFailure = []*v1.Pod{
		podFixture(podName1, serverClass1, serverGroup1),
		podFixture(podName2, serverClass1, serverGroup2),
		podFixtureFailed(podName3, serverClass1, serverGroup3),
	}

	// A set of pods who are missing the server class label.
	fixturePodsInvalidServerClassLabels = []*v1.Pod{
		podFixture(podName1, "", serverGroup1),
	}

	// A set of pods who are missing the server group label.
	fixturePodsInvalidServerGroupNodeSelector = []*v1.Pod{
		podFixture(podName1, serverClass1, ""),
	}
)

// podFixture returns a pod object with the node configuration (server class) set.
func podFixture(name, class, group string) *v1.Pod {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{},
		},
		Spec: v1.PodSpec{
			NodeSelector: map[string]string{},
		},
		Status: v1.PodStatus{
			Phase: v1.PodRunning,
		},
	}

	if class != "" {
		pod.Labels[constants.LabelNodeConf] = class
	}

	if group != "" {
		pod.Spec.NodeSelector[constants.ServerGroupLabel] = group
	}

	return pod
}

// podFixtureFailed returns a pod object with the node configuration (server class) set.
func podFixtureFailed(name, class, group string) *v1.Pod {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{},
		},
		Spec: v1.PodSpec{
			NodeSelector: map[string]string{},
		},
		Status: v1.PodStatus{
			Phase: v1.PodFailed,
		},
	}

	if class != "" {
		pod.Labels[constants.LabelNodeConf] = class
	}

	if group != "" {
		pod.Spec.NodeSelector[constants.ServerGroupLabel] = group
	}

	return pod
}

// checkCreateScheduling asserts that the the pod has been scheduled where we expected.
func checkCreateScheduling(t *testing.T, selected, expected string) {
	if selected != expected {
		t.Fatalf("Scheduler created server in group '%s', expected '%s'", selected, expected)
	}
}

// checkDeleteScheduling asserts that the selected server is as we expect.
func checkDeleteScheduling(t *testing.T, selected, expected string) {
	if selected != expected {
		t.Fatalf("Scheduler deleted '%s', expected '%s'", selected, expected)
	}
}

// mustCreate ensures create completes without error.
func mustCreate(t *testing.T, s Scheduler, name, class, group string) string {
	group, err := s.Create(name, class, group)
	if err != nil {
		t.Fatal(err)
	}

	return group
}

// Adds a pod to global server groups and expects it to be scheduled deterministically.
func TestStripeCreateGlobalSingle(t *testing.T) {
	c := fixtureCluster

	s, err := NewStripeScheduler(fixturePodsEmpty, c)
	if err != nil {
		t.Fatal(err)
	}

	checkCreateScheduling(t, mustCreate(t, s, serverClass1, "", ""), serverGroup1)
}

// Adds four pods to global server groups and expects them to be scheduled deterministically.
func TestStripeCreateGlobalMultiple(t *testing.T) {
	c := fixtureCluster

	s, err := NewStripeScheduler(fixturePodsEmpty, c)
	if err != nil {
		t.Fatal(err)
	}

	checkCreateScheduling(t, mustCreate(t, s, serverClass1, "", ""), serverGroup1)
	checkCreateScheduling(t, mustCreate(t, s, serverClass1, "", ""), serverGroup2)
	checkCreateScheduling(t, mustCreate(t, s, serverClass1, "", ""), serverGroup3)
	checkCreateScheduling(t, mustCreate(t, s, serverClass1, "", ""), serverGroup1)
}

// Adds three pods to override server groups and expects them to be scheduled deterministically.
func TestStripeCreateOverrideMultiple(t *testing.T) {
	c := fixtureCluster

	s, err := NewStripeScheduler(fixturePodsEmpty, c)
	if err != nil {
		t.Fatal(err)
	}

	checkCreateScheduling(t, mustCreate(t, s, serverClass2, "", ""), serverGroup1)
	checkCreateScheduling(t, mustCreate(t, s, serverClass2, "", ""), serverGroup2)
	checkCreateScheduling(t, mustCreate(t, s, serverClass2, "", ""), serverGroup1)
}

// TestStripe bad configurations error.
func TestStripeInvalidConfiguration(t *testing.T) {
	c := fixtureClusterInvalid

	_, err := NewStripeScheduler(fixturePodsEmpty, c)
	if err == nil {
		t.Fatal("Scheduler accepted invalid configuration")
	}
}

// TestStripe pods missing server class labels error.
func TestStripeInvalidPodServerClassLabels(t *testing.T) {
	c := fixtureCluster

	_, err := NewStripeScheduler(fixturePodsInvalidServerClassLabels, c)
	if err == nil {
		t.Fatal("Scheduler accepted pods with missing server class labels")
	}
}

// Test pods missing server group labels error.
func TestStripeInvalidPodServerGroupNodeSelector(t *testing.T) {
	c := fixtureCluster

	_, err := NewStripeScheduler(fixturePodsInvalidServerGroupNodeSelector, c)
	if err == nil {
		t.Fatal("Scheduler accepted pods with missing server group labels")
	}
}

// Test that pods are deterministically scheduled when initialised with existing pods.
func TestStripeCreateGlobalExistingPods(t *testing.T) {
	c := fixtureCluster

	s, err := NewStripeScheduler(fixturePodsCreate, c)
	if err != nil {
		t.Fatal(err)
	}

	checkCreateScheduling(t, mustCreate(t, s, serverClass1, "", ""), serverGroup3)
}

// Test that pods are deterministically scheduled when initialised with existing pods.
func TestStripeCreateOverrideExistingPods(t *testing.T) {
	c := fixtureCluster

	s, err := NewStripeScheduler(fixturePodsCreate, c)
	if err != nil {
		t.Fatal(err)
	}

	checkCreateScheduling(t, mustCreate(t, s, serverClass2, "", ""), serverGroup2)
}

// Test that scheduling a deletion of an empty server class errors.
func TestStripeDeleteGlobalSingleEmptyClass(t *testing.T) {
	c := fixtureCluster

	s, err := NewStripeScheduler(fixturePodsEmpty, c)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.Delete(serverClass1); err == nil {
		t.Fatal("Scheduler scheduled deletion of pod from empty server class")
	}
}

// Test that scheduling a deletion of a server is deterministic.
func TestStripeDeleteGlobalSingle(t *testing.T) {
	c := fixtureCluster

	s, err := NewStripeScheduler(fixturePodsDelete, c)
	if err != nil {
		t.Fatal(err)
	}

	server, err := s.Delete(serverClass1)
	if err != nil {
		t.Fatal(err)
	}

	checkDeleteScheduling(t, server, podName6)
}

// Test that scheduling a deletion of multiple servers is deterministic.
func TestStripeDeleteGlobalMultiple(t *testing.T) {
	c := fixtureCluster

	s, err := NewStripeScheduler(fixturePodsDelete, c)
	if err != nil {
		t.Fatal(err)
	}

	server1, err := s.Delete(serverClass1)
	if err != nil {
		t.Fatal(err)
	}

	server2, err := s.Delete(serverClass1)
	if err != nil {
		t.Fatal(err)
	}

	server3, err := s.Delete(serverClass1)
	if err != nil {
		t.Fatal(err)
	}

	checkDeleteScheduling(t, server1, podName6)
	checkDeleteScheduling(t, server2, podName5)
	checkDeleteScheduling(t, server3, podName4)
}

// Test that deletion and addition occur correctly.
func TestStipeDeleteCreateGlobalSingle(t *testing.T) {
	c := fixtureCluster

	s, err := NewStripeScheduler(fixturePodsDelete, c)
	if err != nil {
		t.Fatal(err)
	}

	server1, err := s.Delete(serverClass1)
	if err != nil {
		t.Fatal(err)
	}

	checkDeleteScheduling(t, server1, podName6)
	checkCreateScheduling(t, mustCreate(t, s, serverClass1, podName7, ""), serverGroup3)
}

// Test that the scheduler correctly interrogates pod state.
func TestStripeAddGlobalSingleOnPodFailure(t *testing.T) {
	c := fixtureCluster

	s, err := NewStripeScheduler(fixturePodsFailure, c)
	if err != nil {
		t.Fatal(err)
	}

	checkCreateScheduling(t, mustCreate(t, s, serverClass1, podName4, ""), serverGroup3)
}
