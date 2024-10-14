package clusternodes

import (
	"net/url"
	"strings"

	requestutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/request"
)

// AddNode starts a process to add a node to the cluster.
/*
 * POST :: /controller/addNode.
 * docs.couchbase.com/server/current/rest-api/rest-cluster-addnodes.html
 * Unmarshal into map[string]interface{}.
 * NOTE: Services: kv, index, n1ql, eventing, fts, cbas, and backup.
 * NOTE: Rebalance the cluster after adding the CB node. Include the node added in knownNodes.
 */
func AddNode(hostname, cbNodeHostname, username, password string, services []string) *requestutils.Request {
	formData := url.Values{}
	formData.Set("hostname", cbNodeHostname)

	if username != "" {
		formData.Set("user", username)
	}

	if password != "" {
		formData.Set("password", password)
	}

	if services != nil {
		formData.Set("services", url.PathEscape(strings.Join(services, ",")))
	}

	return &requestutils.Request{
		Host:   hostname,
		Path:   "/controller/addNode",
		Method: "POST",
		Headers: map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
		},
		Body: formData.Encode(),
	}
}

// JoinNodeToCluster adds an individual CB server node to a CB cluster.
/*
 * POST :: /node/controller/doJoinCluster.
 * docs.couchbase.com/server/current/rest-api/rest-cluster-joinnode.html.
 */
func JoinNodeToCluster(hostname, clusterMemberHostIP, clusterMemberPort, username, password string, services []string) *requestutils.Request {
	formData := url.Values{}
	formData.Set("clusterMemberHostIp", clusterMemberHostIP)

	if clusterMemberPort != "" {
		formData.Set("clusterMemberPort", clusterMemberPort)
	}

	if username != "" {
		formData.Set("user", username)
	}

	if password != "" {
		formData.Set("password", password)
	}

	if services != nil {
		formData.Set("services", url.PathEscape(strings.Join(services, ",")))
	}

	return &requestutils.Request{
		Host:   hostname,
		Path:   "/node/controller/doJoinCluster",
		Method: "POST",
		Headers: map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
		},
		Body: formData.Encode(),
	}
}

// EjectNode removes a CB node (not active CB node) from the CB cluster.
/*
 * POST :: /controller/ejectNode.
 * docs.couchbase.com/server/current/rest-api/rest-cluster-removenode.html.
 * NOTE: Cannot remove active nodes using this, to remove an active node use RebalanceStart() with ejectedNodes.
 * NOTE: Can be used on failed over nodes, nodes in pending state, or nodes that have been recently added or joined but not yet rebalanced into the cluster.
 */
func EjectNode(hostname, otpNode string) *requestutils.Request {
	formData := url.Values{}

	otpNodes := ConvertToOTPNames([]string{otpNode})

	formData.Set("otpNode", otpNodes[0])

	return &requestutils.Request{
		Host:   hostname,
		Path:   "/controller/ejectNode",
		Method: "POST",
		Headers: map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
		},
		Body: formData.Encode(),
	}
}
