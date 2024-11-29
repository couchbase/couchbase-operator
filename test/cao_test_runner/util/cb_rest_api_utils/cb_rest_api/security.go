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

	ListRoles() ([]security.Roles, error)
	ListCurrentUsersAndRoles() ([]security.Users, error)
	CheckPermissions(permCheckSpec string) error

	CreateLocalUser(username, password string, roles, groups []string) error
	UpdateLocalUser(username, password string) error
	DeleteLocalUser(username string) error

	CreateExternalUser(username string, roles, groups []string) error
	DeleteExternalUser(username string) error

	ListGroups() ([]security.Groups, error)
	CreateGroup(groupName, desc, ldapGroupRef string, roles []string) error
	DeleteGroup(groupName string) error
}

type Security struct {
	hostname   string
	port       int
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
func NewSecurityAPI(hostname string, port int, username, password, secretName, namespace string,
	requestTimeout time.Duration, isSecure bool) (SecurityAPI, error) {
	if hostname == "" {
		return nil, fmt.Errorf("new buckets api: %w", ErrHostnameNotFound)
	}

	if port <= 0 {
		return nil, fmt.Errorf("new buckets api: %w", ErrPortNotFound)
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

	// Checking the hostname
	// TODO split the hostname and port.
	// TODO update GetHTTPHostname to have isSecure parameter to support https.
	updatedHostname, err := requestutils.GetHTTPHostname(hostname, int64(port))
	if err != nil {
		return nil, fmt.Errorf("new buckets api: %w", err)
	}

	reqClient := requestutils.NewClient()
	reqClient.SetHTTPAuth(username, password)

	return &Security{
		hostname:   updatedHostname,
		port:       port,
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

func (s *Security) ListRoles() ([]security.Roles, error) {
	var roles []security.Roles

	err := s.reqClient.Do(security.ListRoles(s.hostname), &roles, s.reqTimeout)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}

	return roles, nil
}

func (s *Security) ListCurrentUsersAndRoles() ([]security.Users, error) {
	var users []security.Users

	err := s.reqClient.Do(security.ListCurrentUsersAndRoles(s.hostname), &users, s.reqTimeout)
	if err != nil {
		return nil, fmt.Errorf("list current users and roles: %w", err)
	}

	return users, nil
}

func (s *Security) CheckPermissions(permCheckSpec string) error {
	if permCheckSpec == "" {
		return fmt.Errorf("check permissions: %w", ErrPermCheckSpecNotFound)
	}

	err := s.reqClient.Do(security.CheckPermissions(s.hostname, permCheckSpec), nil, s.reqTimeout)
	if err != nil {
		return fmt.Errorf("check permissions %s: %w", permCheckSpec, err)
	}

	return nil
}

func (s *Security) CreateLocalUser(username, password string, roles, groups []string) error {
	if username == "" {
		return fmt.Errorf("create local user: %w", ErrUsernameNotFound)
	}

	if password == "" {
		return fmt.Errorf("create local user: %w", ErrPasswordNotFound)
	}

	if roles == nil && groups == nil {
		logrus.Warn("create local user: no roles or groups provided")
	}

	err := s.reqClient.Do(security.CreateLocalUser(s.hostname, username, password, roles, groups), nil, s.reqTimeout)
	if err != nil {
		return fmt.Errorf("create local user %s with roles %v: %w", username, roles, err)
	}

	return nil
}

func (s *Security) UpdateLocalUser(username, password string) error {
	if username == "" {
		return fmt.Errorf("create local user: %w", ErrUsernameNotFound)
	}

	if password == "" {
		return fmt.Errorf("create local user: %w", ErrPasswordNotFound)
	}

	err := s.reqClient.Do(security.UpdateLocalUser(s.hostname, username, password), nil, s.reqTimeout)
	if err != nil {
		return fmt.Errorf("update user %s: %w", username, err)
	}

	return nil
}

func (s *Security) DeleteLocalUser(username string) error {
	if username == "" {
		return fmt.Errorf("create local user: %w", ErrUsernameNotFound)
	}

	err := s.reqClient.Do(security.DeleteLocalUser(s.hostname, username), nil, s.reqTimeout)
	if err != nil {
		return fmt.Errorf("delete user %s: %w", username, err)
	}

	return nil
}

func (s *Security) CreateExternalUser(username string, roles, groups []string) error {
	if username == "" {
		return fmt.Errorf("create external user: %w", ErrUsernameNotFound)
	}

	if roles == nil && groups == nil {
		logrus.Warn("create external user: no roles or groups provided")
	}

	err := s.reqClient.Do(security.CreateExternalUser(s.hostname, username, roles, groups), nil, s.reqTimeout)
	if err != nil {
		return fmt.Errorf("create external user %s with roles %v: %w", username, roles, err)
	}

	return nil
}

func (s *Security) DeleteExternalUser(username string) error {
	if username == "" {
		return fmt.Errorf("delete external user: %w", ErrUsernameNotFound)
	}

	err := s.reqClient.Do(security.DeleteExternalUser(s.hostname, username), nil, s.reqTimeout)
	if err != nil {
		return fmt.Errorf("delete external user %s: %w", username, err)
	}

	return nil
}

func (s *Security) ListGroups() ([]security.Groups, error) {
	var groups []security.Groups

	err := s.reqClient.Do(security.ListGroups(s.hostname), &groups, s.reqTimeout)
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}

	return groups, nil
}

func (s *Security) CreateGroup(groupName, desc, ldapGroupRef string, roles []string) error {
	if groupName == "" {
		return fmt.Errorf("create new group: %w", ErrGroupNameNotFound)
	}

	err := s.reqClient.Do(security.CreateGroup(s.hostname, groupName, desc, ldapGroupRef, roles), nil, s.reqTimeout)
	if err != nil {
		return fmt.Errorf("create new group %s with roles %v: %w", groupName, roles, err)
	}

	return nil
}

func (s *Security) DeleteGroup(groupName string) error {
	if groupName == "" {
		return fmt.Errorf("delete group: %w", ErrGroupNameNotFound)
	}

	err := s.reqClient.Do(security.DeleteGroup(s.hostname, groupName), nil, s.reqTimeout)
	if err != nil {
		return fmt.Errorf("delete group %s: %w", groupName, err)
	}

	return nil
}
