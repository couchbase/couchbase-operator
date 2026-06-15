/*
Copyright 2021-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

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
		c.log.Info("Delaying external DNS check", "cluster", c.namespacedName(), "pod", pod.Name, "waitUntil", c.getPodDNSCheckExpiry(pod))
		return false, k8sutil.FlagPodPendingExternalDNS(c.k8s, pod, "Delaying DNS Check")
	}

	// Attempt to reach the alternate address. We will timeout after 5 seconds and try again on the next reconciliation.
	if err := waitAlternateAddressReachable(5*time.Second, addresses); err != nil {
		c.log.Info("Waiting on DNS Propagation", "cluster", c.namespacedName(), "member", member.Name(), "error", err)
		return false, k8sutil.FlagPodPendingExternalDNS(c.k8s, pod, "Waiting on DNS Propagation")
	}

	err = k8sutil.RemovePodCondition(c.k8s, pod, k8sutil.PodPendingExternalDNSCondition)
	if err != nil {
		return false, err
	}

	c.log.Info("DNS available, adding alternate addresses", "cluster", c.namespacedName(), "member", member.Name())

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
			c.log.Info("External address collection failed", "cluster", c.namespacedName(), "name", member.Name())
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
func (c *Cluster) generateNetworkConfiguration(onInit bool) (*couchbaseutil.NodeNetworkConfiguration, []*couchbaseutil.ListenerConfiguration) {
	addressFamily, addressFamilyOnly := addressFamilyResourceToCouchbase(c.cluster.AddressFamily())
	supportsAFFiltering := c.supportsAFFiltering()

	networkConfiguration := &couchbaseutil.NodeNetworkConfiguration{
		AddressFamily: addressFamily,
	}

	// Adding members during cluster startup/mid network migration can cause issues with address family filtering.
	// To avoid this, we will not set address family only during member init, and rely on the reconcile loop to fix up the settings
	// once a member is part of the cluster.
	if onInit {
		addressFamilyOnly = false
	}

	if supportsAFFiltering {
		networkConfiguration.AddressFamilyOnly = &addressFamilyOnly
	}

	listenerSettings := []*couchbaseutil.ListenerConfiguration{{
		AddressFamily: addressFamily,
	}}

	return networkConfiguration, listenerSettings
}

// initMemberNetworking sets up IP address families on pod start up.  Sadly you cannot also
// setup node-to-node networking because Server will collapse in a heap.  Apply all configuration
// blindly because we cannot see what Couchbase's configuration is at this time...
func (c *Cluster) initMemberNetworking(member couchbaseutil.Member) error {
	// Apparently setting the cluster's configuration to IPv6 is not enough for it
	// to work, there is some other magical -- and completely undocumented -- thing
	// called a listener that needs to be manually turned on.  Why?  Surely telling
	// it to use IPv6 is enough right?
	networkConfiguration, listenerSettings := c.generateNetworkConfiguration(true)

	for _, listenerSetting := range listenerSettings {
		if err := couchbaseutil.EnableExternalListener(listenerSetting).InPlaintext().RetryFor(10*time.Second).On(c.api, member); err != nil {
			return err
		}
	}

	return couchbaseutil.SetNodeNetworkConfiguration(networkConfiguration).InPlaintext().RetryFor(10*time.Second).On(c.api, member)
}

// detectNetworkConfigurationChanges checks the current network configuration of the cluster and compares it to the desired configuration, returning a list of members that need to be updated, and what updates they need.
func (c *Cluster) detectNetworkConfigurationChanges(requestedNetworkConfiguration *couchbaseutil.NodeNetworkConfiguration, requestedListenerConfiguration []*couchbaseutil.ListenerConfiguration) ([]couchbaseutil.Member, map[string][]*couchbaseutil.ListenerConfiguration, error) {
	var info couchbaseutil.ClusterInfo
	if err := couchbaseutil.GetPoolsDefault(&info).On(c.api, c.members); err != nil {
		return nil, nil, err
	}

	var updateMembers []couchbaseutil.Member
	enableMemberListeners := map[string][]*couchbaseutil.ListenerConfiguration{}

	for _, member := range c.members {
		// Skip members that have not been CBS-added yet (async initialization pending)
		// or are in the process of being deleted. Both can appear in c.members transiently:
		// In both cases, info.GetNode would fail with "requested resource not found".
		if pod, ok := c.k8s.Pods.Get(member.Name()); ok &&
			(pod.DeletionTimestamp != nil || k8sutil.HasPendingInitializationCondition(pod)) {
			continue
		}

		node, err := info.GetNode(member.GetHostName())
		if err != nil {
			return nil, nil, err
		}

		currentNetworkConfiguration := &couchbaseutil.NodeNetworkConfiguration{
			AddressFamily: node.AddressFamily.ConvertAddressFamilyOutToAddressFamily(),
			// N2N is controlled in a different reconcile step, so we'll equalize them here to avoid updating unnecessarily.
			// Requested will be empty so should not be sent due to omitEmpty.
			NodeEncryption: requestedNetworkConfiguration.NodeEncryption,
		}

		if c.supportsAFFiltering() {
			currentNetworkConfiguration.AddressFamilyOnly = &node.AddressFamilyOnly
		}

		if reflect.DeepEqual(currentNetworkConfiguration, requestedNetworkConfiguration) {
			continue
		}

		// Find the listeners we need to enable.
		memberEnableListeners := []*couchbaseutil.ListenerConfiguration{}
		for _, requiredListener := range requestedListenerConfiguration {
			if containsExternalListenerConfiguration(node.ExternalListeners, requiredListener) {
				continue
			}

			memberEnableListeners = append(memberEnableListeners, requiredListener)
		}

		enableMemberListeners[member.Name()] = memberEnableListeners
		updateMembers = append(updateMembers, member)
	}

	return updateMembers, enableMemberListeners, nil
}

// reconcileClusterNetworking updates the address family and listener configuration for the cluster.
// This method should only update address family (ipv4 <-> ipv6). It should not change node-to-node encryption settings (other than the mndatory disable).
func (c *Cluster) reconcileClusterNetworking() error {
	requestedNetworkConfiguration, requestedListenerConfiguration := c.generateNetworkConfiguration(false)

	// Poll Couchbase for the current network configuration, keeping record
	// of any members whose configuration does not match what is expected.
	// Server has an endpoint for updating network config, enabling listeners and disabling listeners, so we need to work out which members need which updates.
	updateMembers, enableMemberListeners, err := c.detectNetworkConfigurationChanges(requestedNetworkConfiguration, requestedListenerConfiguration)
	if err != nil || len(updateMembers) == 0 {
		return err
	}

	// For some reason you need to disable failover and N2N encryption before changing the network configuration.
	resetAutoFailover, err := c.DisableAutoFailover()
	if err != nil {
		return err
	}

	defer resetAutoFailover()

	// If it needs to be, N2N will be re-enabled in the next step after this (reconcileTLSPostTopologyChange()).
	// This is a noop if N2N is not enabled, so we can just call it regardless of the desired N2N state.
	if err := c.disableNodeToNode(); err != nil {
		return err
	}

	// Pass 1: Enable all necessary listeners across every member before changing any network config.
	for _, member := range updateMembers {
		for _, listenerSetting := range enableMemberListeners[member.Name()] {
			if err := couchbaseutil.EnableExternalListener(listenerSetting).RetryFor(time.Minute).On(c.api, member); err != nil {
				return err
			}
		}
	}

	// Pass 2: Update network config on all members that need it. After each config update we wait for
	// the member's management API to recover before moving to the next, ensuring each
	// node's network stack has stabilised.
	for _, member := range updateMembers {
		if err := couchbaseutil.SetNodeNetworkConfiguration(requestedNetworkConfiguration).RetryFor(2*time.Minute).On(c.api, member); err != nil {
			return err
		}

		if err := c.waitForMemberAPI(member, 2*time.Minute); err != nil {
			return err
		}
	}

	// Pass 3: Now that configs are updated and each member's API has recovered, disable any listeners that are no longer required.
	for _, member := range updateMembers {
		if err := couchbaseutil.DisableUnusedExternalListeners().RetryFor(time.Minute).On(c.api, member); err != nil {
			return err
		}
	}

	c.raiseEvent(k8sutil.NetworkSettingsModifiedEvent(c.cluster))
	c.log.Info("Network settings were updated", "cluster", c.cluster.NamespacedName())

	return nil
}

func containsExternalListenerConfiguration(listeners []couchbaseutil.ExternalListener, target *couchbaseutil.ListenerConfiguration) bool {
	for _, listener := range listeners {
		if listener.AddressFamily.ConvertAddressFamilyOutToAddressFamily() == target.AddressFamily {
			return true
		}
	}

	return false
}

func (c *Cluster) waitForMemberAPI(member couchbaseutil.Member, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		var info couchbaseutil.ClusterInfo
		if err := couchbaseutil.GetPoolsDefault(&info).On(c.api, member); err == nil {
			return nil
		}

		if time.Now().After(deadline) {
			return errors.NewStackTracedError(fmt.Errorf("timeout waiting for member %s management API recovery", member.Name()))
		}

		time.Sleep(time.Second)
	}
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
		c.log.Info("Timed out waiting for DNS Propagation", "cluster", c.namespacedName(), "pod", pod.Name, "timeoutAfter", timeoutAfter)
		return true
	}

	return false
}
