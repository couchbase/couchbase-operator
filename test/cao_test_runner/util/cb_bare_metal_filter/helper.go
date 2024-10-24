package cbbaremetalfilter

import (
	"fmt"
	"time"

	cbrestapi "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cb_rest_api"
	clusternodesapi "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cb_rest_api/cluster_nodes"
	requestutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/request"
)

// GetClusterDetails returns the CB cluster details `/pools/default`.
func GetClusterDetails(namespace, hostname, cbClusterSecret string) (*cbrestapi.PoolsDefault, error) {
	requestClient := requestutils.NewClient()

	cbAuth, err := requestutils.GetCBClusterAuth(cbClusterSecret, namespace)
	if err != nil {
		return nil, fmt.Errorf("get cluster details: %w", err)
	}

	requestClient.SetHTTPAuth(cbAuth.Username, cbAuth.Password)

	hostname, err = requestutils.GetHTTPHostname(hostname, 8091)
	if err != nil {
		return nil, fmt.Errorf("get cluster details: %w", err)
	}

	var poolsDefault cbrestapi.PoolsDefault

	err = requestClient.Do(clusternodesapi.ClusterDetails(hostname), &poolsDefault, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("get cluster details: %w", err)
	}

	return &poolsDefault, nil
}
