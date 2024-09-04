package clusternodes

import (
	requestutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/request"
)

// ClusterDetails retrieves the cluster details.
/*
 * GET :: /pools/default.
 * docs.couchbase.com/server/current/rest-api/rest-cluster-details.html.
 * Unmarshal into the struct cbrestapi.PoolsDefault.
 */
func ClusterDetails(hostname string) *requestutils.Request {
	return &requestutils.Request{
		Host:   hostname,
		Path:   "/pools/default",
		Method: "GET",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
	}
}

// ClusterTasks retrieves the cluster tasks.
/*
 * GET :: /pools/default/tasks.
 * docs.couchbase.com/server/current/rest-api/rest-get-cluster-tasks.html.
 * Unmarshal into the struct cbrestapi.TaskList.
 */
func ClusterTasks(hostname string) *requestutils.Request {
	return &requestutils.Request{
		Host:   hostname,
		Path:   "/pools/default/tasks",
		Method: "GET",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
	}
}

// ClusterInfo retrieves the cluster information.
/*
 * GET :: /pools.
 * docs.couchbase.com/server/current/rest-api/rest-cluster-get.html.
 * Unmarshal into the map[string]interface{}.
 */
func ClusterInfo(hostname string) *requestutils.Request {
	return &requestutils.Request{
		Host:   hostname,
		Path:   "/pools",
		Method: "GET",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
	}
}

// GetTerseClusterInfo retrieves the terse cluster information.
/*
 * GET :: /pools/default/terseClusterInfo.
 * docs.couchbase.com/server/current/rest-api/rest-cluster-get.html.
 * Unmarshal into the struct cbrestapi.TerseClusterInfo.
 */
func GetTerseClusterInfo(hostname string) *requestutils.Request {
	return &requestutils.Request{
		Host:   hostname,
		Path:   "/pools/default/terseClusterInfo",
		Method: "GET",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
	}
}
