package cluster

import (
	"context"
	"fmt"
	"net"
	"reflect"
	"strings"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/cluster/persistence"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/netutil"
	v1 "k8s.io/api/core/v1"
)

// createAlternateAddressesExternal calculates what the current state of the node's alternate
// addresses should be. For public addresses we maintain the default ports, however set the
// alternate address to the DDNS name.  For private addresses these will be an IP based on the
// node address and node ports in the 30000 range.
func (c *Cluster) createAlternateAddressesExternal(member couchbaseutil.Member) (*couchbaseutil.AlternateAddressesExternal, error) {
	var hostname string

	actual, exists := c.k8s.Pods.Get(member.Name())

	if c.cluster.Spec.Networking.DNS != nil {
		// Use the user provided DNS name.
		hostname = k8sutil.GetDNSName(c.cluster, member.Name())
	} else {
		// Lookup the node IP the pod is running on.
		var err error
		hostname, err = k8sutil.GetHostIP(c.k8s, member.Name())
		if err != nil {
			return nil, err
		}
	}

	if exists && actual.Spec.HostNetwork == true && c.cluster.Spec.Networking.DNS == nil {
		hostname = actual.Spec.NodeName
	}

	ports, err := k8sutil.GetAlternateAddressExternalPorts(c.k8s, member.Name())
	if err != nil {
		return nil, err
	}

	addresses := &couchbaseutil.AlternateAddressesExternal{
		Hostname: hostname,
		Ports:    ports,
	}

	return addresses, nil
}

// waitAlternateAddressReachable waits for advertised addresses to become reachable.
// This takes into account the time taken to create an external load balancer and
// DDNS updates.  Obviously this is a best effort as different DNS servers may behave
// differently, and what we see is not necessarily what the client sees.
func waitAlternateAddressReachable(timeout time.Duration, addresses *couchbaseutil.AlternateAddressesExternal) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// All exposed features contain the admin port, only TLS enabled ports
	// are always guaranteed to exist.
	port := 18091

	if addresses.Ports != nil {
		port = int(addresses.Ports.AdminServicePortTLS)
	}

	// If the address is IPv6, wrap it in brackets as per https://golang.org/pkg/net/#Dial.
	hostname := addresses.Hostname

	ip := net.ParseIP(hostname)
	if ip != nil {
		if strings.Contains(ip.String(), ":") {
			hostname = fmt.Sprintf("[%s]", ip.String())
		}
	}

	return netutil.WaitForHostPort(ctx, fmt.Sprintf("%s:%d", hostname, port))
}

// Get alternate addresses from server, when server is
// exposed over LoadBalancer then Ports can be ignored.
func (c *Cluster) getAlternateAddressesExternal(member couchbaseutil.Member) (*couchbaseutil.AlternateAddressesExternal, error) {
	existingAddresses := &couchbaseutil.AlternateAddressesExternal{}
	if err := c.getAlternateAddressesExternalInto(member, existingAddresses); err != nil {
		return nil, err
	}

	if existingAddresses.Hostname == "" {
		return nil, nil
	}

	// Remove Ports if member features are exposed with a loadbalancer
	if svc, found := c.k8s.Services.Get(member.Name()); found {
		if svc.Spec.Type == v1.ServiceTypeLoadBalancer {
			existingAddresses.Ports = nil
		}
	}

	return existingAddresses, nil
}

// getAlternateAddressesExternal gets the alternate addresses for this node.
// It is *NOT* an error condition for this not to exist, which is an indication
// that the client code is not clever enough to handle the snafu.
func (c *Cluster) getAlternateAddressesExternalInto(m couchbaseutil.Member, alternateAddresses *couchbaseutil.AlternateAddressesExternal) error {
	nodeServices := &couchbaseutil.NodeServices{}
	if err := couchbaseutil.GetNodeServices(nodeServices).On(c.api, m); err != nil {
		return err
	}

	for _, node := range nodeServices.NodesExt {
		if !node.ThisNode {
			continue
		}

		if node.AlternateAddresses != nil {
			*alternateAddresses = *node.AlternateAddresses.External
		}

		return nil
	}

	// The absence of this node is probably due to it not being balanced in yet.
	// /pools/default/nodeServices apparently only shows nodes when the rebalance
	// starts.  Don't raise an error.
	return nil
}

// getNodeNetworkConfiguration gets the network configuration settings for a node.
func (c *Cluster) getNodeNetworkConfiguration(m couchbaseutil.Member, s *couchbaseutil.NodeNetworkConfiguration) error {
	node := &couchbaseutil.NodeInfo{}
	if err := couchbaseutil.GetNodesSelf(node).On(c.api, m); err != nil {
		return err
	}

	onOrOff := couchbaseutil.Off

	if node.NodeEncryption {
		onOrOff = couchbaseutil.On
	}

	*s = couchbaseutil.NodeNetworkConfiguration{
		NodeEncryption: onOrOff,
	}

	return nil
}

// addMemberAlternateAddresses adds the alternate addresses to the member.
// It will return false if the alternate addresses are not yet reachable,
// and true if they are. If the cluster's alternate address check delay has not elapsed, or
// the alternate address is not reachable, this will return false and a condition will be added
// to the pod.
func (c *Cluster) addMemberAlternateAddresses(member couchbaseutil.Member, existingAddresses *couchbaseutil.AlternateAddressesExternal) (bool, error) {
	// Get the requested alternate address specification.
	addresses, err := c.createAlternateAddressesExternal(member)
	if err != nil {
		return false, err
	}

	// BUG: MB-49376
	// Kubernetes service always exposes data ports but Couchbase doesn't
	// include these ports in alternative address configs if node service
	// does not explicitly include "data".
	// Therefore for functional correctness, the Kubernetes data ports
	// will be same as existing ports (if specified).
	if c.alternatePortsNeedUpdating(member.Config(), addresses, existingAddresses) {
		c.addDataServiceExternalPorts(addresses.Ports, existingAddresses.Ports)
	}

	pod, found := c.k8s.Pods.Get(member.Name())
	if !found {
		return false, errors.ErrResourceRequired
	}

	// Check to see if we need to perform any updates. If we have already
	// set the alternate address, we can exit before checking the DNS connection and remove
	// the pending DNS flag.
	if reflect.DeepEqual(addresses, existingAddresses) {
		err = k8sutil.RemovePodCondition(c.k8s, pod, k8sutil.PodPendingExternalDNSCondition)
		return false, err
	}

	if !c.hasDNSCheckDelayElapsed(pod) {
		log.Info("Delaying external DNS check", "cluster", c.namespacedName(), "pod", pod.Name, "waitUntil", c.getPodDNSCheckExpiry(pod))
		return false, k8sutil.FlagPodPendingExternalDNS(c.k8s, pod, "Delaying DNS Check")
	}

	// Attempt to reach the alternate address. We will timeout after 5 seconds and try again on the next reconciliation.
	if err := waitAlternateAddressReachable(5*time.Second, addresses); err != nil {
		log.Info("Waiting on DNS Propagation", "cluster", c.namespacedName(), "member", member.Name(), "error", err)
		return false, k8sutil.FlagPodPendingExternalDNS(c.k8s, pod, "Waiting on DNS Propagation")
	}

	err = k8sutil.RemovePodCondition(c.k8s, pod, k8sutil.PodPendingExternalDNSCondition)
	if err != nil {
		return false, err
	}

	log.Info("DNS available, adding alternate addresses", "cluster", c.namespacedName(), "member", member.Name())

	return true, couchbaseutil.SetAlternateAddressesExternal(addresses).On(c.api, member)
}

func (c *Cluster) cleanMembersWithHostnameAAPersistence() error {
	if c.cluster.Spec.Networking.ImprovedHostNetwork {
		return nil
	}

	// If the persistence var is false, we don't need to do anything.
	// If the persistence var doesn't exist, we need to set it to false, as fetching
	// an empty persistence value takes a lot of time due to it retrying.
	persistenceVar, err := c.state.Get(persistence.MembersWithHostnameAAadded)
	if persistenceVar == "false" {
		return nil
	}

	if err == nil {
		for _, member := range c.members {
			if err := couchbaseutil.DeleteAlternateAddressesExternal().On(c.api, member); err != nil {
				return err
			}
		}
	}

	return c.state.Upsert(persistence.MembersWithHostnameAAadded, "false")
}

func (c *Cluster) handleExposedFeatures(member couchbaseutil.Member, existingAddresses *couchbaseutil.AlternateAddressesExternal) error {
	if existingAddresses == nil {
		return nil
	}

	indexes, _ := c.state.Get(persistence.MembersWithHostnameAAadded)

	// If it exists in the indexes, we DON'T need to delete the alternate addresses
	if !couchbaseutil.DoesMemberIndexExistInIndexes(indexes, member.Name()) {
		if err := couchbaseutil.DeleteAlternateAddressesExternal().On(c.api, c.readyMembers()); err != nil {
			return err
		}
	}

	return nil
}

// initMemberAlternateAddresses injects the K8S node's L3 address and alternate
// ports into the requested member.  Clients may use these addresses/ports to
// connect to the cluster if there is no direct L3 connectivity into the pod
// network.
//
//nolint:gocognit
func (c *Cluster) reconcileMemberAlternateAddresses() error {
	if err := c.cleanMembersWithHostnameAAPersistence(); err != nil {
		return err
	}

	var persistenceIndexes string

	usePersistence := c.cluster.Spec.Networking.ImprovedHostNetwork && !c.cluster.Spec.Networking.InitPodsWithNodeHostname
	if usePersistence {
		persistenceIndexes, _ = c.state.Get(persistence.MembersWithHostnameAAadded)
	}

	// Examine each member in turn as they will have different node
	// addresses (i.e. you must be using anti affinity or kubernetes
	// has no way of addressing individual cluster nodes).
	for _, member := range c.members {
		// Skip if the member doesn't have a pod (external member)
		_, exists := c.k8s.Pods.Get(member.Name())
		if !exists {
			continue
		}

		// Grab the current configuration
		existingAddresses, err := c.getAlternateAddressesExternal(member)
		if err != nil {
			// If we cannot make contact then just continue, it may have been deleted
			log.Info("External address collection failed", "cluster", c.namespacedName(), "name", member.Name())
			return nil
		}

		// If we don't have any exposed ports, but the node reports it is configured so
		// then remove the configuration.
		if !c.cluster.Spec.HasExposedFeatures() {
			if err := c.handleExposedFeatures(member, existingAddresses); err != nil {
				return err
			}

			continue
		}

		// Perform the update if the member is not in the persistence indexes. If we are not using persistence,
		// we will always perform the update as the persistenceIndex will be a zero value.
		if !couchbaseutil.DoesMemberIndexExistInIndexes(persistenceIndexes, member.Name()) {
			dnsReached, err := c.addMemberAlternateAddresses(member, existingAddresses)
			if err != nil {
				return err
			}

			if dnsReached && usePersistence {
				persistenceIndexes, err = couchbaseutil.AddMemberIndexToIndexList(persistenceIndexes, member.Name())
				if err != nil {
					return err
				}
			}
		}
	}

	if usePersistence {
		if err := c.state.Upsert(persistence.MembersWithHostnameAAadded, persistenceIndexes); err != nil {
			return err
		}
	}

	return nil
}

// alternatePortsNeedUpdating checks if existing alternate ports need to be updated.
// The result is 'true' if the config uses alternate ports associated with a non-kv member.
func (c *Cluster) alternatePortsNeedUpdating(config string, requested *couchbaseutil.AlternateAddressesExternal, existing *couchbaseutil.AlternateAddressesExternal) bool {
	if c.cluster.Spec.ConfigHasDataService(config) || c.cluster.Spec.Networking.ExposedFeatureServiceTemplate == nil || requested == nil || existing == nil {
		return false
	}

	return c.cluster.Spec.Networking.ExposedFeatureServiceTemplate.Spec.Type == v1.ServiceTypeNodePort
}

// addDataServiceExternalPorts adds data service ports to existing alternative address ports.
func (c *Cluster) addDataServiceExternalPorts(k8sAddressPorts *couchbaseutil.AlternateAddressesExternalPorts, existingAddressPorts *couchbaseutil.AlternateAddressesExternalPorts) {
	existingAddressPorts.DataServicePort = k8sAddressPorts.DataServicePort
	existingAddressPorts.DataServicePortTLS = k8sAddressPorts.DataServicePortTLS
	existingAddressPorts.ViewAndXDCRServicePort = k8sAddressPorts.ViewAndXDCRServicePort
	existingAddressPorts.ViewAndXDCRServicePortTLS = k8sAddressPorts.ViewAndXDCRServicePortTLS
}

// supportsAFFiltering tells us whether we can support address familiy filtering
// or not e.g. only the requested familiy shows up, rather than the normal
// dual-stack stuff.
func (c *Cluster) supportsAFFiltering() bool {
	lowestImage, err := c.cluster.Spec.LowestInUseCouchbaseVersionImage()
	if err != nil {
		return false
	}

	tag, err := k8sutil.CouchbaseVersion(lowestImage)
	if err != nil {
		return false
	}

	version, err := couchbaseutil.NewVersion(tag)
	if err != nil {
		return false
	}

	return version.GreaterEqualString("7.0.2")
}

// AddressFamilyResourceToCouchbase translates from our consistent API to Couchbase's.
func addressFamilyResourceToCouchbase(af couchbasev2.AddressFamily) (couchbaseutil.AddressFamily, bool) {
	switch af {
	case couchbasev2.IPv4, couchbasev2.IPv4Only:
		return couchbaseutil.AddressFamilyIPV4, true
	case couchbasev2.IPv6, couchbasev2.IPv6Only:
		return couchbaseutil.AddressFamilyIPV6, true
	case couchbasev2.IPv6Priority:
		return couchbaseutil.AddressFamilyIPV6, false
	default:
		return couchbaseutil.AddressFamilyIPV4, false
	}
}

// generateNetworkConfiguration generates the required network configuration for
// the requested cluster configuration.
func (c *Cluster) generateNetworkConfiguration() *couchbaseutil.NodeNetworkConfiguration {
	addressFamily, addressFamilyOnly := addressFamilyResourceToCouchbase(c.cluster.AddressFamily())
	networkConfiguration := &couchbaseutil.NodeNetworkConfiguration{
		AddressFamily: addressFamily,
	}

	if c.supportsAFFiltering() {
		networkConfiguration.AddressFamilyOnly = &addressFamilyOnly
	}

	return networkConfiguration
}

// initMemberNetworking sets up IP address families on pod start up.  Sadly you cannot also
// setup node-to-node networking because Server will collapse in a heap.  Apply all configuration
// blindly because we cannot see what Couchbase's configuration is at this time...
func (c *Cluster) initMemberNetworking(member couchbaseutil.Member) error {
	// Apparently setting the cluster's configuration to IPv6 is not enough for it
	// to work, there is some other magical -- and completely undocumented -- thing
	// called a listener that needs to be manually turned on.  Why?  Surely telling
	// it to use IPv6 is enough right?
	addressFamily, _ := addressFamilyResourceToCouchbase(c.cluster.AddressFamily())
	listenerSettings := &couchbaseutil.ListenerConfiguration{
		AddressFamily: addressFamily,
	}

	if err := couchbaseutil.EnableExternalListener(listenerSettings).InPlaintext().RetryFor(10*time.Second).On(c.api, member); err != nil {
		return err
	}

	return couchbaseutil.SetNodeNetworkConfiguration(c.generateNetworkConfiguration()).InPlaintext().RetryFor(10*time.Second).On(c.api, member)
}

// reconcileClusterNetworking allows a read/modify/write version of the above.
func (c *Cluster) reconcileClusterNetworking() error {
	// Work out what ne need the networking to look like.
	requestedNetworkConfiguration := c.generateNetworkConfiguration()

	// Poll Couchbase for the current network configuration, keeping record
	// of any members whose configuration does not match what is expected.
	var info couchbaseutil.ClusterInfo

	if err := couchbaseutil.GetPoolsDefault(&info).On(c.api, c.members); err != nil {
		return err
	}

	var updateMembers []couchbaseutil.Member

	for _, member := range c.members {
		node, err := info.GetNode(member.GetHostName())
		if err != nil {
			return err
		}

		currentNetworkConfiguration := &couchbaseutil.NodeNetworkConfiguration{
			AddressFamily: node.AddressFamily.ConvertAddressFamilyOutToAddressFamily(),
		}

		if c.supportsAFFiltering() {
			currentNetworkConfiguration.AddressFamilyOnly = &node.AddressFamilyOnly
		}

		if reflect.DeepEqual(currentNetworkConfiguration, requestedNetworkConfiguration) {
			continue
		}

		updateMembers = append(updateMembers, member)
	}

	// Nothing to do, move along...
	if len(updateMembers) == 0 {
		return nil
	}

	// For some reason you need to disable failover because server is
	// incapable of doing this itself.  Perhaps it's because updating settings causes
	// a failover because it's slow or broken in some repect?
	failoverSettings := &couchbaseutil.AutoFailoverSettings{}
	if err := couchbaseutil.GetAutoFailoverSettings(failoverSettings).On(c.api, c.readyMembers()); err != nil {
		return err
	}

	if failoverSettings.Enabled {
		newFailoverSettings := *failoverSettings
		newFailoverSettings.Enabled = false

		if err := couchbaseutil.SetAutoFailoverSettings(&newFailoverSettings).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		defer func() {
			_ = couchbaseutil.SetAutoFailoverSettings(failoverSettings).On(c.api, c.readyMembers())
		}()
	}

	for _, member := range updateMembers {
		// Retry this because server doesn't attempt to actually perform the failover
		// update operation, it simply says "200 okay!", which is a lie.
		if err := couchbaseutil.SetNodeNetworkConfiguration(requestedNetworkConfiguration).RetryFor(time.Minute).On(c.api, member); err != nil {
			return err
		}
	}

	c.raiseEvent(k8sutil.NetworkSettingsModifiedEvent(c.cluster))

	return nil
}

// hasDNSCheckDelayElapsed checks if the DNS check delay has elapsed.
func (c *Cluster) hasDNSCheckDelayElapsed(pod *v1.Pod) bool {
	if c.cluster.Spec.Networking.WaitForAddressReachableDelay == nil {
		return true
	}

	if time.Now().After(c.getPodDNSCheckExpiry(pod)) {
		return true
	}

	return false
}

func (c *Cluster) getPodDNSCheckExpiry(pod *v1.Pod) time.Time {
	return pod.CreationTimestamp.Time.Add(c.cluster.Spec.Networking.WaitForAddressReachableDelay.Duration)
}

func (c *Cluster) hasDNSCheckTimeoutElapsed(pod *v1.Pod) bool {
	if c.cluster.Spec.Networking.WaitForAddressReachable == nil {
		return false
	}

	timeoutDuration := c.cluster.Spec.Networking.WaitForAddressReachable.Duration
	timeoutAfter := k8sutil.GetPendingTransitionTime(pod).Add(timeoutDuration)

	if time.Now().After(timeoutAfter) {
		log.Info("Timed out waiting for DNS Propagation", "cluster", c.namespacedName(), "pod", pod.Name, "timeoutAfter", timeoutAfter)
		return true
	}

	return false
}
