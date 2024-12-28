package clusternodes

import requestutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/request"

// NodeDetails retrieves the details for the current node.
/*
 * GET :: /nodes/self.
 * docs.couchbase.com/server/current/rest-api/rest-getting-storage-information.html.
 * Unmarshal into the struct clusternodesapi.Nodes.
 */
func NodeDetails(hostname, port string) *requestutils.Request {
	if port == "" {
		port = "8091"
	}

	return &requestutils.Request{
		Host:   hostname,
		Port:   port,
		Path:   "/nodes/self",
		Method: "GET",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
	}
}

// GetNodeStatuses retrieves the status of nodes.
/*
 * GET :: /nodeStatuses.
 * Not documented in CB docs.
 * Unmarshal into the struct clusternodesapi.NodeStatuses.
 */
func GetNodeStatuses(hostname, port string) *requestutils.Request {
	if port == "" {
		port = "8091"
	}

	return &requestutils.Request{
		Host:   hostname,
		Port:   port,
		Path:   "/nodeStatuses",
		Method: "GET",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
	}
}
