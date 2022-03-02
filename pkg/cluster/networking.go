package cluster

import (
	"context"
	"fmt"
	"net"
	"reflect"
	"strings"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
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

	ports, err := k8sutil.GetAlternateAddressExternalPorts(c.k8s, c.cluster.Namespace, member.Name())
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

// initMemberAlternateAddresses injects the K8S node's L3 address and alternate
// ports into the requested member.  Clients may use these addresses/ports to
// connect to the cluster if there is no direct L3 connectivity into the pod
// network.
func (c *Cluster) reconcileMemberAlternateAddresses() error {
	// Start a global timout counter, this caters for any/all alternate addresses
	// in the system as we are waiting for external-DNS to do its thing, and all
	// services will be processed in bulk.
	delay := time.Duration(0)
	if c.cluster.Spec.Networking.WaitForAddressReachableDelay != nil {
		delay = c.cluster.Spec.Networking.WaitForAddressReachableDelay.Duration
	}

	ctx, cancel := context.WithTimeout(context.Background(), delay)
	defer cancel()

	// Examine each member in turn as they will have different node
	// addresses (i.e. you must be using anti affinity or kubernetes
	// has no way of addressing individual cluster nodes).
	for _, member := range c.members {
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
			if existingAddresses != nil {
				if err := couchbaseutil.DeleteAlternateAddressesExternal().On(c.api, member); err != nil {
					return err
				}
			}

			continue
		}

		// Get the requested alternate address specification.
		addresses, err := c.createAlternateAddressesExternal(member)
		if err != nil {
			return err
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

		// Check to see if we need to perform any updates, ignoring if not
		if reflect.DeepEqual(addresses, existingAddresses) {
			continue
		}

		// Wait for a period of time before allowing polling to happen in order to
		// avoid negative caching of DNS values.
		log.Info("Waiting for DNS propagation", "cluster", c.namespacedName())

		<-ctx.Done()

		// Next check to see if the DNS entry is actually live (and visible by the Operator),
		// before installing it into Couchbase server, which will then propagate to clients
		// and potentially break them.
		log.Info("Polling for DNS availability", "cluster", c.namespacedName(), "service", member.Name())

		timeout := 10 * time.Minute

		if c.cluster.Spec.Networking.WaitForAddressReachable != nil {
			timeout = c.cluster.Spec.Networking.WaitForAddressReachable.Duration
		}

		if err := waitAlternateAddressReachable(timeout, addresses); err != nil {
			return err
		}

		log.Info("DNS available", "cluster", c.namespacedName(), "service", member.Name())

		// Perform the update
		if err := couchbaseutil.SetAlternateAddressesExternal(addresses).On(c.api, member); err != nil {
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
	tag, err := k8sutil.CouchbaseVersion(c.cluster.Spec.Image)
	if err != nil {
		return false
	}

	version, err := couchbaseutil.NewVersion(tag)
	if err != nil {
		return false
	}

	return version.GreaterEqualString("7.0.2")
}

// generateNetworkConfiguration generates the required network configuration for
// the requested cluster configuration.
func (c *Cluster) generateNetworkConfiguration() *couchbaseutil.NodeNetworkConfiguration {
	networkConfiguration := &couchbaseutil.NodeNetworkConfiguration{}

	t := true
	f := false

	// If nothing is specified, or we explcitly ask for IPv4 set the current protocol.
	// If IPv4 is explcitly asked for, and it's supported, turn off dual stack support.
	if c.cluster.Spec.Networking.AddressFamily == nil || *c.cluster.Spec.Networking.AddressFamily == couchbasev2.AFInet {
		networkConfiguration.AddressFamily = couchbaseutil.AddressFamilyIPV4

		if c.supportsAFFiltering() {
			networkConfiguration.AddressFamilyOnly = &f

			if c.cluster.Spec.Networking.AddressFamily != nil {
				networkConfiguration.AddressFamilyOnly = &t
			}
		}
	}

	// If IPv6 was asked for, set that as the communication protocol and disable IPv4.
	if c.cluster.Spec.Networking.AddressFamily != nil && *c.cluster.Spec.Networking.AddressFamily == couchbasev2.AFInet6 {
		networkConfiguration.AddressFamily = couchbaseutil.AddressFamilyIPV6

		if c.supportsAFFiltering() {
			networkConfiguration.AddressFamilyOnly = &t
		}
	}

	return networkConfiguration
}

// initMemberNetworking sets up IP address families on pod start up.  Sadly you cannot also
// setup node-to-node networking because Server will collapse in a heap.  Apply all configuration
// blindly because we cannot see what Couchbase's configuration is at this time...
func (c *Cluster) initMemberNetworking(member couchbaseutil.Member) error {
	if err := couchbaseutil.SetNodeNetworkConfiguration(c.generateNetworkConfiguration()).InPlaintext().RetryFor(10*time.Second).On(c.api, member); err != nil {
		return err
	}

	return nil
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
