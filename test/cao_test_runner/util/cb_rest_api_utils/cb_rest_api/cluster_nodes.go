package cbrestapi

import (
	"errors"
	"fmt"
	"time"

	clusternodesapi "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cb_rest_api_utils/cb_rest_api_spec/cluster_nodes"
	requestutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/request"
)

var (
	ErrPodnameNotFound   = errors.New("pod name not provided")
	ErrUsernameNotFound  = errors.New("username not provided")
	ErrNamespaceNotFound = errors.New("namespace not provided")
	ErrPasswordNotFound  = errors.New("password not provided")
	ErrRequestTimeout    = errors.New("request timeout not provided")
)

type ClusterNodesAPI interface {
	// CB Cluster Information APIs.
	PoolsDefault(portForward bool) (*clusternodesapi.PoolsDefault, error)
	PoolsDefaultTasks(portForward bool) ([]clusternodesapi.Task, error)

	// Rebalance APIs.
	StopRebalance(portForward bool) error
}

type ClusterNodes struct {
	podName    string
	hostname   string
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
func NewClusterNodesAPI(podName, clusterName, username, password, secretName, namespace string,
	requestTimeout time.Duration, isSecure bool, isBareMetal bool) (ClusterNodesAPI, error) {
	if podName == "" {
		return nil, fmt.Errorf("new cluster nodes api: %w", ErrPodnameNotFound)
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

	var hostname string
	var err error

	if !isBareMetal {
		hostname, err = requestutils.GetPodHostname(podName, clusterName, namespace)
		if err != nil {
			return nil, fmt.Errorf("new cluster nodes api: %w", err)
		}
	} else {
		hostname = podName
	}

	reqClient := requestutils.NewClient()
	reqClient.SetHTTPAuth(username, password)

	return &ClusterNodes{
		podName:    podName,
		hostname:   hostname,
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

func (cn *ClusterNodes) PoolsDefault(portForward bool) (*clusternodesapi.PoolsDefault, error) {
	var poolsDefault clusternodesapi.PoolsDefault

	var portForwardConfig *requestutils.PortForwardConfig

	req := clusternodesapi.ClusterDetails(cn.hostname, "")

	if portForward {
		portForwardConfig = &requestutils.PortForwardConfig{
			PodName: cn.podName,
			Port:    req.Port,
		}
	} else {
		portForwardConfig = nil
	}

	err := cn.reqClient.Do(req, &poolsDefault, cn.reqTimeout, portForwardConfig)
	if err != nil {
		return nil, fmt.Errorf("pools default: %w", err)
	}

	return &poolsDefault, nil
}

func (cn *ClusterNodes) PoolsDefaultTasks(portForward bool) ([]clusternodesapi.Task, error) {
	var poolsDefaultTasks []clusternodesapi.Task

	var portForwardConfig *requestutils.PortForwardConfig

	req := clusternodesapi.ClusterTasks(cn.hostname, "")

	if portForward {
		portForwardConfig = &requestutils.PortForwardConfig{
			PodName: cn.podName,
			Port:    req.Port,
		}
	} else {
		portForwardConfig = nil
	}

	err := cn.reqClient.Do(req, &poolsDefaultTasks, cn.reqTimeout, portForwardConfig)
	if err != nil {
		return nil, fmt.Errorf("pools default tasks: %w", err)
	}

	return poolsDefaultTasks, nil
}

// ===============================================================
// ======================= Rebalance APIs ========================
// ===============================================================

func (cn *ClusterNodes) StopRebalance(portForward bool) error {
	req := clusternodesapi.RebalanceStop(cn.hostname, "")

	var portForwardConfig *requestutils.PortForwardConfig

	if portForward {
		portForwardConfig = &requestutils.PortForwardConfig{
			PodName: cn.podName,
			Port:    req.Port,
		}
	} else {
		portForwardConfig = nil
	}

	err := cn.reqClient.Do(clusternodesapi.RebalanceStop(cn.hostname, "8091"), nil, cn.reqTimeout, portForwardConfig)
	if err != nil {
		return fmt.Errorf("stop rebalance: %w", err)
	}

	return nil
}
