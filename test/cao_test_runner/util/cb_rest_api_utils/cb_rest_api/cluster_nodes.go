package cbrestapi

import (
	"errors"
	"fmt"
	"time"

	clusternodesapi "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cb_rest_api_utils/cb_rest_api_spec/cluster_nodes"
	requestutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/request"
)

var (
	ErrHostnameNotFound  = errors.New("hostname not provided")
	ErrPortNotFound      = errors.New("port not provided")
	ErrUsernameNotFound  = errors.New("username not provided")
	ErrNamespaceNotFound = errors.New("namespace not provided")
	ErrPasswordNotFound  = errors.New("password not provided")
	ErrRequestTimeout    = errors.New("request timeout not provided")
)

type ClusterNodesAPI interface {
	// CB Cluster Information APIs.
	PoolsDefault() (*clusternodesapi.PoolsDefault, error)

	// Rebalance APIs.
	StopRebalance() error
}

type ClusterNodes struct {
	hostname   string
	port       int // TODO not being used as of now. hostname has the port included.
	username   string
	password   string
	isSecure   bool // True for HTTPS request.
	reqTimeout time.Duration
	reqClient  *requestutils.Client
}

// NewClusterNodesAPI returns the ClusterNodesAPI interface.
/*
 * If secretName is provided (along with namespace) then username and password will be taken from the K8S secret.
 */
func NewClusterNodesAPI(hostname string, port int, username, password, secretName, namespace string,
	requestTimeout time.Duration, isSecure bool) (ClusterNodesAPI, error) {
	if hostname == "" {
		return nil, fmt.Errorf("new cluster nodes api: %w", ErrHostnameNotFound)
	}

	if port <= 0 {
		return nil, fmt.Errorf("new cluster nodes api: %w", ErrPortNotFound)
	}

	if secretName != "" {
		if namespace == "" {
			return nil, fmt.Errorf("new cluster nodes api: %w", ErrNamespaceNotFound)
		}

		cbAuth, err := requestutils.GetCBClusterAuth(secretName, namespace)
		if err != nil {
			return nil, fmt.Errorf("new cluster nodes api: %w", err)
		}

		username = cbAuth.Username
		password = cbAuth.Password
	}

	if username == "" {
		return nil, fmt.Errorf("new cluster nodes api: %w", ErrUsernameNotFound)
	}

	if password == "" {
		return nil, fmt.Errorf("new cluster nodes api: %w", ErrPasswordNotFound)
	}

	if requestTimeout <= 0 {
		return nil, fmt.Errorf("new cluster nodes api: %w", ErrRequestTimeout)
	}

	// Checking the hostname
	// TODO split the hostname and port.
	// TODO update GetHTTPHostname to have isSecure parameter to support https.
	updatedHostname, err := requestutils.GetHTTPHostname(hostname, int64(port))
	if err != nil {
		return nil, fmt.Errorf("new cluster nodes api: %w", err)
	}

	reqClient := requestutils.NewClient()
	reqClient.SetHTTPAuth(username, password)

	return &ClusterNodes{
		hostname:   updatedHostname,
		port:       port,
		username:   username,
		password:   password,
		isSecure:   isSecure,
		reqTimeout: requestTimeout,
		reqClient:  reqClient,
	}, nil
}

// ===============================================================
// ================= CB Cluster Information APIs =================
// ===============================================================

func (cn *ClusterNodes) PoolsDefault() (*clusternodesapi.PoolsDefault, error) {
	var poolsDefault clusternodesapi.PoolsDefault

	err := cn.reqClient.Do(clusternodesapi.ClusterDetails(cn.hostname), &poolsDefault, cn.reqTimeout)
	if err != nil {
		return nil, fmt.Errorf("pools default: %w", err)
	}

	return &poolsDefault, nil
}

// ===============================================================
// ======================= Rebalance APIs ========================
// ===============================================================

func (cn *ClusterNodes) StopRebalance() error {
	err := cn.reqClient.Do(clusternodesapi.RebalanceStop(cn.hostname), nil, cn.reqTimeout)
	if err != nil {
		return fmt.Errorf("stop rebalance: %w", err)
	}

	return nil
}
