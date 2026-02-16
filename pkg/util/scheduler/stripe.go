package scheduler

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"sync"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/astar"
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

	// unschedulableServerClasses is for tracking pods that exist, but that aren't
	// scheduled for some reason, either we're enabling schduling or the server
	// group is no longer valid.
	unschedulableServerClasses serverClassGroupMap

	// removableUnscheduledServerClasses is for tracking pods that exist, but that aren't
	// part of the cluster spec and therefore are allowed to be removed
	// when determining which server group to remove a pod from.
	removableUnscheduledServerClasses serverClassGroupMap

	// removableServerClasses is for tracking our internal state of ordered removal
	// of pods by server classes
	removableServerClasses serverClassServerRemovalMap

	// avoidGroups is a list of server groups to avoid when scheduling.
	avoidGroups map[string]bool

	// mu allows us to lock/unlock goroutines that run concurrently if we need to avoid race conditions when accessing
	// the scheduler cache
	mu sync.Mutex
}

// getServerGroupsForClass gets the list of server groups to schedule pods across
// given a specific server class name.
func GetServerGroupsForClass(cluster *couchbasev2.CouchbaseCluster, class *couchbasev2.ServerConfig) ([]string, error) {
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

// initServerClasses populates the scheduler with all server classes that
// are defined in the specification.
func (sched *stripeSchedulerImpl) initServerClasses(cluster *couchbasev2.CouchbaseCluster) error {
	sched.avoidGroups = map[string]bool{}

	for i := range cluster.Spec.Servers {
		class := cluster.Spec.Servers[i]

		groups, err := GetServerGroupsForClass(cluster, &class)
		if err != nil {
			return err
		}

		if cluster.Spec.ShuffleServerGroups {
			// Copy slice so we're not messing with the original
			groups = append([]string(nil), groups...)

			ShuffleServerGroups(groups, cluster.NamespacedName())

			sched.serverClasses[class.Name] = newOrderedServerGroups()
		} else {
			sched.serverClasses[class.Name] = newLexicalServerGroups()
		}

		for _, group := range groups {
			sched.serverClasses[class.Name].addGroup(group)
		}
	}

	return nil
}

// getPodServerGroup extracts the pod's server group from the specification.
// With hindsight, this should have been metadata e.g. an annotation.
func getPodServerGroup(pod *v1.Pod) (string, error) {
	if pod.Spec.NodeSelector == nil {
		return "", fmt.Errorf("%w: node selector not set", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	group, ok := pod.Spec.NodeSelector[constants.ServerGroupLabel]
	if ok {
		return group, nil
	}

	// During an upgrade to 2.3 or higher, we need to fall back to
	// the old beta label.
	group, ok = pod.Spec.NodeSelector[v1.LabelFailureDomainBetaZone]
	if ok {
		return group, nil
	}

	return "", fmt.Errorf("%w: node selector not as expected", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
}

// populateServerClasses populates the server group lists with pods.
func (sched *stripeSchedulerImpl) populateServerClasses(pods []*v1.Pod) error {
	for _, pod := range pods {
		// Pod is faulty ignore it
		if pod.Status.Phase != v1.PodPending && pod.Status.Phase != v1.PodRunning {
			continue
		}

		class, ok := pod.Labels[constants.LabelNodeConf]
		if !ok {
			return fmt.Errorf("%s: pod %s does not have server class label: %w", stripeErrorHeader, pod.Name, errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
		}

		// Class deleted, ignore the pod
		if _, ok := sched.serverClasses[class]; !ok {
			continue
		}

		// Pod has no scheduling information... we're upgrading from a non-scheduled
		// to a scheduled cluster.
		group, err := getPodServerGroup(pod)
		if err != nil {
			if _, ok := sched.unschedulableServerClasses[class]; !ok {
				sched.unschedulableServerClasses[class] = newLexicalServerGroups()
			}

			sched.unschedulableServerClasses[class].addGroupIfDoesntExist(group)
			sched.unschedulableServerClasses[class].getGroup(group).push(pod.Name)

			continue
		}

		// Pod is not part of an active server class... we're migrating server classes.
		if !sched.serverClasses[class].groupExists(group) {
			if _, ok := sched.unschedulableServerClasses[class]; !ok {
				sched.unschedulableServerClasses[class] = newLexicalServerGroups()
			}

			sched.unschedulableServerClasses[class].addGroupIfDoesntExist(group)
			sched.unschedulableServerClasses[class].getGroup(group).push(pod.Name)

			// Given the pod exists but is no longer required by the spec, we should consider it for removal
			// when determining which server group to remove a pod from. This differs to unschedulableServerClasses
			// which includes pods that may not exist or have a marked server group.
			if _, ok := sched.removableUnscheduledServerClasses[class]; !ok {
				sched.removableUnscheduledServerClasses[class] = newLexicalServerGroups()
			}

			sched.removableUnscheduledServerClasses[class].addGroupIfDoesntExist(group)
			sched.removableUnscheduledServerClasses[class].getGroup(group).push(pod.Name)

			continue
		}

		sched.serverClasses[class].getGroup(group).push(pod.Name)
	}

	return nil
}

// NewStripeScheduler creates an initializes a new stripe scheduler, caching
// state from the current set of pods for the cluster.
func NewStripeScheduler(pods []*v1.Pod, cluster *couchbasev2.CouchbaseCluster) (Scheduler, error) {
	// Initialize data structures, creating maps for each server class
	// and empty lists for each server group defined for that class
	sched := &stripeSchedulerImpl{
		serverClasses:                     serverClassGroupMap{},
		unschedulableServerClasses:        serverClassGroupMap{},
		removableUnscheduledServerClasses: serverClassGroupMap{},
		removableServerClasses:            serverClassServerRemovalMap{},
	}

	if err := sched.initServerClasses(cluster); err != nil {
		return nil, err
	}

	if err := sched.populateServerClasses(pods); err != nil {
		return nil, err
	}

	return sched, nil
}

// Create inspects the pod and determines the server configuration, it is then able
// to select either server configuration specific server groups or default to the
// global configuration.  Pods in this server configuration are listed and mapped
// to the set of server groups.  To schedule we pick the set of server groups which
// contain the fewest pods, then deterministically select the smallest
// before labelling the pod with this server group as a label selector.
func (sched *stripeSchedulerImpl) Create(class, name, group string) (string, error) {
	if _, ok := sched.serverClasses[class]; !ok {
		return "", fmt.Errorf("%s: pod %s server class '%s' undefined: %w", stripeErrorHeader, name, class, errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	// When calculating the server class to use, we need to consider multiple goroutines accessing the scheduler cache simultaneously.
	sched.mu.Lock()
	defer sched.mu.Unlock()

	if group == "" {
		group = sched.getSmallesGroupForClass(class)
	}

	classServerList := sched.serverClasses[class].getGroup(group)

	if classServerList == nil {
		return "", fmt.Errorf("%s: pod %s server class '%s' group '%s' has no server list: %w", stripeErrorHeader, name, class, group, errors.NewStackTracedError(errors.ErrResourceRequired))
	}

	classServerList.push(name)

	return group, nil
}

// removePodFromServerGroup removes a pod from either or both scheduled and removable unscheduled server classes.
func (sched *stripeSchedulerImpl) removePodFromServerGroup(class, serverGroup, podName string) error {
	// Try scheduled server classes first
	if serverList := sched.serverClasses[class].getGroup(serverGroup); serverList != nil {
		if serverList.find(podName) {
			return serverList.del(podName)
		}
	}

	// Try unscheduled server classes
	if sched.removableUnscheduledServerClasses[class] != nil {
		if serverList := sched.removableUnscheduledServerClasses[class].getGroup(serverGroup); serverList != nil {
			if serverList.find(podName) {
				return serverList.del(podName)
			}
		}
	}

	return fmt.Errorf("%s: server named %s could not be deleted for class %s and server group %s: %w", stripeErrorHeader, podName, class, serverGroup, errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
}

func (sched *stripeSchedulerImpl) getSmallesGroupForClass(class string) string {
	if len(sched.avoidGroups) == 0 {
		return sched.serverClasses[class].smallestGroup()
	}

	orderedGroups := sched.serverClasses[class].sizeOrderedGroups()

	for _, group := range orderedGroups {
		if _, ok := sched.avoidGroups[group]; !ok {
			return group
		}
	}

	// We can't find any that we can't avoid so pick randomly
	return orderedGroups[rand.Intn(len(orderedGroups))]
}

func (sched *stripeSchedulerImpl) Delete(class string) (string, error) {
	// Select the victim server group based on population
	if _, ok := sched.serverClasses[class]; !ok {
		return "", fmt.Errorf("%s: no server list present for server class '%s': %w", stripeErrorHeader, class, errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	var podName string

	// Victims are selected based on priority order of the backing serverRemovalPriorityQueue.
	rq, rqFound := sched.removableServerClasses[class]

	// If we have a removal queue, we use that to prioritize the removals
	if rqFound {
		largestGroups := sched.serverClasses[class].largestGroups()

		removalServerListsMap := map[string]*serverList{}

		// Get all the possible removal candidates
		for _, group := range largestGroups {
			removalServerListsMap[group] = sched.serverClasses[class].getGroup(group)
		}

		if sched.removableUnscheduledServerClasses[class] != nil {
			largestUnscheduledGroups := sched.removableUnscheduledServerClasses[class].largestGroups()
			for _, group := range largestUnscheduledGroups {
				removalServerListsMap[group] = sched.removableUnscheduledServerClasses[class].getGroup(group)
			}
		}

		var serverGroup string
		// Find the highest priority node in the list of candidates
		podName, serverGroup = rq.dequeHighestRankedCandidate(removalServerListsMap)
		if podName != "" {
			if err := sched.removePodFromServerGroup(class, serverGroup, podName); err != nil {
				return "", err
			}

			return podName, nil
		}
	}

	// If the rq hasn't prioritized any thing then select the victim server
	// deterministically based on alphabetical order.
	serverGroup := sched.serverClasses[class].largestGroup()

	server, err := sched.serverClasses[class].getGroup(serverGroup).pop()
	if err != nil {
		return "", fmt.Errorf("%s: no server list found for server class '%s': %w", stripeErrorHeader, class, errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	return server, nil
}

// Upgrade removes a node from the scheduler as it's an upgrade target.
func (sched *stripeSchedulerImpl) Upgrade(class, name string) error {
	if serverClasses := sched.serverClasses[class]; serverClasses != nil {
		for serverGroup := range serverClasses.getMap() {
			if err := serverClasses.getGroup(serverGroup).del(name); err == nil {
				return nil
			}
		}
	}

	if unschedulableClass := sched.unschedulableServerClasses[class]; unschedulableClass != nil {
		for serverGroup := range unschedulableClass.getMap() {
			if err := unschedulableClass.getGroup(serverGroup).del(name); err == nil {
				return nil
			}
		}
	}

	return fmt.Errorf("%s: server '%s' does not exist in class '%s': %w", stripeErrorHeader, name, class, errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
}

// EnQueueRemovals sets the list server names per serve class name for in-order removal.
func (sched *stripeSchedulerImpl) EnQueueRemovals(class string, servers []string) {
	if _, ok := sched.removableServerClasses[class]; !ok {
		sched.removableServerClasses[class] = NewServerRemovalPriorityQueue()
		sched.removableServerClasses[class].enqueueAll(servers)
	} else {
		sched.removableServerClasses[class].enqueueAll(servers)
	}
}

// Member records information about a cluster member.
type Member struct {
	// ServerGroup records the server group a member is currently in.
	// Leave this empty for a sentinel that means it has no scheduling
	// information.
	ServerGroup string

	// Moved indicates whether this member has already moved.  It should
	// be possible to balance members across server groups in a single
	// move.
	Moved bool
}

// clone deep copies a member.
func (m *Member) clone() *Member {
	t := *m
	return &t
}

// ServerGroup records information about a server group.
type ServerGroup struct {
	// Size is the number of members in this group.
	Size int

	// Unschedulable means that the server group is known about but is
	// not desired to be used.
	Unschedulable bool
}

// clone deep copies a server group.
func (b *ServerGroup) clone() *ServerGroup {
	t := *b
	return &t
}

// state implments our AStarable game state.
type state struct {
	// Members maps member names to member information.
	Members map[string]*Member

	// ServerGroups maps server group names to group information.
	ServerGroups map[string]*ServerGroup

	// Moves recores the moves required to get to this state.
	Moves []Move
}

// newState returns a new initial game state for the given scheduler and server class.
func newState(sched *stripeSchedulerImpl, class string) *state {
	s := &state{
		Members:      map[string]*Member{},
		ServerGroups: map[string]*ServerGroup{},
	}

	if groups, ok := sched.serverClasses[class]; ok {
		for group, members := range groups.getMap() {
			if _, ok := s.ServerGroups[group]; !ok {
				s.ServerGroups[group] = &ServerGroup{}
			}

			for _, member := range members.servers {
				s.Members[member] = &Member{
					ServerGroup: group,
				}

				s.ServerGroups[group].Size++
			}
		}
	}

	if groups, ok := sched.unschedulableServerClasses[class]; ok {
		for group, members := range groups.getMap() {
			if _, ok := s.ServerGroups[group]; !ok {
				s.ServerGroups[group] = &ServerGroup{
					Unschedulable: true,
				}
			}

			for _, member := range members.servers {
				s.Members[member] = &Member{
					ServerGroup: group,
				}

				s.ServerGroups[group].Size++
			}
		}
	}

	return s
}

// clone deep copies the state.
func (s *state) clone() *state {
	c := &state{
		Members:      map[string]*Member{},
		ServerGroups: map[string]*ServerGroup{},
		Moves:        make([]Move, len(s.Moves)),
	}

	for k, v := range s.Members {
		c.Members[k] = v.clone()
	}

	for k, v := range s.ServerGroups {
		c.ServerGroups[k] = v.clone()
	}

	copy(c.Moves, s.Moves)

	return c
}

// score is used to "score" the state, e.g. how close to a solution are we.
// We use this to converge on a winning solution.
func (s *state) score() (min, max int, unschedulable bool) {
	// Initaalize the minimum to the largest possible integer, so we are
	// guaranteed everything else real will be smaller than it.
	min = constants.IntMax

	for _, serverGroup := range s.ServerGroups {
		// Ignore the size for unschedulable groups, as this shouldn't
		// be used for scheduling/scoring, but do indicate it's not okay
		// and needs fixing.
		if serverGroup.Unschedulable {
			if serverGroup.Size > 0 {
				unschedulable = true
			}

			continue
		}

		if serverGroup.Size > max {
			max = serverGroup.Size
		}

		if serverGroup.Size < min {
			min = serverGroup.Size
		}
	}

	return
}

// Done indicates the state is a winning solution, in this case when
// all server groups are balanced -- the difference in magnitude must be no
// greater than one.
func (s *state) Done() bool {
	min, max, unschedulable := s.score()
	if unschedulable {
		return false
	}

	return (max - min) < 2
}

// Hash returns a unique scalar representing the state, so we only
// ever interrogate it once.
func (s *state) Hash() string {
	// Only concern ourselves with the size of the serverGroups, we should not
	// need to bother with repeating moves from serverGroup to serverGroup but with
	// different members.
	raw, err := json.Marshal(s.ServerGroups)
	if err != nil {
		panic(err)
	}

	return string(raw)
}

// Move performs a member movement into another server group that moves us
// nearer to the solution state.
func (s *state) Move() []astar.AStarable {
	// Calculate the "score" for this move, selecting the largest and smallest
	// server group size that is also allowed to be used.
	min, max, _ := s.score()

	moves := []astar.AStarable{}

	// Members in a server group that isn't schedulable must move to the smallest
	// one that is.  This takes precedence over normal rebalancing.
	for name, member := range s.Members {
		// Ignore moved pieces and those that are already on schedulable server groups.
		if member.Moved || !s.ServerGroups[member.ServerGroup].Unschedulable {
			continue
		}

		// Try move each unmoved, unschedulable, member to one of the
		// smallest schedulable server groups.
		for serverGroupName, serverGroup := range s.ServerGroups {
			if serverGroup.Unschedulable || serverGroup.Size != min {
				continue
			}

			next := s.clone()
			next.Members[name].ServerGroup = serverGroupName
			next.Members[name].Moved = true
			next.ServerGroups[member.ServerGroup].Size--
			next.ServerGroups[serverGroupName].Size++

			m := Move{
				Name: name,
				From: member.ServerGroup,
				To:   serverGroupName,
			}

			next.Moves = append(next.Moves, m)

			moves = append(moves, next)
		}
	}

	if len(moves) != 0 {
		return moves
	}

	// Once all the unschedulable members have been moved, we can restore balance
	// (to the Force), and move members from the biggest server groups, to the
	// smallest.
	for name, member := range s.Members {
		// Ignore moved pieces and those that aren't in the largest server groups.
		if member.Moved || s.ServerGroups[member.ServerGroup].Size != max {
			continue
		}

		// Try move each unmoved member from the largest to the smallest
		// schedulable server groups.
		for serverGroupName, serverGroup := range s.ServerGroups {
			// Select the serverGroup only if it is a minimum set.
			if serverGroup.Unschedulable || serverGroup.Size != min {
				continue
			}

			next := s.clone()
			next.Members[name].ServerGroup = serverGroupName
			next.Members[name].Moved = true
			next.ServerGroups[member.ServerGroup].Size--
			next.ServerGroups[serverGroupName].Size++

			m := Move{
				Name: name,
				From: member.ServerGroup,
				To:   serverGroupName,
			}

			next.Moves = append(next.Moves, m)

			moves = append(moves, next)
		}
	}

	return moves
}

// Reschedule looks at the current state, and if it doesn't match that
// requested when the scheduler was initialized, return a set of mutually
// exclusive moves to get us back into a conforming state.
func (sched *stripeSchedulerImpl) Reschedule() ([]Move, error) {
	var moves []Move

	knownClasses := map[string]interface{}{}

	for class := range sched.serverClasses {
		knownClasses[class] = nil
	}

	for class := range sched.unschedulableServerClasses {
		knownClasses[class] = nil
	}

	for class := range knownClasses {
		s := newState(sched, class)

		if s.Done() {
			continue
		}

		result, err := astar.AStar(s)
		if err != nil {
			return nil, err
		}

		moves = append(moves, result.(*state).Moves...)
	}

	return moves, nil
}

// RescheduleUnschedulableOnly performs a simplified reschedule that only moves
// pods from unschedulable server groups to valid server groups. This is used
// in unstable cluster mode where we want to rescue pods from removed server
// groups without doing full cluster rebalancing.
func (sched *stripeSchedulerImpl) RescheduleUnschedulableOnly() ([]Move, error) {
	var moves []Move

	// Only process classes that have unschedulable pods
	for class := range sched.unschedulableServerClasses {
		// Create state for this class
		s := newState(sched, class)

		// Check if there are any unschedulable pods that need moving
		hasUnschedulablePods := false

		for _, serverGroup := range s.ServerGroups {
			if serverGroup.Unschedulable && serverGroup.Size > 0 {
				hasUnschedulablePods = true
				break
			}
		}

		if !hasUnschedulablePods {
			continue
		}

		// Generate moves only for unschedulable pods
		classMoves := sched.generateUnschedulableMoves(s)
		moves = append(moves, classMoves...)
	}

	return moves, nil
}

// generateUnschedulableMoves creates moves to relocate pods from unschedulable
// server groups to the smallest available schedulable server groups.
func (sched *stripeSchedulerImpl) generateUnschedulableMoves(s *state) []Move {
	var moves []Move

	// Find the minimum size among schedulable server groups
	minSize := constants.IntMax
	for _, serverGroup := range s.ServerGroups {
		if !serverGroup.Unschedulable && serverGroup.Size < minSize {
			minSize = serverGroup.Size
		}
	}

	// If no schedulable groups exist, we can't move anything
	if minSize == constants.IntMax {
		return moves
	}

	// Move each pod from unschedulable groups to smallest schedulable groups
	for memberName, member := range s.Members {
		// Only move pods that are in unschedulable server groups
		if !s.ServerGroups[member.ServerGroup].Unschedulable {
			continue
		}

		// Find a schedulable server group with minimum size to move to
		for serverGroupName, serverGroup := range s.ServerGroups {
			if serverGroup.Unschedulable || serverGroup.Size != minSize {
				continue
			}

			// Create the move
			move := Move{
				Name: memberName,
				From: member.ServerGroup,
				To:   serverGroupName,
			}

			moves = append(moves, move)

			// Update the size for next iteration (simple load balancing)
			serverGroup.Size++
			minSize = serverGroup.Size

			// Move to next member
			break
		}
	}

	return moves
}

// LogStatus writes formatted state for debugging.
func (sched *stripeSchedulerImpl) LogStatus(cluster string) {
	// Remember the classes/groups so we can do an ordered/sorted traversal
	mapClass := map[string]interface{}{}
	mapGroup := map[string]interface{}{}

	for class, groups := range sched.serverClasses {
		mapClass[class] = nil

		for group := range groups.getMap() {
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
			servers := sched.serverClasses[class].getGroup(group)
			if servers == nil {
				continue
			}

			servers.sort()

			for _, server := range servers.servers {
				log.Info("Scheduler status", "cluster", cluster, "name", server, "class", class, "group", group)
			}
		}
	}

	for class, groups := range sched.unschedulableServerClasses {
		for group, servers := range groups.getMap() {
			for _, server := range servers.servers {
				log.Info("Scheduler status (unschedulable)", "cluster", cluster, "name", server, "class", class, "group", group)
			}
		}
	}
}

func (sched *stripeSchedulerImpl) AvoidGroups(groups ...string) {
	// Avoid concurrent map writes when multiple goroutines call AvoidGroups
	// during parallel pod creation. Use the scheduler mutex to protect access.
	sched.mu.Lock()
	defer sched.mu.Unlock()

	for _, group := range groups {
		sched.avoidGroups[group] = true
	}
}
