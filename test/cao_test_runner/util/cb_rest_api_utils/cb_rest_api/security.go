package cbrestapi

import (
	"errors"
	"fmt"
	"time"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cb_rest_api_utils/cb_rest_api_spec/security"
	requestutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/request"
	"github.com/sirupsen/logrus"
)

var (
	ErrPermCheckSpecNotFound = errors.New("permission check spec not found")
	ErrGroupNameNotFound     = errors.New("group name not found")
)

type SecurityAPI interface {
	// RBAC Authorization APIs.

	ListRoles(portForward bool) ([]security.Roles, error)
	ListCurrentUsersAndRoles(portForward bool) ([]security.Users, error)
	CheckPermissions(permCheckSpec string, portForward bool) error

	CreateLocalUser(username, password string, roles, groups []string, portForward bool) error
	UpdateLocalUser(username, password string, portForward bool) error
	DeleteLocalUser(username string, portForward bool) error

	CreateExternalUser(username string, roles, groups []string, portForward bool) error
	DeleteExternalUser(username string, portForward bool) error

	ListGroups(portForward bool) ([]security.Groups, error)
	CreateGroup(groupName, desc, ldapGroupRef string, roles []string, portForward bool) error
	DeleteGroup(groupName string, portForward bool) error
}

type Security struct {
	podName    string
	hostname   string
	username   string
	password   string
	isSecure   bool // True for HTTPS request.
	reqTimeout time.Duration
	reqClient  *requestutils.Client
}

// NewSecurityAPI returns the SecurityAPI interface.
/*
 * If secretName is provided (along with namespace) then username and password will be taken from the K8S secret.
 */
func NewSecurityAPI(podName, clusterName, username, password, secretName, namespace string,
	requestTimeout time.Duration, isSecure bool) (SecurityAPI, error) {
	if podName == "" {
		return nil, fmt.Errorf("new cluster nodes api: %w", ErrPodnameNotFound)
	}

	if secretName != "" {
		if namespace == "" {
			return nil, fmt.Errorf("new buckets api: %w", ErrNamespaceNotFound)
		}

		cbAuth, err := requestutils.GetCBClusterAuth(secretName, namespace)
		if err != nil {
			return nil, fmt.Errorf("new buckets api: %w", err)
		}

		username = cbAuth.Username
		password = cbAuth.Password
	}

	if username == "" {
		return nil, fmt.Errorf("new buckets api: %w", ErrUsernameNotFound)
	}

	if password == "" {
		return nil, fmt.Errorf("new buckets api: %w", ErrPasswordNotFound)
	}

	if requestTimeout <= 0 {
		return nil, fmt.Errorf("new buckets api: %w", ErrRequestTimeout)
	}

	hostname, err := requestutils.GetPodHostname(podName, clusterName, namespace)
	if err != nil {
		return nil, fmt.Errorf("new cluster nodes api: %w", err)
	}

	reqClient := requestutils.NewClient()
	reqClient.SetHTTPAuth(username, password)

	return &Security{
		podName:    podName,
		hostname:   hostname,
		username:   username,
		password:   password,
		isSecure:   isSecure,
		reqTimeout: requestTimeout,
		reqClient:  reqClient,
	}, nil
}

// =======================================================================================
// ============================= RBAC Authorization APIs =================================
// =======================================================================================

func (s *Security) ListRoles(portForward bool) ([]security.Roles, error) {
	var roles []security.Roles

	req := security.ListRoles(s.hostname, "")

	var portForwardConfig *requestutils.PortForwardConfig

	if portForward {
		portForwardConfig = &requestutils.PortForwardConfig{
			PodName: s.podName,
			Port:    req.Port,
		}
	} else {
		portForwardConfig = nil
	}

	err := s.reqClient.Do(req, &roles, s.reqTimeout, portForwardConfig)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}

	return roles, nil
}

func (s *Security) ListCurrentUsersAndRoles(portForward bool) ([]security.Users, error) {
	var users []security.Users

	req := security.ListCurrentUsersAndRoles(s.hostname, "")

	var portForwardConfig *requestutils.PortForwardConfig

	if portForward {
		portForwardConfig = &requestutils.PortForwardConfig{
			PodName: s.podName,
			Port:    req.Port,
		}
	} else {
		portForwardConfig = nil
	}

	err := s.reqClient.Do(req, &users, s.reqTimeout, portForwardConfig)
	if err != nil {
		return nil, fmt.Errorf("list current users and roles: %w", err)
	}

	return users, nil
}

func (s *Security) CheckPermissions(permCheckSpec string, portForward bool) error {
	if permCheckSpec == "" {
		return fmt.Errorf("check permissions: %w", ErrPermCheckSpecNotFound)
	}

	req := security.CheckPermissions(s.hostname, "", permCheckSpec)

	var portForwardConfig *requestutils.PortForwardConfig

	if portForward {
		portForwardConfig = &requestutils.PortForwardConfig{
			PodName: s.podName,
			Port:    req.Port,
		}
	} else {
		portForwardConfig = nil
	}

	err := s.reqClient.Do(req, nil, s.reqTimeout, portForwardConfig)
	if err != nil {
		return fmt.Errorf("check permissions %s: %w", permCheckSpec, err)
	}

	return nil
}

func (s *Security) CreateLocalUser(username, password string, roles, groups []string, portForward bool) error {
	if username == "" {
		return fmt.Errorf("create local user: %w", ErrUsernameNotFound)
	}

	if password == "" {
		return fmt.Errorf("create local user: %w", ErrPasswordNotFound)
	}

	if roles == nil && groups == nil {
		logrus.Warn("create local user: no roles or groups provided")
	}

	req := security.CreateLocalUser(s.hostname, "", username, password, roles, groups)

	var portForwardConfig *requestutils.PortForwardConfig

	if portForward {
		portForwardConfig = &requestutils.PortForwardConfig{
			PodName: s.podName,
			Port:    req.Port,
		}
	} else {
		portForwardConfig = nil
	}

	err := s.reqClient.Do(req, nil, s.reqTimeout, portForwardConfig)
	if err != nil {
		return fmt.Errorf("create local user %s with roles %v: %w", username, roles, err)
	}

	return nil
}

func (s *Security) UpdateLocalUser(username, password string, portForward bool) error {
	if username == "" {
		return fmt.Errorf("create local user: %w", ErrUsernameNotFound)
	}

	if password == "" {
		return fmt.Errorf("create local user: %w", ErrPasswordNotFound)
	}

	req := security.UpdateLocalUser(s.hostname, "", username, password)

	var portForwardConfig *requestutils.PortForwardConfig

	if portForward {
		portForwardConfig = &requestutils.PortForwardConfig{
			PodName: s.podName,
			Port:    req.Port,
		}
	} else {
		portForwardConfig = nil
	}

	err := s.reqClient.Do(req, nil, s.reqTimeout, portForwardConfig)
	if err != nil {
		return fmt.Errorf("update user %s: %w", username, err)
	}

	return nil
}

func (s *Security) DeleteLocalUser(username string, portForward bool) error {
	if username == "" {
		return fmt.Errorf("create local user: %w", ErrUsernameNotFound)
	}

	req := security.DeleteLocalUser(s.hostname, "", username)

	var portForwardConfig *requestutils.PortForwardConfig

	if portForward {
		portForwardConfig = &requestutils.PortForwardConfig{
			PodName: s.podName,
			Port:    req.Port,
		}
	} else {
		portForwardConfig = nil
	}

	err := s.reqClient.Do(req, nil, s.reqTimeout, portForwardConfig)
	if err != nil {
		return fmt.Errorf("delete user %s: %w", username, err)
	}

	return nil
}

func (s *Security) CreateExternalUser(username string, roles, groups []string, portForward bool) error {
	if username == "" {
		return fmt.Errorf("create external user: %w", ErrUsernameNotFound)
	}

	if roles == nil && groups == nil {
		logrus.Warn("create external user: no roles or groups provided")
	}

	req := security.CreateExternalUser(s.hostname, "", username, roles, groups)

	var portForwardConfig *requestutils.PortForwardConfig

	if portForward {
		portForwardConfig = &requestutils.PortForwardConfig{
			PodName: s.podName,
			Port:    req.Port,
		}
	} else {
		portForwardConfig = nil
	}

	err := s.reqClient.Do(req, nil, s.reqTimeout, portForwardConfig)
	if err != nil {
		return fmt.Errorf("create external user %s with roles %v: %w", username, roles, err)
	}

	return nil
}

func (s *Security) DeleteExternalUser(username string, portForward bool) error {
	if username == "" {
		return fmt.Errorf("delete external user: %w", ErrUsernameNotFound)
	}

	req := security.DeleteExternalUser(s.hostname, "", username)

	var portForwardConfig *requestutils.PortForwardConfig

	if portForward {
		portForwardConfig = &requestutils.PortForwardConfig{
			PodName: s.podName,
			Port:    req.Port,
		}
	} else {
		portForwardConfig = nil
	}

	err := s.reqClient.Do(req, nil, s.reqTimeout, portForwardConfig)
	if err != nil {
		return fmt.Errorf("delete external user %s: %w", username, err)
	}

	return nil
}

func (s *Security) ListGroups(portForward bool) ([]security.Groups, error) {
	var groups []security.Groups

	req := security.ListGroups(s.hostname, "")

	var portForwardConfig *requestutils.PortForwardConfig

	if portForward {
		portForwardConfig = &requestutils.PortForwardConfig{
			PodName: s.podName,
			Port:    req.Port,
		}
	} else {
		portForwardConfig = nil
	}

	err := s.reqClient.Do(req, &groups, s.reqTimeout, portForwardConfig)
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}

	return groups, nil
}

func (s *Security) CreateGroup(groupName, desc, ldapGroupRef string, roles []string, portForward bool) error {
	if groupName == "" {
		return fmt.Errorf("create new group: %w", ErrGroupNameNotFound)
	}

	req := security.CreateGroup(s.hostname, "", groupName, desc, ldapGroupRef, roles)

	var portForwardConfig *requestutils.PortForwardConfig

	if portForward {
		portForwardConfig = &requestutils.PortForwardConfig{
			PodName: s.podName,
			Port:    req.Port,
		}
	} else {
		portForwardConfig = nil
	}

	err := s.reqClient.Do(req, nil, s.reqTimeout, portForwardConfig)
	if err != nil {
		return fmt.Errorf("create new group %s with roles %v: %w", groupName, roles, err)
	}

	return nil
}

func (s *Security) DeleteGroup(groupName string, portForward bool) error {
	if groupName == "" {
		return fmt.Errorf("delete group: %w", ErrGroupNameNotFound)
	}

	req := security.DeleteGroup(s.hostname, "", groupName)

	var portForwardConfig *requestutils.PortForwardConfig

	if portForward {
		portForwardConfig = &requestutils.PortForwardConfig{
			PodName: s.podName,
			Port:    req.Port,
		}
	} else {
		portForwardConfig = nil
	}

	err := s.reqClient.Do(req, nil, s.reqTimeout, portForwardConfig)
	if err != nil {
		return fmt.Errorf("delete group %s: %w", groupName, err)
	}

	return nil
}
