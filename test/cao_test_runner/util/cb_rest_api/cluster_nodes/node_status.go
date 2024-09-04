package clusternodes

import requestutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/request"

// NodeDetails retrieves the details for the current node.
/*
 * GET :: /nodes/self.
 * docs.couchbase.com/server/current/rest-api/rest-getting-storage-information.html.
 * Unmarshal into the struct cbrestapi.Nodes.
 */
func NodeDetails(hostname string) *requestutils.Request {
	return &requestutils.Request{
		Host:   hostname,
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
 * Unmarshal into the struct cbrestapi.NodeStatuses.
 */
func GetNodeStatuses(hostname string) *requestutils.Request {
	return &requestutils.Request{
		Host:   hostname,
		Path:   "/nodeStatuses",
		Method: "GET",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
	}
}
