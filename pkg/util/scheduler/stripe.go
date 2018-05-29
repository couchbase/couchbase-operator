package scheduler

import (
	"fmt"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"k8s.io/api/core/v1"
)

const (
	stripeErrorHeader = "stripe scheduler"
)

// stripeSchedulerImpl implements a simple scheduler which stripes pods across
// server groups.
type stripeSchedulerImpl struct {
	// serverClasses is for tracking our internal state of where pods reside
	// within server classes
	serverClasses serverClassGroupMap
}

// getServerGroupsForClass gets the list of server groups to schedule pods across
// given a specific server class name
func getServerGroupsForClass(cluster *api.CouchbaseCluster, class *api.ServerConfig) ([]string, error) {
	// Determine the server groups to use, defaulting to the global configuration
	// if server configuration specific settings do not exist.
	serverGroups := class.ServerGroups
	if len(serverGroups) == 0 {
		serverGroups = cluster.Spec.ServerGroups
		if len(serverGroups) == 0 {
			return nil, fmt.Errorf("%s: no server groups defined for server config '%s'", stripeErrorHeader, class.Name)
		}
	}

	return serverGroups, nil
}

// NewStripeScheduler creates an initializes a new sripe scheduler, caching
// state from the current set of pods for the cluster
func NewStripeScheduler(podGetter PodGetter, cluster *api.CouchbaseCluster) (Scheduler, error) {
	// Initialize data structures, creating maps for each server class
	// and empty lists for each server group defined for that class
	sched := &stripeSchedulerImpl{
		serverClasses: serverClassGroupMap{},
	}
	for _, class := range cluster.Spec.ServerSettings {
		groups, err := getServerGroupsForClass(cluster, &class)
		if err != nil {
			return nil, err
		}
		sched.serverClasses[class.Name] = serverGroups{}
		for _, group := range groups {
			sched.serverClasses[class.Name][group] = &serverList{}
		}
	}

	// Populate the server group lists with pods
	pods, err := podGetter.Get()
	if err != nil {
		return nil, fmt.Errorf("%s: %v", stripeErrorHeader, err)
	}
	for _, pod := range pods {
		class, ok := pod.Labels[constants.LabelNodeConf]
		if !ok {
			return nil, fmt.Errorf("%s: pod %s does not have server class label", stripeErrorHeader, pod.Name)
		}
		// Class deleted, ignore the pod
		if _, ok := sched.serverClasses[class]; !ok {
			continue
		}
		group, ok := pod.Spec.NodeSelector[constants.ServerGroupLabel]
		if !ok {
			return nil, fmt.Errorf("%s: pod %s does not have server group selector", stripeErrorHeader, pod.Name)
		}
		if _, ok := sched.serverClasses[class][group]; !ok {
			return nil, fmt.Errorf("%s: pod %s server group '%s' undefined", stripeErrorHeader, pod.Name, group)
		}
		sched.serverClasses[class][group].push(pod.Name)
	}

	return sched, nil
}

// Create inspects the pod and determines the server configuration, it is then able
// to select either server configuration specific server groups or default to the
// global configuration.  Pods in this server configuration are listed and mapped
// to the set of server groups.  To schedule we pick the set of server groups which
// contain the fewest pods, then deterministically select the lexicaly smallest
// before labelling the pod with this server group as a label selector.
func (sched *stripeSchedulerImpl) Create(pod *v1.Pod) error {
	// Infer the server configuration from the pod
	class, ok := pod.Labels[constants.LabelNodeConf]
	if !ok {
		return fmt.Errorf("%s: pod missing label '%s'", stripeErrorHeader, constants.LabelNodeConf)
	}
	if _, ok := sched.serverClasses[class]; !ok {
		return fmt.Errorf("%s: pod %s server class '%s' undefined", stripeErrorHeader, pod.Name, class)
	}

	// Find the smallest server group population and add the selected group to the
	// pod's node selectors
	group := sched.serverClasses[class].smallestGroup()
	pod.Spec.NodeSelector[constants.ServerGroupLabel] = group
	sched.serverClasses[class][group].push(pod.Name)

	return nil
}

func (sched *stripeSchedulerImpl) Delete(class string) (string, error) {
	// Select the victim server group based on population
	if _, ok := sched.serverClasses[class]; !ok {
		return "", fmt.Errorf("%s: server group map missing server class '%s'", stripeErrorHeader, class)
	}
	serverGroup := sched.serverClasses[class].largestGroup()

	// Select the victim server deterministically based on alphabetical order
	server, err := sched.serverClasses[class][serverGroup].pop()
	if err != nil {
		return "", fmt.Errorf("%s: server group '%s' in class '%s' empty", stripeErrorHeader, serverGroup, class)
	}

	return server, nil
}
