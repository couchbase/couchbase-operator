package scheduler

import (
	"fmt"
	"sort"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/kubernetes"
)

const (
	errorHeader = "stripe scheduler"
)

// stripeSchedulerImpl implements a simple scheduler which stripes pods across
// server groups.
type stripeSchedulerImpl struct {
}

// NewStripeScheduler creates an initializes a new sripe scheduler
func NewStripeScheduler() Scheduler {
	return &stripeSchedulerImpl{}
}

// Create inspects the pod and determines the server configuration, it is then able
// to select either server configuration specific server groups or default to the
// global configuration.  Pods in this server configuration are listed and mapped
// to the set of server groups.  To schedule we pick the set of server groups which
// contain the fewest pods, then deterministically select the lexicaly smallest
// before labelling the pod with this server group as a label selector.
func (sched *stripeSchedulerImpl) Create(client kubernetes.Interface, cluster *api.CouchbaseCluster, pod *v1.Pod) error {
	// Infer the server configuration from the pod
	serverConfigName, ok := pod.Labels[constants.LabelNodeConf]
	if !ok {
		return fmt.Errorf("%s: pod missing label '%s'", errorHeader, constants.LabelNodeConf)
	}
	serverConfig := cluster.Spec.GetServerConfigByName(serverConfigName)
	if serverConfig == nil {
		return fmt.Errorf("%s: server config missing for '%s'", errorHeader, serverConfigName)
	}

	// Determine the server groups to use, defaulting to the global configuration
	// if server configuration specific settings do not exist.
	serverGroups := serverConfig.ServerGroups
	if len(serverGroups) == 0 {
		serverGroups = cluster.Spec.ServerGroups
		if len(serverGroups) == 0 {
			return fmt.Errorf("%s: no server groups defined for server config '%s'", errorHeader, serverConfig.Name)
		}
	}

	// List all pods in our cluster and in the server configuration group
	clusterRequirement, err := labels.NewRequirement(constants.LabelCluster, selection.Equals, []string{cluster.Name})
	if err != nil {
		return fmt.Errorf("%s: failed to generate label requirement '%s': %v", errorHeader, constants.LabelCluster, err)
	}
	serverConfigRequirement, err := labels.NewRequirement(constants.LabelNodeConf, selection.Equals, []string{serverConfigName})
	if err != nil {
		return fmt.Errorf("%s: failed to generate label requirement '%s': %v", errorHeader, constants.LabelNodeConf, err)
	}
	selector := labels.NewSelector()
	selector = selector.Add(*clusterRequirement)
	selector = selector.Add(*serverConfigRequirement)
	existingPods, err := client.CoreV1().Pods(cluster.Namespace).List(metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return fmt.Errorf("%s: failed to list existing pods: %v", errorHeader, err)
	}

	// Map from server group to pods
	serverGroupPodMap := map[string][]*v1.Pod{}
	for _, serverGroup := range serverGroups {
		serverGroupPodMap[serverGroup] = []*v1.Pod{}
	}
	for _, existingPod := range existingPods.Items {
		serverGroup, ok := existingPod.Spec.NodeSelector[constants.ServerGroupLabel]
		if !ok {
			return fmt.Errorf("%s: pod %s does not have server group selector", errorHeader, existingPod.Name)
		}
		if _, ok := serverGroupPodMap[serverGroup]; !ok {
			return fmt.Errorf("%s: pod %s server group selector '%s' unknown", errorHeader, existingPod.Name, serverGroup)
		}
		serverGroupPodMap[serverGroup] = append(serverGroupPodMap[serverGroup], &existingPod)
	}

	// Select the server groups with the fewest pods
	smallestSize := int(^uint(0) >> 1)
	var smallestServerGroups []string
	for serverGroup, pods := range serverGroupPodMap {
		size := len(pods)
		if size < smallestSize {
			smallestSize = size
			smallestServerGroups = []string{}
		}
		if size == smallestSize {
			smallestServerGroups = append(smallestServerGroups, serverGroup)
		}
	}

	// Deterministically select the server group to create the pod in
	sort.Strings(smallestServerGroups)
	if _, ok := pod.Spec.NodeSelector[constants.ServerGroupLabel]; !ok {
		pod.Spec.NodeSelector = map[string]string{}
	}
	pod.Spec.NodeSelector[constants.ServerGroupLabel] = smallestServerGroups[0]

	return nil
}
