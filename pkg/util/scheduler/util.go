package scheduler

import (
	"container/list"
	"fmt"
	"hash/fnv"
	"math/rand"
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

	return fmt.Errorf("%w: server name doesn't exist in server list", errors.NewStackTracedError(errors.ErrInternalError))
}

// find looks for a particular named server from the server list.
func (s *serverList) find(name string) bool {
	for _, server := range s.servers {
		if server == name {
			return true
		}
	}

	return false
}

type serverGroups interface {
	addGroup(name string)
	addGroupIfDoesntExist(group string)
	getMap() map[string]*serverList
	getGroup(name string) *serverList
	groupExists(group string) bool
	smallestGroup() string
	largestGroup() string
	largestGroups() []string

	// smallestGroups returns a list of the smallest server groups is order.
	smallestGroups() []string

	// sizeOrderedGroups returns a list of server groups in ascending size order.
	sizeOrderedGroups() []string
}

func newOrderedServerGroups() orderedServerGroups {
	return orderedServerGroups{
		groupsOrder: &[]string{},
		groupMap:    groupMap{},
	}
}

type orderedServerGroups struct {
	groupsOrder *[]string
	groupMap
}

func (s orderedServerGroups) addGroup(name string) {
	*s.groupsOrder = append(*s.groupsOrder, name)
	s.groupMap.addGroup(name)
}

func (s orderedServerGroups) addGroupIfDoesntExist(group string) {
	if _, ok := s.groupMap[group]; !ok {
		s.addGroup(group)
	}
}

// filterGroupsOnSize returns an alphabetically sorted list of server group names
// based on some predicate based on the list of servers in each group.
func (s orderedServerGroups) filter(predicate filterPredicate) []string {
	groups := []string{}

	for _, group := range *s.groupsOrder {
		if predicate(s.groupMap[group]) {
			groups = append(groups, group)
		}
	}

	return groups
}

// smallestGroups returns a list of the smallest server groups.
func (s orderedServerGroups) smallestGroups() []string {
	min := s.minSize()

	return s.filter(func(servers *serverList) bool {
		return len(servers.servers) == min
	})
}

// smallestGroup return the smallest server group for a class. On contention,
// the first item in the list is returned.
func (s orderedServerGroups) smallestGroup() string {
	return s.smallestGroups()[0]
}

// largestGroups returns a list of the largest server groups.
func (s orderedServerGroups) largestGroups() []string {
	max := s.maxSize()

	return s.filter(func(servers *serverList) bool {
		return len(servers.servers) == max
	})
}

// largestGroup return the largest server group for a class. On contention,
// the last item in the list is returned.
func (s orderedServerGroups) largestGroup() string {
	groups := s.largestGroups()
	return groups[len(groups)-1]
}

// sizeOrderedGroups returns a list of server groups in size order.
func (s orderedServerGroups) sizeOrderedGroups() []string {
	groups := append([]string(nil), *s.groupsOrder...)

	sort.SliceStable(groups, func(i, j int) bool {
		return len(s.getGroup(groups[i]).servers) < len(s.getGroup(groups[j]).servers)
	})

	return groups
}

// serverGroups maps server group names to a list of servers.
type groupMap map[string]*serverList

// NewLexicalServerGroups returns a new lexical server groups. This is a server group
// that returns the smallest or largest server group based on lexical order.
func newLexicalServerGroups() lexicalServerGroups {
	return lexicalServerGroups{
		groupMap: groupMap{},
	}
}

type lexicalServerGroups struct {
	groupMap
}

func (s groupMap) addGroup(name string) {
	s[name] = &serverList{}
}

func (s groupMap) addGroupIfDoesntExist(group string) {
	if _, ok := s[group]; !ok {
		s.addGroup(group)
	}
}

func (s groupMap) getMap() map[string]*serverList {
	return s
}

func (s groupMap) getGroup(name string) *serverList {
	return s[name]
}

func (s groupMap) groupExists(group string) bool {
	_, ok := s[group]
	return ok
}

// sizes returns a list of the size of each server group.
func (s groupMap) sizes() []int {
	// Map from from groups of pods to a list of lengths
	sizes := []int{}

	for _, servers := range s {
		sizes = append(sizes, len(servers.servers))
	}

	return sizes
}

// minSize finds the smallest server group population in the
// provided server group map.
func (s groupMap) minSize() int {
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
func (s groupMap) maxSize() int {
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

// filterGroupsOnSize returns a list of server group names
// based on some predicate based on the list of servers in each group.
func (s groupMap) filter(predicate filterPredicate) []string {
	groups := []string{}

	for group, servers := range s {
		if predicate(servers) {
			groups = append(groups, group)
		}
	}

	return groups
}

func (s lexicalServerGroups) sizeOrderedGroups() []string {
	groups := []string{}

	for group := range s.groupMap {
		groups = append(groups, group)
	}

	// First sort the groups alphabetically
	sort.Strings(groups)

	// Then sort the groups by size
	sort.SliceStable(groups, func(i, j int) bool {
		return len(s.getGroup(groups[i]).servers) < len(s.getGroup(groups[j]).servers)
	})

	return groups
}

// smallestGroups returns an alphabetically sorted list of the smallest server
// groups for a class.
func (s groupMap) smallestGroups() []string {
	min := s.minSize()

	groups := s.filter(func(servers *serverList) bool {
		return len(servers.servers) == min
	})
	sort.Strings(groups)

	return groups
}

// smallestGroup return the smallest server group for a class, returning the
// item with the smallest name on contention.
func (s lexicalServerGroups) smallestGroup() string {
	groups := s.smallestGroups()

	return groups[0]
}

// largestGroups returns an alphabetically sorted list of the largest server
// groups for a class.
func (s groupMap) largestGroups() []string {
	max := s.maxSize()

	return s.filter(func(servers *serverList) bool {
		return len(servers.servers) == max
	})
}

// largestGroup return the largest server group for a class, returning the
// item with the largest name on contention.
func (s lexicalServerGroups) largestGroup() string {
	groups := s.largestGroups()
	sort.Strings(groups)

	return groups[len(groups)-1]
}

// serverClassGroupMap maps server classes to their server groups of pods.
type serverClassGroupMap map[string]serverGroups

// serverRemovalPriorityQueue represents a priority queue for removing pods(servers)
// in either a FIFO way or picking between potential removal candidates.
type serverRemovalPriorityQueue struct {
	servers *list.List
}

func NewServerRemovalPriorityQueue() *serverRemovalPriorityQueue {
	return &serverRemovalPriorityQueue{
		servers: list.New(),
	}
}

// enqueueAll adds all servers(pods) to the end of the queue.
func (q *serverRemovalPriorityQueue) enqueueAll(servers []string) {
	for _, server := range servers {
		q.servers.PushBack(server)
	}
}

// dequeue removes and returns the server(pod) from the front of the queue.
func (q *serverRemovalPriorityQueue) dequeue() string {
	if q.servers.Len() == 0 {
		return ""
	}

	frontElement := q.servers.Front()
	q.servers.Remove(frontElement)

	return frontElement.Value.(string)
}

// dequeHighestRankedCandidate removes and returns the highest ranked server(pod) from the queue along with
// the server group it belongs to.
func (q *serverRemovalPriorityQueue) dequeHighestRankedCandidate(serverLists map[string]*serverList) (string, string) {
	for e := q.servers.Front(); e != nil; e = e.Next() {
		server := e.Value.(string)
		for serverGroup, sl := range serverLists {
			if sl.find(server) {
				q.servers.Remove(e)
				return server, serverGroup
			}
		}
	}

	return "", ""
}

// serverClassServerRemovalMap maps server classes to the serverDeletionQueue FIFO queue.
// N.B. Don't care about serverGroup.
type serverClassServerRemovalMap map[string]*serverRemovalPriorityQueue

func hashString(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))

	return h.Sum64()
}

// ShuffleServerGroups shuffles the server groups in a pseudo-random manner using
// the cluster name as the seed.
func ShuffleServerGroups(groups []string, clusterName string) {
	seed := hashString(clusterName)
	randSrc := rand.NewSource(int64(seed))
	rand.New(randSrc).Shuffle(len(groups), func(i, j int) {
		groups[i], groups[j] = groups[j], groups[i]
	})
}
