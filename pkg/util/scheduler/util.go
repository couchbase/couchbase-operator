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

	"github.com/couchbase/couchbase-operator/pkg/errors"
)

// serverList is a list of servers in a server group.
type serverList struct {
	servers []string
}

// push adds a new node to the server list.
func (s *serverList) push(server string) {
	s.servers = append(s.servers, server)
}

// sort alphabetically sorts a server list.
func (s *serverList) sort() {
	sort.Strings(s.servers)
}

// pop alphabetically sorts a server list, removes and returns the tail item.
func (s *serverList) pop() (string, error) {
	if len(s.servers) == 0 {
		return "", fmt.Errorf("%w: pop from empty server list", errors.NewStackTracedError(errors.ErrInternalError))
	}

	item := len(s.servers) - 1
	server := s.servers[item]
	s.servers = s.servers[:item]

	return server, nil
}

// del removes the named server from the server list.
func (s *serverList) del(name string) error {
	for index, server := range s.servers {
		if server == name {
			s.servers = append(s.servers[:index], s.servers[index+1:]...)
			return nil
		}
	}

	return fmt.Errorf("%w: del of non-existent server from server list", errors.NewStackTracedError(errors.ErrInternalError))
}

// serverGroups maps server group names to a list of servers.
type serverGroups map[string]*serverList

// sizes returns a list of the size of each server group.
func (s serverGroups) sizes() []int {
	// Map from from groups of pods to a list of lengths
	sizes := []int{}

	for _, servers := range s {
		sizes = append(sizes, len(servers.servers))
	}

	return sizes
}

// minSize finds the smallest server group population in the
// provided server group map.
func (s serverGroups) minSize() int {
	sizes := s.sizes()
	min := sizes[0]

	for _, size := range sizes {
		if size < min {
			min = size
		}
	}

	return min
}

// maxSize finds the largest server group population in the
// provided server group map.
func (s serverGroups) maxSize() int {
	sizes := s.sizes()
	max := 0

	for _, size := range sizes {
		if size > max {
			max = size
		}
	}

	return max
}

// filterPredicate is used to filter server groups based typically on a closure.
type filterPredicate func(*serverList) bool

// filterGroupsOnSize returns an alphabetically sorted list of server group names
// based on some predicate based on the list of servers in each group.
func (s serverGroups) filter(predicate filterPredicate) []string {
	groups := []string{}

	for group, servers := range s {
		if predicate(servers) {
			groups = append(groups, group)
		}
	}

	return groups
}

// smallestGroups returns an alphabetically sorted list of the smallest server
// groups for a class.
func (s serverGroups) smallestGroups() []string {
	min := s.minSize()

	return s.filter(func(servers *serverList) bool {
		return len(servers.servers) == min
	})
}

// smallestGroup return the smallest server group for a class, returning the
// item with the smallest name on contention.
func (s serverGroups) smallestGroup() string {
	groups := s.smallestGroups()
	sort.Strings(groups)

	return groups[0]
}

// largestGroups returns an alphabetically sorted list of the largest server
// groups for a class.
func (s serverGroups) largestGroups() []string {
	max := s.maxSize()

	return s.filter(func(servers *serverList) bool {
		return len(servers.servers) == max
	})
}

// largestGroup return the largest server group for a class, returning the
// item with the largest name on contention.
func (s serverGroups) largestGroup() string {
	groups := s.largestGroups()
	sort.Strings(groups)

	return groups[len(groups)-1]
}

// serverClassGroupMap maps server classes to their server groups of pods.
type serverClassGroupMap map[string]serverGroups
