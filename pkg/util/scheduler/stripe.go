/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package scheduler

import (
	"fmt"
	"sort"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	v1 "k8s.io/api/core/v1"
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
// given a specific server class name.
func getServerGroupsForClass(cluster *couchbasev2.CouchbaseCluster, class *couchbasev2.ServerConfig) ([]string, error) {
	// Determine the server groups to use, defaulting to the global configuration
	// if server configuration specific settings do not exist.
	serverGroups := class.ServerGroups
	if len(serverGroups) == 0 {
		serverGroups = cluster.Spec.ServerGroups
		if len(serverGroups) == 0 {
			return nil, fmt.Errorf("%s: no server groups defined for server config '%s': %w", stripeErrorHeader, class.Name, errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
		}
	}

	return serverGroups, nil
}

// NewStripeScheduler creates an initializes a new sripe scheduler, caching
// state from the current set of pods for the cluster.
func NewStripeScheduler(pods []*v1.Pod, cluster *couchbasev2.CouchbaseCluster) (Scheduler, error) {
	// Initialize data structures, creating maps for each server class
	// and empty lists for each server group defined for that class
	sched := &stripeSchedulerImpl{
		serverClasses: serverClassGroupMap{},
	}

	for i := range cluster.Spec.Servers {
		class := cluster.Spec.Servers[i]

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
	for _, pod := range pods {
		// Pod is faulty ignore it
		if pod.Status.Phase != v1.PodPending && pod.Status.Phase != v1.PodRunning {
			continue
		}

		class, ok := pod.Labels[constants.LabelNodeConf]
		if !ok {
			return nil, fmt.Errorf("%s: pod %s does not have server class label: %w", stripeErrorHeader, pod.Name, errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
		}

		// Class deleted, ignore the pod
		if _, ok := sched.serverClasses[class]; !ok {
			continue
		}

		group, ok := pod.Spec.NodeSelector[constants.ServerGroupLabel]
		if !ok {
			return nil, fmt.Errorf("%s: pod %s does not have server group selector: %w", stripeErrorHeader, pod.Name, errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
		}

		if _, ok := sched.serverClasses[class][group]; !ok {
			return nil, fmt.Errorf("%s: pod %s server group '%s' undefined: %w", stripeErrorHeader, pod.Name, group, errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
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
func (sched *stripeSchedulerImpl) Create(class, name, group string) (string, error) {
	if _, ok := sched.serverClasses[class]; !ok {
		return "", fmt.Errorf("%s: pod %s server class '%s' undefined: %w", stripeErrorHeader, name, class, errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	if group == "" {
		group = sched.serverClasses[class].smallestGroup()
	}

	sched.serverClasses[class][group].push(name)

	return group, nil
}

func (sched *stripeSchedulerImpl) Delete(class string) (string, error) {
	// Select the victim server group based on population
	if _, ok := sched.serverClasses[class]; !ok {
		return "", fmt.Errorf("%s: server group map missing server class '%s': %w", stripeErrorHeader, class, errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	serverGroup := sched.serverClasses[class].largestGroup()

	// Select the victim server deterministically based on alphabetical order
	server, err := sched.serverClasses[class][serverGroup].pop()
	if err != nil {
		return "", fmt.Errorf("%s: server group '%s' in class '%s' empty: %w", stripeErrorHeader, serverGroup, class, errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	return server, nil
}

// Upgrade removes a node from the scheduler as it's an upgrade target.
func (sched *stripeSchedulerImpl) Upgrade(class, name string) error {
	for serverGroup := range sched.serverClasses[class] {
		if err := sched.serverClasses[class][serverGroup].del(name); err == nil {
			return nil
		}
	}

	return fmt.Errorf("%s: server '%s' does not exist in class '%s': %w", stripeErrorHeader, name, class, errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
}

// LogStatus writes formatted state for debugging.
func (sched *stripeSchedulerImpl) LogStatus(cluster string) {
	// Remember the classes/groups so we can do an ordered/sorted traversal
	mapClass := map[string]interface{}{}
	mapGroup := map[string]interface{}{}

	for class, groups := range sched.serverClasses {
		mapClass[class] = nil

		for group := range groups {
			mapGroup[group] = nil
		}
	}

	// Make the output deterministic
	listClass := []string{}
	for class := range mapClass {
		listClass = append(listClass, class)
	}

	listGroup := []string{}
	for group := range mapGroup {
		listGroup = append(listGroup, group)
	}

	sort.Strings(listClass)
	sort.Strings(listGroup)

	for _, class := range listClass {
		for _, group := range listGroup {
			// The class may not contain a particular group, and that may contain no
			// servers, but we are protected by zero values being returned.
			servers := sched.serverClasses[class][group]
			if servers == nil {
				continue
			}

			servers.sort()

			for _, server := range servers.servers {
				log.Info("Scheduler status", "cluster", cluster, "name", server, "class", class, "group", group)
			}
		}
	}
}
