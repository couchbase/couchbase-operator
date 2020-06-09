package cluster

import (
	"fmt"
	"reflect"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

type RBACFeature string

const (
	RBACFeatureLDAPAuth  RBACFeature = "ldap"
	RBACFeatureGroupAuth RBACFeature = "groups"
)

// reconcileRBACResources compares requested and actual rbac resources
// creates, updates, and deletes where necessary.
func (c *Cluster) reconcileRBACResources() error {
	if !c.cluster.Spec.Security.RBAC.Managed {
		return nil
	}

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

// reconcileGroups creates, edits, removes server groups according to requested configuration.
func (c *Cluster) reconcileGroups() ([]string, error) {
	// gather selected groups
	selector := labels.Everything()

	if c.cluster.Spec.Security.RBAC.Selector != nil {
		var err error
		if selector, err = metav1.LabelSelectorAsSelector(c.cluster.Spec.Security.RBAC.Selector); err != nil {
			return nil, err
		}
	}

	requestedGroups := make(map[string]couchbaseutil.Group)

	for _, cbGroup := range c.k8s.CouchbaseGroups.List() {
		if selector.Matches(labels.Set(cbGroup.Labels)) {
			group := couchbaseutil.Group{
				ID:           cbGroup.Name,
				LDAPGroupRef: cbGroup.Spec.LDAPGroupRef,
			}

			// copy roles to group
			for _, role := range cbGroup.Spec.Roles {
				group.Roles = append(group.Roles, couchbaseutil.UserRole{
					Role:       string(role.Name),
					BucketName: role.Bucket,
				})
			}

			requestedGroups[group.ID] = group
		}
	}

	// reconcile with existing groups
	existingGroupNames := []string{}

	existingGroups := &couchbaseutil.GroupList{}
	if err := couchbaseutil.ListGroups(existingGroups).On(c.api, c.readyMembers()); err != nil {
		return existingGroupNames, err
	}

	for _, group := range *existingGroups {
		e := group

		if r, ok := requestedGroups[e.ID]; ok {
			// update changed group
			rolesMatch := reflect.DeepEqual(couchbaseutil.RolesToStr(r.Roles), couchbaseutil.RolesToStr(e.Roles))
			if !rolesMatch || r.LDAPGroupRef != e.LDAPGroupRef {
				if err := c.createGroup(r); err != nil {
					return existingGroupNames, err
				}

				c.raiseEvent(k8sutil.GroupEditEvent(e.ID, c.cluster))
				log.Info("edit CouchbaseGroup", "name", e.ID)
			}
		} else {
			// delete unrequested group
			if err := couchbaseutil.DeleteGroup(&e).On(c.api, c.readyMembers()); err != nil {
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
			if err := c.createGroup(group); err != nil {
				return existingGroupNames, err
			}

			c.raiseEvent(k8sutil.GroupCreateEvent(group.ID, c.cluster))
			log.Info("create CouchbaseGroup", "name", group.ID)
		}
	}

	// create
	return existingGroupNames, nil
}

// reconcileUsers creates, edits, removes server users according to requested configuration.
func (c *Cluster) reconcileUsers(groups []string) ([]string, error) {
	// Map of requested users by name
	requestedUsers := make(map[string]couchbaseutil.User)

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
				user := couchbaseutil.User{
					ID:     cbUser.Name,
					Name:   cbUser.Spec.FullName,
					Domain: couchbaseutil.AuthDomain(cbUser.Spec.AuthDomain),
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
			} else if _, found := couchbasev2.HasItem(groupName, user.Groups); !found {
				// Add additional group to user
				user.Groups = append(user.Groups, groupName)
				requestedUsers[user.ID] = user
			}
		}
	}

	// reconcile with existing users
	existingUsers := &couchbaseutil.UserList{}
	if err := couchbaseutil.ListUsers(existingUsers).On(c.api, c.readyMembers()); err != nil {
		return nil, nil
	}

	existingUserNames := []string{}

	for _, user := range *existingUsers {
		e := user

		if r, ok := requestedUsers[e.ID]; ok {
			// requested user exists
			// update user if group bindings change
			if !reflect.DeepEqual(r.Groups, e.Groups) || !reflect.DeepEqual(r.Name, e.Name) {
				if err := couchbaseutil.CreateUser(&r).On(c.api, c.readyMembers()); err != nil {
					return existingUserNames, err
				}

				c.raiseEvent(k8sutil.UserEditEvent(e.ID, c.cluster))
				log.Info("edit CouchbaseUser", "name", e.ID)
			}
		} else {
			// delete unrequested user
			if err := couchbaseutil.DeleteUser(&e).On(c.api, c.readyMembers()); err != nil {
				return existingUserNames, err
			}

			c.raiseEvent(k8sutil.UserDeleteEvent(e.ID, c.cluster))
			log.Info("delete CouchbaseUser", "name", e.ID)
		}

		existingUserNames = append(existingUserNames, e.ID)
	}

	// create requested groups that do not exist
	for i := range requestedUsers {
		r := requestedUsers[i]

		if _, found := couchbasev2.HasItem(r.ID, existingUserNames); !found {
			if err := couchbaseutil.CreateUser(&r).On(c.api, c.readyMembers()); err != nil {
				return existingUserNames, err
			}

			c.raiseEvent(k8sutil.UserCreateEvent(r.ID, c.cluster))
			log.Info("create CouchbaseUser", "name", r.ID)
		}
	}

	return existingUserNames, nil
}

// create couchbase group with verification.
func (c *Cluster) createGroup(group couchbaseutil.Group) error {
	for _, role := range group.Roles {
		if role.BucketName != "" && role.BucketName != "*" {
			buckets := couchbaseutil.BucketList{}
			if err := couchbaseutil.ListBuckets(&buckets).On(c.api, c.readyMembers()); err != nil {
				return err
			}

			if _, err := buckets.Get(role.BucketName); err != nil {
				return fmt.Errorf("%w: group `%s` with role `%s` references a bucket which does not exist: %s", errors.ErrResourceRequired, group.ID, role.Role, role.BucketName)
			}
		}
	}

	return couchbaseutil.CreateGroup(&group).On(c.api, c.readyMembers())
}

// Get auth password to be set for user.
func (c *Cluster) getRBACAuthPassword(authSecret string) (string, error) {
	var password string

	secret, found := c.k8s.Secrets.Get(authSecret)
	if !found {
		return password, fmt.Errorf("%w: unable to find secret %s", errors.ErrResourceRequired, authSecret)
	}

	data := secret.Data
	if dataPassword, ok := data[constants.AuthSecretPasswordKey]; ok {
		password = string(dataPassword)
	} else {
		return password, fmt.Errorf("%w: rbac secret missing key %s", errors.ErrResourceAttributeRequired, constants.AuthSecretPasswordKey)
	}

	return password, nil
}

// reconcileLDAPSettings synchronizes couchbase ldap settings with requested settings.
func (c *Cluster) reconcileLDAPSettings() error {
	// Get current ldap cluster spec
	apiLDAPSettings := &couchbaseutil.LDAPSettings{}
	if err := couchbaseutil.GetLDAPSettings(apiLDAPSettings).On(c.api, c.readyMembers()); err != nil {
		return err
	}

	ldap := c.cluster.Spec.Security.LDAP
	if ldap == nil {
		if len(apiLDAPSettings.Hosts) > 0 {
			// Reset settings to default
			settings := couchbaseutil.LDAPSettings{Encryption: couchbaseutil.LDAPEncryptionNone}
			return couchbaseutil.SetLDAPSettings(&settings).On(c.api, c.readyMembers())
		}

		// nothing to reconcile
		return nil
	}

	// Convert requested ldap spec
	specLDAPSettings := couchbaseutil.LDAPSettings{
		AuthenticationEnabled: ldap.AuthenticationEnabled,
		AuthorizationEnabled:  ldap.AuthorizationEnabled,
		Hosts:                 ldap.Hosts,
		Port:                  ldap.Port,
		Encryption:            couchbaseutil.LDAPEncryption(ldap.Encryption),
		EnableCertValidation:  ldap.EnableCertValidation,
		GroupsQuery:           ldap.GroupsQuery,
		BindDN:                ldap.BindDN,
		UserDNMapping:         couchbaseutil.LDAPUserDNMapping(ldap.UserDNMapping),
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
				return fmt.Errorf("%w: unable to get ldap tls secret `%s`", errors.ErrResourceRequired, tlsSecretName)
			}

			ca, ok := tlsSecret.Data[constants.LDAPSecretCACert]
			if !ok {
				return fmt.Errorf("%w: unable to find %s in tls ldap secret", errors.ErrResourceAttributeRequired, constants.LDAPSecretCACert)
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
				return fmt.Errorf("%w: unable to get ldap bind secret `%s`", errors.ErrResourceRequired, bindSecretName)
			}

			password, ok := bindSecret.Data[constants.LDAPSecretPassword]
			if !ok {
				return fmt.Errorf("%w: unable to find %s in ldap bind secret", errors.ErrResourceAttributeRequired, constants.LDAPSecretPassword)
			}

			specLDAPSettings.BindPass = string(password)
		}

		// Update ldap settings according requested spec
		return couchbaseutil.SetLDAPSettings(&specLDAPSettings).On(c.api, c.readyMembers())
	}

	return nil
}
