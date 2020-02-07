package cluster

import (
	"fmt"
	"reflect"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/cluster/rbac"
	cberrors "github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
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

	// Get requested RBAC resources
	requestResources, err := c.gatherRequestResources()
	if err != nil {
		return err
	}

	// Get actual RBAC resources
	actualResources, err := c.gatherActualResources()
	if err != nil {
		return err
	}

	// Create resources being requested which do not actually exist,
	// or update if they exist but differ
	for _, requested := range requestResources.List() {
		if actual := actualResources.Get(requested.BindType(), requested.Name()); actual == nil {
			// create
			if err := c.handleResourceAction(rbac.ResourceActionCreated, requested); err != nil {
				return err
			}
		} else if !requested.Equal(actual) {
			// update
			if err := c.handleResourceAction(rbac.ResourceActionEdited, requested); err != nil {
				return err
			}
		}
	}

	// Delete resources which actually exist but aren't being requested
	for _, actual := range actualResources.List() {
		if requested := requestResources.Get(actual.BindType(), actual.Name()); requested == nil {
			if err := c.handleResourceAction(rbac.ResourceActionDeleted, actual); err != nil {
				return err
			}
		}
	}

	// Update status to reflect requested resources
	c.cluster.Status.Groups = requestResources.GetNamedResources(couchbasev2.RoleBindingTypeGroup)
	c.cluster.Status.Users = requestResources.GetNamedResources(couchbasev2.RoleBindingTypeUser)

	return nil
}

// gatherRequestResources gets all requested RBAC resources
func (c *Cluster) gatherRequestResources() (rbac.ResourceList, error) {

	resourceList := make(rbac.ResourceList)

	// Fetch roles referred to by bindings
	couchbaseRoleBindings := c.k8s.CouchbaseRoleBindings.List()
	for _, roleBinding := range couchbaseRoleBindings {

		// gather roles
		roles := []couchbasev2.Role{}
		roleRef := roleBinding.Spec.RoleRef
		roleName := roleRef.Name
		couchbaseRole, found := c.k8s.CouchbaseRoles.Get(roleName)
		var err error

		if found {
			roles = append(roles, couchbaseRole.Spec.Roles...)
		} else {
			// warn if role is missing because resource may
			// be deleted if there are no roles to bind
			msg := fmt.Sprintf("Rolebinding `%s` refers to a missing role `%s`", roleBinding.Name, roleName)
			log.V(1).Info(msg, "cluster", c.namespacedName())
			continue
		}

		// when referenced roles are missing, then resources bound to the roles
		// will also be deleted unless found in a different rolebinding
		if len(roles) == 0 {
			continue
		}

		// Gather roles for each subject
		for _, roleSubject := range roleBinding.Spec.Subjects {

			// roleSubjects are version gated
			if roleSubject.Kind == couchbasev2.RoleBindingTypeGroup {
				if !c.allowRBACFeature(RBACFeatureGroupAuth) {
					log.Info(fmt.Sprintf("Couchbase Groups require version: %s, please remove from role binding `%s`", constants.CouchbaseVersion650, roleBinding.Name), "cluster", c.namespacedName())
					continue
				}
			}

			for _, r := range roles {

				// Check for existing subject
				resource := resourceList.Get(roleSubject.Kind, roleSubject.Name)
				if resource == nil {

					// Fetch subject from k8s
					if resource, err = c.getRoleBindingSubject(roleSubject); err == nil {
						// Add to list
						resourceList.Add(resource)
					} else {
						if err != cberrors.ErrResourceLabelMismatch {
							msg := fmt.Sprintf("Rolebinding `%s` refers to a missing role subject, `%s` will not be created", roleBinding.Name, roleSubject.Name)
							log.V(1).Info(msg, "cluster", c.namespacedName())
						}
						continue
					}
				}

				// Add role to subject
				resource.AddRole(cbmgr.UserRole{
					Role:       r.Name,
					BucketName: r.Bucket,
				})
			}
		}
	}

	return resourceList, nil
}

// getRoleBindingSubject gets resource referred to by a role subject
func (c *Cluster) getRoleBindingSubject(roleSubject couchbasev2.CouchbaseRoleBindingSubject) (rbac.ResourceInterface, error) {

	opts := rbac.ResourceOpts{}

	// only choose selected resources
	selector := labels.Everything()
	if c.cluster.Spec.Security.RBAC.Selector != nil {
		var err error
		if selector, err = metav1.LabelSelectorAsSelector(c.cluster.Spec.Security.RBAC.Selector); err != nil {
			return nil, err
		}
	}

	switch roleSubject.Kind {
	case couchbasev2.RoleBindingTypeUser:

		// fetch resource as couchbase user
		if user, found := c.k8s.CouchbaseUsers.Get(roleSubject.Name); found {

			if !selector.Matches(labels.Set(user.Labels)) {
				return nil, cberrors.ErrResourceLabelMismatch
			}

			// Require password when using internal auth domain
			if user.Spec.AuthDomain == couchbasev2.InternalAuthDomain {
				password, err := c.getRBACAuthPassword(user.Spec.AuthSecret)
				if err != nil {
					return nil, err
				}
				opts.Password = password
			}
			return rbac.NewResource(user, &opts)
		}

	case couchbasev2.RoleBindingTypeGroup:
		// fetch resource as couchbase group
		if group, found := c.k8s.CouchbaseGroups.Get(roleSubject.Name); found {
			if !selector.Matches(labels.Set(group.Labels)) {
				return nil, cberrors.ErrResourceLabelMismatch
			}
			return rbac.NewResource(group, &opts)
		}
	default:
		return nil, fmt.Errorf("unknown role binding type: %s", roleSubject.Kind)
	}

	return nil, fmt.Errorf("unable to get role subject %s", roleSubject.Name)
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

// gatherActualResources returns resources which exist on couchbase server
func (c *Cluster) gatherActualResources() (rbac.ResourceList, error) {

	// gather users
	resources, err := rbac.ListUserResources(c.client, c.readyMembers())
	if err != nil {
		return nil, err
	}
	// gather groups
	if c.allowRBACFeature(RBACFeatureGroupAuth) {
		groups, err := rbac.ListGroupResources(c.client, c.readyMembers())
		if err != nil {
			return nil, err
		}
		resources.Extend(groups)
	}

	return resources, nil
}

// handleResourceAction performs action on resource and raises appropriate event
func (c *Cluster) handleResourceAction(action rbac.ResourceAction, resource rbac.ResourceInterface) error {
	event, err := resource.Do(action, c.cluster, c.client, c.readyMembers())
	if err != nil {
		return err
	}
	c.raiseEvent(event)
	log.Info(fmt.Sprintf("%s %s", action, resource.BindType()), "name", resource.Name())
	return nil
}

// reconcileLDAPSettings synchronizes couchbase ldap settings with requested settings
func (c *Cluster) reconcileLDAPSettings() error {

	if !c.allowRBACFeature(RBACFeatureLDAPAuth) {
		return nil
	}

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

// allowRBACFeature determines if a feature used for RBAC is allowed
// for the current version
func (c *Cluster) allowRBACFeature(feature RBACFeature) bool {
	version, err := couchbaseutil.NewVersion(c.cluster.Status.CurrentVersion)
	if err != nil {
		log.Error(err, fmt.Sprintf("Unable to get current cluster version, not allowing rbac feature: %s", feature), "cluster", c.namespacedName())
		return false
	}
	switch feature {
	case RBACFeatureLDAPAuth:
		required, _ := couchbaseutil.NewVersion(constants.CouchbaseVersion650)
		if version.Less(required) {
			if c.cluster.Spec.Security.LDAP != nil {
				message := fmt.Sprintf("LDAP Security settings not allowed for version: %s, requires: %s", version, required)
				log.Info(message, "cluster", c.namespacedName())
			}
			return false
		}
	case RBACFeatureGroupAuth:
		required, _ := couchbaseutil.NewVersion(constants.CouchbaseVersion650)
		if version.Less(required) {
			return false
		}
	}
	return true
}
