# Using Couchbase Clients

The Kubernetes networking model poses some challenges when interacting with a Couchbase cluster.  This documents possible ways that your clients can connect with the cluster.

## From a Pod in the Same Kubernetes Cluster

This is the simplest network model to operate with and the recommended method.

When a cluster is created the operator will also create a headless service which creates A records for each Couchbase node and an SRV record for the cluster as a whole.  To create a connection to the cluster from within the overlay (as a pod in the same Kubernetes cluster) we recommend using the SRV record to perform service discovery:

```python
from couchbase.cluster import Cluster

c = Cluster('couchbase://my-cluster.default.svc.cluster.local')
```

Where the domain is composed of ```my-cluster```, which is the name of the CouchbaseCluster resource and the super domain.

## From an External address

Accessing the cluster from outside of the local Kubernetes is up to the end user to configure dependant on their network architecture or cloud provider.  This is of interest for users wanting to access Couchbase Server across the public internet, either from another cloud or on-premise hardware.

It is unlikely that DNS based service discovery will be available due to the Kubernetes implementation, so you will need to gather a list of pod IP addresses for you clients to use.  If you are able to direct DNS queries at the remote Kubernetes DNS service you can get a list of A records by looking up ```my-cluster.default.svc.cluster.local```.

Although not supported officially, this section aims to detail how to access your cluster, giving example scenarios.

### Routed Network

A routed network involves inter-pod communication to occur via a router as north-south traffic, for example 10.0.0.0/24 and 10.0.1.0/24 are allocated as pod address prefixes on two Kubernetes nodes.  Traffic between them is sent to the default router which then has a routing table entry for each prefix and forwards matching packets to the correct Kubernetes node.

As a result any traffic passing through the router can be forwarded on directly to the target pod via its IP address.  Clients could reside on the same subnet as the cluster, a separate subnet connected to the router, or peered with another network entirely via a VPN tunnel for example.

### Overlay Network

An overlay network captures packets destined for another Kubernetes node, encapsulates it (e.g. GRE, VXLAN) then forwards it directly to the destination node via east-west traffic, where the packet is decapsulated.  This relies on service discovery in order for nodes to know about their peer addresses and their pod network prefixes.

The cluster cannot be accessed via node ports as clients do not support the concept of each node having different ports per service.  As a result any traffic from clients must be tunnelled into the Kubernetes cluster, either to the node or to a pod running in the overlay, then NATed to avoid asymmetric routing.

A possible solution could involve a publicly addressable VPN gateway server which decapsulates remote traffic, forwards to a Kubernetes node via an IP-in-IP tunnel, which then performs source NAT after routing into the overlay.
