package clusternodes

import (
	requestutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/request"
)

// RebalanceProgress retrieves the rebalance progress.
/*
 * GET :: /pools/default/rebalanceProgress.
 * docs.couchbase.com/server/current/rest-api/rest-get-rebalance-progress.html.
 * Unmarshal into the map[string]interface{}.
 */
func RebalanceProgress(hostname string) *requestutils.Request {
	return &requestutils.Request{
		Host:   hostname,
		Path:   "/pools/default/rebalanceProgress",
		Method: "GET",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
	}
}

// RebalanceReport retrieves the rebalance report using the given report ID.
/*
 * GET :: /logs/rebalanceReport?reportID=<report-id>.
 * docs.couchbase.com/server/current/rest-api/rest-get-cluster-tasks.html.
 * Unmarshal into the map[string]interface{}.
 */
func RebalanceReport(hostname, reportID string) *requestutils.Request {
	return &requestutils.Request{
		Host:   hostname,
		Path:   "/logs/rebalanceReport?reportID=" + reportID,
		Method: "GET",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
	}
}
