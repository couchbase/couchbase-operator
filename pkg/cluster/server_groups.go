package cluster

import (
	"context"
	"fmt"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
)

// getServerGroups looks over the spec and collects all server groups
// which are defined.
func (c *Cluster) getServerGroups() []string {
	// Gather a set of unique server groups
	serverGroups := map[string]interface{}{}

	for _, serverGroup := range c.cluster.Spec.ServerGroups {
		serverGroups[serverGroup] = nil
	}

	for _, serverClass := range c.cluster.Spec.Servers {
		for _, serverGroup := range serverClass.ServerGroups {
			serverGroups[serverGroup] = nil
		}
	}

	// Map into a list
	serverGroupList := []string{}

	for serverGroup := range serverGroups {
		serverGroupList = append(serverGroupList, serverGroup)
	}

	return serverGroupList
}

// createServerGroups creates any server groups defined in the specification
// whuch Couchbase doesn't know about.
func (c *Cluster) createServerGroups(existingGroups *couchbaseutil.ServerGroups) error {
	serverGroups := c.getServerGroups()

	ctx, cancel := context.WithTimeout(c.ctx, extendedRetryPeriod)
	defer cancel()

	// Create any that do not exist
	for i := range serverGroups {
		serverGroup := serverGroups[i]

		if existingGroups.GetServerGroup(serverGroup) != nil {
			continue
		}

		if err := couchbaseutil.CreateServerGroup(serverGroup).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		// 409s have been seen due to this not being updated quick enough, ensure the
		// new server group exists before continuing
		callback := func() error {
			if err := couchbaseutil.ListServerGroups(existingGroups).On(c.api, c.readyMembers()); err != nil {
				return err
			}

			if existingGroups.GetServerGroup(serverGroup) == nil {
				return fmt.Errorf("%w: server group %s not found", errors.NewStackTracedError(errors.ErrCouchbaseServerError), serverGroup)
			}

			return nil
		}

		if err := retryutil.Retry(ctx, 5*time.Second, callback); err != nil {
			return err
		}
	}

	return nil
}

// Given a server group update return the index of the named group
// TODO: Move to gocouchbaseutil as a receiver function.
func serverGroupIndex(update *couchbaseutil.ServerGroupsUpdate, name string) (int, error) {
	for index, group := range update.Groups {
		if group.Name == name {
			return index, nil
		}
	}

	return -1, fmt.Errorf("%w: server group %s undefined", errors.NewStackTracedError(errors.ErrInternalError), name)
}

// reconcileServerGroups looks at the cluster specification, if we have enabled
// server groups, lookup the availability zone the member is in, create the server
// group if it doesn't exist and add the member to the group.
func (c *Cluster) reconcileServerGroups() (bool, error) {
	// Cluster not server group aware
	if !c.cluster.Spec.ServerGroupsEnabled() {
		return false, nil
	}

	// Poll the server for existing information
	existingGroups := &couchbaseutil.ServerGroups{}
	if err := couchbaseutil.ListServerGroups(existingGroups).On(c.api, c.readyMembers()); err != nil {
		return false, err
	}

	// Create any server groups which need defining
	if err := c.createServerGroups(existingGroups); err != nil {
		return false, err
	}

	// Create a server group update
	newGroups := couchbaseutil.ServerGroupsUpdate{
		Groups: []couchbaseutil.ServerGroupUpdate{},
	}

	for _, existingGroup := range existingGroups.Groups {
		newGroup := couchbaseutil.ServerGroupUpdate{
			Name:  existingGroup.Name,
			URI:   existingGroup.URI,
			Nodes: []couchbaseutil.ServerGroupUpdateOTPNode{},
		}
		newGroups.Groups = append(newGroups.Groups, newGroup)
	}

	// Look at each node in each existing group building up the update
	// structure and also checking to see whether we need to dispatch this
	// change to Couchbase server
	update := false

	for _, existingGroup := range existingGroups.Groups {
		for _, existingMember := range existingGroup.Nodes {
			// Extract the scheduled server group for the node
			podName := existingMember.HostName.GetMemberName()

			// Just reuse the old server group location on error, the pod is likely down
			scheduledServerGroup, err := k8sutil.GetServerGroup(c.k8s, podName)
			if err != nil {
				scheduledServerGroup = existingGroup.Name
			}

			// TODO: should we flag this as a warning and leave it where it is?
			if scheduledServerGroup == "" {
				return false, fmt.Errorf("%w: server group unset for pod %s", errors.NewStackTracedError(errors.ErrCouchbaseServerError), podName)
			}

			// If the node is in the wrong server group schedule an update
			if scheduledServerGroup != existingGroup.Name {
				update = true
			}

			// Calculate the server group to add the node to
			index, err := serverGroupIndex(&newGroups, scheduledServerGroup)
			if err != nil {
				// You have done something stupid like change the pod label
				return false, fmt.Errorf("server group %s for pod %s undefined: %w", scheduledServerGroup, podName, err)
			}

			// Insert the node in the correct server group
			otpNode := couchbaseutil.ServerGroupUpdateOTPNode{
				OTPNode: existingMember.OTPNode,
			}
			newGroups.Groups[index].Nodes = append(newGroups.Groups[index].Nodes, otpNode)
		}
	}

	// Nothing to do
	if !update {
		return false, nil
	}

	return true, couchbaseutil.UpdateServerGroups(existingGroups.GetRevision(), &newGroups).On(c.api, c.readyMembers())
}
