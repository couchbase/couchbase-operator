package cluster

import (
	"fmt"
	"reflect"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	cberrors "github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/gocbmgr"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

type RBACFeature string

const (
	RBACFeatureLDAPAuth  RBACFeature = "ldap"
	RBACFeatureGroupAuth RBACFeature = "groups"
)

// reconcileRBACResources compares requested and actual rbac resources
// creates, updates, and deletes where necessary
func (c *Cluster) reconcileRBACResources() error {

	groups, err := c.reconcileGroups()
	if err != nil {
		return err
	}

	users, err := c.reconcileUsers(groups)
	if err != nil {
		return err
	}

	// Update status to reflect requested resources
	c.cluster.Status.Groups = groups
	c.cluster.Status.Users = users

	return nil
}

// reconcileGroups creates, edits, removes server groups according to requested configuration
func (c *Cluster) reconcileGroups() ([]string, error) {

	// gather selected groups
	selector := labels.Everything()
	if c.cluster.Spec.Security.RBAC.Selector != nil {
		var err error
		if selector, err = metav1.LabelSelectorAsSelector(c.cluster.Spec.Security.RBAC.Selector); err != nil {
			return nil, err
		}
	}
	requestedGroups := make(map[string]cbmgr.Group)
	for _, cbGroup := range c.k8s.CouchbaseGroups.List() {
		if selector.Matches(labels.Set(cbGroup.Labels)) {
			group := cbmgr.Group{
				ID:           cbGroup.Name,
				LDAPGroupRef: cbGroup.Spec.LDAPGroupRef,
			}
			// copy roles to group
			for _, role := range cbGroup.Spec.Roles {
				group.Roles = append(group.Roles, cbmgr.UserRole{
					Role:       role.Name,
					BucketName: role.Bucket,
				})
			}
			requestedGroups[group.ID] = group
		}
	}

	// reconcile with existing groups
	existingGroups, err := c.client.ListGroups(c.readyMembers())
	existingGroupNames := []string{}
	if err != nil {
		return existingGroupNames, err
	}
	for _, e := range existingGroups {
		if r, ok := requestedGroups[e.ID]; ok {
			// update changed group
			rolesMatch := reflect.DeepEqual(cbmgr.RolesToStr(r.Roles), cbmgr.RolesToStr(e.Roles))
			if !rolesMatch || r.LDAPGroupRef != e.LDAPGroupRef {
				if err := c.client.CreateGroup(c.readyMembers(), r); err != nil {
					return existingGroupNames, err
				}
				c.raiseEvent(k8sutil.GroupEditEvent(e.ID, c.cluster))
				log.Info("edit CouchbaseGroup", "name", e.ID)
			}
		} else {
			// delete unrequested group
			if err := c.client.DeleteGroup(c.readyMembers(), *e); err != nil {
				return existingGroupNames, err
			}
			c.raiseEvent(k8sutil.GroupDeleteEvent(e.ID, c.cluster))
			log.Info("delete CouchbaseGroup", "name", e.ID)
		}
		existingGroupNames = append(existingGroupNames, e.ID)
	}

	// create requested groups that do not exist
	for _, group := range requestedGroups {
		if _, found := couchbasev2.HasItem(group.ID, existingGroupNames); !found {
			if err := c.client.CreateGroup(c.readyMembers(), group); err != nil {
				return existingGroupNames, err
			}
			c.raiseEvent(k8sutil.GroupCreateEvent(group.ID, c.cluster))
			log.Info("create CouchbaseGroup", "name", group.ID)
		}
	}

	// create
	return existingGroupNames, nil
}

// reconcileUsers creates, edits, removes server users according to requested configuration
func (c *Cluster) reconcileUsers(groups []string) ([]string, error) {

	// Map of requested users by name
	requestedUsers := make(map[string]cbmgr.User)

	// get rolebindings
	couchbaseRoleBindings := c.k8s.CouchbaseRoleBindings.List()
	for _, roleBinding := range couchbaseRoleBindings {

		roleRef := roleBinding.Spec.RoleRef
		groupName := roleRef.Name

		// warn if referred group is missing because
		// bound users may not be created or deleted
		if _, found := couchbasev2.HasItem(groupName, groups); !found {
			msg := fmt.Sprintf("Rolebinding `%s` refers to a missing group `%s`", roleBinding.Name, groupName)
			log.V(1).Info(msg, "cluster", c.namespacedName())
			continue
		}

		// Gather users bound to group
		for _, roleSubject := range roleBinding.Spec.Subjects {

			selector := labels.Everything()
			if c.cluster.Spec.Security.RBAC.Selector != nil {
				var err error
				if selector, err = metav1.LabelSelectorAsSelector(c.cluster.Spec.Security.RBAC.Selector); err != nil {
					return nil, err
				}
			}
			cbUser, found := c.k8s.CouchbaseUsers.Get(roleSubject.Name)
			if found {
				if !selector.Matches(labels.Set(cbUser.Labels)) {
					// ignoring non-matching users
					continue
				}
			} else {
				msg := fmt.Sprintf("Rolebinding `%s` refers to a missing user `%s`", roleBinding.Name, roleSubject.Name)
				log.V(1).Info(msg, "cluster", c.namespacedName())
				continue
			}

			// Add group to user
			if user, ok := requestedUsers[cbUser.Name]; !ok {
				user := cbmgr.User{
					ID:     cbUser.Name,
					Name:   cbUser.Spec.FullName,
					Domain: cbmgr.AuthDomain(cbUser.Spec.AuthDomain),
					Groups: []string{groupName},
				}

				// Require password when using internal auth domain
				if cbUser.Spec.AuthDomain == couchbasev2.InternalAuthDomain {
					password, err := c.getRBACAuthPassword(cbUser.Spec.AuthSecret)
					if err != nil {
						return nil, err
					}
					user.Password = password
				}
				requestedUsers[user.ID] = user
			} else {
				// Add additional group to user
				if _, found := couchbasev2.HasItem(groupName, user.Groups); !found {
					user.Groups = append(user.Groups, groupName)
					requestedUsers[user.ID] = user
				}
			}
		}
	}

	// reconcile with existing users
	existingUsers, err := c.client.ListUsers(c.readyMembers())
	if err != nil {
		return nil, nil
	}
	existingUserNames := []string{}
	for _, e := range existingUsers {
		if r, ok := requestedUsers[e.ID]; ok {
			// requested user exists
			// update user if group bindings change
			if !reflect.DeepEqual(r.Groups, e.Groups) || !reflect.DeepEqual(r.Name, e.Name) {
				if err := c.client.CreateUser(c.readyMembers(), r); err != nil {
					return existingUserNames, err
				}
				c.raiseEvent(k8sutil.UserEditEvent(e.ID, c.cluster))
				log.Info("edit CouchbaseUser", "name", e.ID)
			}
		} else {
			// delete unrequested user
			if err := c.client.DeleteUser(c.readyMembers(), *e); err != nil {
				return existingUserNames, err
			}
			c.raiseEvent(k8sutil.UserDeleteEvent(e.ID, c.cluster))
			log.Info("delete CouchbaseUser", "name", e.ID)
		}
		existingUserNames = append(existingUserNames, e.ID)
	}

	// create requested groups that do not exist
	for _, r := range requestedUsers {
		if _, found := couchbasev2.HasItem(r.ID, existingUserNames); !found {

			if err := c.client.CreateUser(c.readyMembers(), r); err != nil {
				return existingUserNames, err
			}
			c.raiseEvent(k8sutil.UserCreateEvent(r.ID, c.cluster))
			log.Info("create CouchbaseUser", "name", r.ID)
		}
	}
	return existingUserNames, nil
}

// Get auth password to be set for user
func (c *Cluster) getRBACAuthPassword(authSecret string) (string, error) {
	var password string
	secret, found := c.k8s.Secrets.Get(authSecret)
	if !found {
		return password, fmt.Errorf("unable to find secret %s", authSecret)
	}

	data := secret.Data
	if dataPassword, ok := data[constants.AuthSecretPasswordKey]; ok {
		password = string(dataPassword)
	} else {
		return password, cberrors.ErrSecretMissingPassword{Reason: authSecret}
	}

	return password, nil
}

// reconcileLDAPSettings synchronizes couchbase ldap settings with requested settings
func (c *Cluster) reconcileLDAPSettings() error {

	// Get current ldap cluster spec
	apiLDAPSettings, err := c.client.GetLDAPSettings(c.readyMembers())
	if err != nil {
		return err
	}
	if ldap := c.cluster.Spec.Security.LDAP; ldap == nil {
		if len(apiLDAPSettings.Hosts) > 0 {
			// Reset settings to default
			settings := cbmgr.LDAPSettings{Encryption: cbmgr.LDAPEncryptionNone}
			return c.client.SetLDAPSettings(c.readyMembers(), &settings)
		}
		// nothing to reconcile
		return nil
	} else {

		// Convert requested ldap spec
		updatedUserDNMapping := []cbmgr.LDAPUserDNMapping{}
		if specDNMapping := ldap.UserDNMapping; specDNMapping != nil {
			for _, dn := range *specDNMapping {
				updatedUserDNMapping = append(updatedUserDNMapping, cbmgr.LDAPUserDNMapping(dn))
			}
		}
		specLDAPSettings := cbmgr.LDAPSettings{
			AuthenticationEnabled: ldap.AuthenticationEnabled,
			AuthorizationEnabled:  ldap.AuthorizationEnabled,
			Hosts:                 ldap.Hosts,
			Port:                  ldap.Port,
			Encryption:            cbmgr.LDAPEncryption(ldap.Encryption),
			EnableCertValidation:  ldap.EnableCertValidation,
			GroupsQuery:           ldap.GroupsQuery,
			BindDN:                ldap.BindDN,
			UserDNMapping:         &updatedUserDNMapping,
			NestedGroupsEnabled:   ldap.NestedGroupsEnabled,
			NestedGroupsMaxDepth:  ldap.NestedGroupsMaxDepth,
			CacheValueLifetime:    ldap.CacheValueLifetime,
		}

		// set cacert if provided and validation cert is enabled
		if specLDAPSettings.EnableCertValidation {
			tlsSecretName := ldap.TLSSecret
			if tlsSecretName != "" {
				tlsSecret, found := c.k8s.Secrets.Get(tlsSecretName)
				if !found {
					return fmt.Errorf("unable to get ldap tls secret `%s`", tlsSecretName)
				}
				ca, ok := tlsSecret.Data[constants.LDAPSecretCACert]
				if !ok {
					return fmt.Errorf("unable to find %s in tls ldap secret", constants.LDAPSecretCACert)
				}
				specLDAPSettings.CACert = string(ca)
			}
		}

		if !reflect.DeepEqual(*apiLDAPSettings, specLDAPSettings) {

			// reconcile and set bind password if provided
			bindSecretName := c.cluster.Spec.Security.LDAP.BindSecret
			if bindSecretName != "" {
				bindSecret, found := c.k8s.Secrets.Get(bindSecretName)
				if !found {
					return fmt.Errorf("unable to get ldap bind secret `%s`", bindSecretName)
				}
				password, ok := bindSecret.Data[constants.LDAPSecretPassword]
				if !ok {
					return fmt.Errorf("unable to find %s in ldap bind secret", constants.LDAPSecretPassword)
				}
				specLDAPSettings.BindPass = string(password)
			}

			// Update ldap settings according requested spec
			return c.client.SetLDAPSettings(c.readyMembers(), &specLDAPSettings)
		}
	}
	return nil
}
