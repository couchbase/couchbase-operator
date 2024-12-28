package clusternodes

import (
	requestutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/request"
)

// ClusterDetails retrieves the cluster details.
/*
 * GET :: /pools/default.
 * docs.couchbase.com/server/current/rest-api/rest-cluster-details.html.
 * Unmarshal into the struct clusternodesapi.PoolsDefault.
 */
func ClusterDetails(hostname, port string) *requestutils.Request {
	if port == "" {
		port = "8091"
	}

	return &requestutils.Request{
		Host:   hostname,
		Port:   port,
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
 * Unmarshal into the struct clusternodesapi.TaskList.
 */
func ClusterTasks(hostname, port string) *requestutils.Request {
	if port == "" {
		port = "8091"
	}

	return &requestutils.Request{
		Host:   hostname,
		Port:   port,
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
func ClusterInfo(hostname, port string) *requestutils.Request {
	if port == "" {
		port = "8091"
	}

	return &requestutils.Request{
		Host:   hostname,
		Port:   port,
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
 * Unmarshal into the struct clusternodesapi.TerseClusterInfo.
 */
func GetTerseClusterInfo(hostname, port string) *requestutils.Request {
	if port == "" {
		port = "8091"
	}

	return &requestutils.Request{
		Host:   hostname,
		Port:   port,
		Path:   "/pools/default/terseClusterInfo",
		Method: "GET",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
	}
}
