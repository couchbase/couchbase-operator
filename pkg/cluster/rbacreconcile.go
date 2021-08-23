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

func (c *Cluster) getScopesBySelector(scopeSpec couchbasev2.ScopeRoleSpec) []*couchbasev2.CouchbaseScope {
	scopes := []*couchbasev2.CouchbaseScope{}
	selector := labels.Nothing()

	if scopeSpec.Selector != nil {
		s, err := metav1.LabelSelectorAsSelector(scopeSpec.Selector)
		if err != nil {
			log.Error(err, "error while fetching scopes using selector", "labels", scopeSpec.Selector.MatchLabels)
			return scopes
		}

		selector = s
	}

	for _, s := range c.k8s.CouchbaseScopes.List() {
		if selector.Matches(labels.Set(s.Labels)) {
			scopes = append(scopes, s)
		}
	}

	for _, group := range c.k8s.CouchbaseScopeGroups.List() {
		if selector.Matches(labels.Set(group.Labels)) {
			scopes = append(scopes, c.getScopesFromGroup(group)...)
		}
	}

	return scopes
}

func (c *Cluster) getScopesFromGroup(group *couchbasev2.CouchbaseScopeGroup) []*couchbasev2.CouchbaseScope {
	scopes := []*couchbasev2.CouchbaseScope{}

	for _, scopeName := range group.Spec.Names {
		if scope, noErr := c.k8s.CouchbaseScopes.Get(string(scopeName)); noErr {
			scopes = append(scopes, scope)
		} else {
			// here this is a situation where we don't have a CouchbaseScope defined
			// for each scope, instead the group defines the scopes by name.
			scopes = append(scopes, &couchbasev2.CouchbaseScope{
				Spec: couchbasev2.CouchbaseScopeSpec{
					Name: scopeName,
				},
			})
		}
	}

	return scopes
}

func (c *Cluster) getRoleScopes(role couchbasev2.Role) []*couchbasev2.CouchbaseScope {
	scopes := []*couchbasev2.CouchbaseScope{}
	if role.Scopes.Selector != nil {
		scopes = c.getScopesBySelector(role.Scopes)
	}

	for _, scope := range role.Scopes.Resources {
		switch scope.Kind {
		case couchbasev2.CouchbaseScopeKind:
			if scope, noErr := c.k8s.CouchbaseScopes.Get(string(scope.Name)); noErr {
				scopes = append(scopes, scope)
			}
		case couchbasev2.CouchbaseScopeGroupKind:
			if group, noErr := c.k8s.CouchbaseScopeGroups.Get(string(scope.Name)); noErr {
				scopes = append(scopes, c.getScopesFromGroup(group)...)
			}
		}
	}

	return scopes
}

func (c *Cluster) getCollectionsFromGroup(group *couchbasev2.CouchbaseCollectionGroup) []*couchbasev2.CouchbaseCollection {
	collections := []*couchbasev2.CouchbaseCollection{}

	for _, collectionName := range group.Spec.Names {
		if collection, noErr := c.k8s.CouchbaseCollections.Get(string(collectionName)); noErr {
			collections = append(collections, collection)
		} else {
			// Here the group holds the keys to creating the collection and there
			// is no CouchbaseCollection resource object for it.  We'll create one
			// just for processing
			collections = append(collections, &couchbasev2.CouchbaseCollection{
				Spec: couchbasev2.CouchbaseCollectionSpec{
					Name: collectionName,
				},
			})
		}
	}

	return collections
}

func (c *Cluster) getCollectionsBySelector(collectionSpec couchbasev2.CollectionRoleSpec) []*couchbasev2.CouchbaseCollection {
	collections := []*couchbasev2.CouchbaseCollection{}
	selector := labels.Nothing()

	if collectionSpec.Selector != nil {
		s, err := metav1.LabelSelectorAsSelector(collectionSpec.Selector)
		if err != nil {
			log.Error(err, "error while fetching collections using selector")
			return collections
		}

		selector = s
	}

	for _, c := range c.k8s.CouchbaseCollections.List() {
		if selector.Matches(labels.Set(c.Labels)) {
			collections = append(collections, c)
		}
	}

	for _, group := range c.k8s.CouchbaseCollectionGroups.List() {
		if selector.Matches(labels.Set(group.Labels)) {
			collections = append(collections, c.getCollectionsFromGroup(group)...)
		}
	}

	return collections
}

func (c *Cluster) getRoleCollections(role couchbasev2.Role) []*couchbasev2.CouchbaseCollection {
	collections := []*couchbasev2.CouchbaseCollection{}

	if role.Collections.Selector != nil {
		collections = c.getCollectionsBySelector(role.Collections)
	}

	for _, collection := range role.Collections.Resources {
		switch collection.Kind {
		case couchbasev2.CouchbaseCollectionKind:
			if collection, noErr := c.k8s.CouchbaseCollections.Get(string(collection.Name)); noErr {
				collections = append(collections, collection)
			}
		case couchbasev2.CouchbaseCollectionGroupKind:
			if group, noErr := c.k8s.CouchbaseCollectionGroups.Get(string(collection.Name)); noErr {
				collections = append(collections, c.getCollectionsFromGroup(group)...)
			}
		}
	}

	return collections
}

func (c *Cluster) getBucketsBySelector(spec couchbasev2.BucketRoleSpec) []*couchbasev2.CouchbaseBucket {
	buckets := []*couchbasev2.CouchbaseBucket{}
	selector := labels.Nothing()

	if spec.Selector != nil {
		s, err := metav1.LabelSelectorAsSelector(spec.Selector)
		if err != nil {
			log.Error(err, "error while fetching buckets using selector")
			return buckets
		}

		selector = s
	}

	for _, b := range c.k8s.CouchbaseBuckets.List() {
		if selector.Matches(labels.Set(b.Labels)) {
			buckets = append(buckets, b)
		}
	}

	return buckets
}

func (c *Cluster) getRoleBuckets(role couchbasev2.Role) []*couchbasev2.CouchbaseBucket {
	buckets := []*couchbasev2.CouchbaseBucket{}

	if role.Buckets.Selector != nil {
		buckets = c.getBucketsBySelector(role.Buckets)
	}

	for _, bucket := range role.Buckets.Resources {
		if bucket, noErr := c.k8s.CouchbaseBuckets.Get(bucket.Name); noErr {
			buckets = append(buckets, bucket)
		}
	}

	if len(buckets) == 0 && role.Bucket != "" && c.bucketExists(role.Bucket) {
		buckets = append(buckets, &couchbasev2.CouchbaseBucket{
			Spec: couchbasev2.CouchbaseBucketSpec{
				Name: couchbasev2.BucketName(role.Bucket),
			},
		})
	}

	return buckets
}

func (c *Cluster) getRoleCombinations(role couchbasev2.Role, buckets []*couchbasev2.CouchbaseBucket, scopes []*couchbasev2.CouchbaseScope, collections []*couchbasev2.CouchbaseCollection) ([]couchbaseutil.UserRole, error) {
	roles := []couchbaseutil.UserRole{}

	log.Info("role being created", "role", role)

	for _, bucket := range buckets {
		if !c.bucketExists(bucket.GetName()) {
			log.Info("skipping role due to non-existent bucket", "bucket", bucket)
			continue
		}

		scopeList := &couchbaseutil.ScopeList{}
		err := couchbaseutil.ListScopes(bucket.GetName(), scopeList).On(c.api, c.readyMembers())

		if err != nil {
			return nil, err
		}

		for _, scope := range scopes {
			// Check to ensure that scope belongs to bucket
			if !scopeList.HasScope(scope.CouchbaseName()) {
				log.Info("skipping role due to non-existent scope", "bucket", bucket.GetName(), "scope", scope.GetName())
				continue
			}

			cbScope := scopeList.GetScope(scope.CouchbaseName())

			for _, collection := range collections {
				// Check to ensure that collection belongs to the scope
				if !cbScope.HasCollection(collection.CouchbaseName()) {
					log.Info("skipping role due to non-existent collection", "bucket", bucket.GetName(), "scope", scope.GetName(), "collection", collection.GetName())
					continue
				}

				r := couchbaseutil.UserRole{
					Role:           string(role.Name),
					BucketName:     bucket.GetName(),
					ScopeName:      scope.CouchbaseName(),
					CollectionName: collection.CouchbaseName(),
				}

				log.Info("Adding role to group", "role", couchbaseutil.RoleToStr(r))

				roles = append(roles, r)
			}

			// If a selector is passed but returns no results, we should not create
			// a more permissive permission set and therefore skip creating the more permissive permission set.
			// However if no collections are present AND we do not have a selector, that is communicated as an intent to create
			// a higher level permission set.
			if len(collections) == 0 && role.Collections.Selector == nil {
				r := couchbaseutil.UserRole{
					Role:           string(role.Name),
					BucketName:     bucket.GetName(),
					ScopeName:      scope.CouchbaseName(),
					CollectionName: "",
				}

				log.Info("Adding role to group", "role", couchbaseutil.RoleToStr(r))

				roles = append(roles, r)
			}
		}
		// If a selector is passed but returns no results, we should not create
		// a more permissive permission set and therefore skip creating the more permissive permission set.
		// However if no scopes are present AND we do not have a selector, that is communicated as an intent to create
		// a higher level permission set.
		if len(scopes) == 0 && role.Scopes.Selector == nil {
			r := couchbaseutil.UserRole{
				Role:           string(role.Name),
				BucketName:     bucket.GetName(),
				ScopeName:      "",
				CollectionName: "",
			}

			log.Info("Adding role to group", "role", couchbaseutil.RoleToStr(r))

			roles = append(roles, r)
		}
	}

	return roles, nil
}

func (c *Cluster) bucketExists(bucket string) bool {
	bucketList := &couchbaseutil.BucketList{}

	err := couchbaseutil.ListBuckets(bucketList).On(c.api, c.readyMembers())
	if err != nil {
		log.Error(err, "error retrieving bucket list")
		return false
	}

	b, err := bucketList.Get(bucket)

	if err != nil {
		log.Error(err, "error checking for bucket %s", bucket)
		return false
	}

	return b != nil
}

func isVersionGreaterThan(image string, version string) (bool, error) {
	tag, err := k8sutil.CouchbaseVersion(image)
	if err != nil {
		log.Error(err, "error retrieving couchbase version")
		return false, err
	}

	available, err := couchbaseutil.VersionAfter(tag, version)
	if err != nil {
		log.Error(err, "error comparing versions")
		return false, err
	}

	return available, nil
}

func (c *Cluster) handleRole(role couchbasev2.Role) []couchbaseutil.UserRole {
	roles := []couchbaseutil.UserRole{}
	// Roles that relate to buckets are scoped to either one, or
	// all of them.  We used to have a special mutator that would fill
	// in the default "all the things" if not specified for bucket
	// specific roles, but we don't mutate any more, as this is becoming
	// problematic in the ecosystem.  Instead we apply a default here.
	// The other option is just to default to "*" but filter it out from
	// non-bucket roles.
	if role.Bucket == "" && role.Buckets.Selector == nil && len(role.Buckets.Resources) == 0 && couchbasev2.IsBucketRole(role.Name) {
		// If we are all buckets for a bucket role, skip processing scopes/collections
		roles = append(roles, couchbaseutil.UserRole{
			Role:           string(role.Name),
			BucketName:     "*",
			ScopeName:      "",
			CollectionName: "",
		})

		return roles
	}

	available, err := isVersionGreaterThan(c.cluster.Spec.Image, "7.0.0")
	if err != nil {
		log.Error(err, "error during rbac reconciliation due to version check")
		return roles
	}

	if available {
		buckets := c.getRoleBuckets(role)

		collections := c.getRoleCollections(role)

		scopes := c.getRoleScopes(role)

		roleCombinations, err := c.getRoleCombinations(role, buckets, scopes, collections)

		if err != nil {
			// handle validation errors
			return roles
		}

		roles = append(roles, roleCombinations...)
	} else {
		roles = append(roles, couchbaseutil.UserRole{
			Role:           string(role.Name),
			BucketName:     role.Bucket,
			ScopeName:      "",
			CollectionName: "",
		})
	}

	return roles
}

// generateGroups generates the list of groups that should exist, under the control
// of the RBAC label selector.
func (c *Cluster) generateGroups() (map[string]couchbaseutil.Group, error) {
	// gather selected groups
	selector := labels.Everything()

	if c.cluster.Spec.Security.RBAC.Selector != nil {
		s, err := metav1.LabelSelectorAsSelector(c.cluster.Spec.Security.RBAC.Selector)
		if err != nil {
			return nil, err
		}

		selector = s
	}

	groups := map[string]couchbaseutil.Group{}

	for _, g := range c.k8s.CouchbaseGroups.List() {
		if !selector.Matches(labels.Set(g.Labels)) {
			continue
		}

		group := couchbaseutil.Group{
			ID:           g.Name,
			LDAPGroupRef: g.Spec.LDAPGroupRef,
		}

		for _, role := range g.Spec.Roles {
			group.Roles = append(group.Roles, c.handleRole(role)...)
		}

		groups[g.Name] = group
	}

	return groups, nil
}

// reconcileGroups creates, edits, removes server groups according to requested configuration.
func (c *Cluster) reconcileGroups() ([]string, error) {
	requestedGroups, err := c.generateGroups()
	if err != nil {
		return nil, err
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

// getUsersForRoleBinding returns the subjects that are bound to the rolebinding
// but under the control of the RBAC label selector.
func (c *Cluster) gatherSubjectsForRoleBinding(roleBinding *couchbasev2.CouchbaseRoleBinding) ([]couchbasev2.CouchbaseUser, error) {
	selector := labels.Everything()

	if c.cluster.Spec.Security.RBAC.Selector != nil {
		s, err := metav1.LabelSelectorAsSelector(c.cluster.Spec.Security.RBAC.Selector)
		if err != nil {
			return nil, err
		}

		selector = s
	}

	var subjects []couchbasev2.CouchbaseUser

	for _, subject := range roleBinding.Spec.Subjects {
		user, ok := c.k8s.CouchbaseUsers.Get(subject.Name)
		if !ok {
			log.V(1).Info("Rolebinding missing subject", "cluster", c.namespacedName(), "rolebinding", roleBinding.Name, "subject", subject.Name)
			continue
		}

		if !selector.Matches(labels.Set(user.Labels)) {
			continue
		}

		subjects = append(subjects, *user)
	}

	return subjects, nil
}

// generateUsers generates all the user configurations that can be created, under the
// control of the groups parameter (e.g. the group must exist before creating the dependent
// user).
func (c *Cluster) generateUsers(groups []string) (map[string]couchbaseutil.User, error) {
	users := map[string]couchbaseutil.User{}

	// For every rolebinding (SM: this is not filtered, why??) check if the
	// group exists, if it does, then accumulate the subjects, that are selected
	// by the cluster.
	for _, binding := range c.k8s.CouchbaseRoleBindings.List() {
		group := binding.Spec.RoleRef.Name

		// Group doesn't exist, so warn that we cannot add any users for it.
		if _, ok := couchbasev2.HasItem(group, groups); !ok {
			log.V(1).Info("Rolebinding missing group", "cluster", c.namespacedName(), "rolebinding", binding.Name, "group", group)
			continue
		}

		subjects, err := c.gatherSubjectsForRoleBinding(binding)
		if err != nil {
			return nil, err
		}

		// For each user we've found for the binding, either use the existing
		// internal user we have cached, or create a new one, then append the
		// group (thus accumulating multiple groups for a specific user).
		for _, subject := range subjects {
			user, ok := users[subject.Name]
			if !ok {
				user = couchbaseutil.User{
					ID:     subject.Name,
					Name:   subject.Spec.FullName,
					Domain: couchbaseutil.AuthDomain(subject.Spec.AuthDomain),
				}

				if subject.Spec.AuthDomain == couchbasev2.InternalAuthDomain {
					password, err := c.getRBACAuthPassword(subject.Spec.AuthSecret)
					if err != nil {
						return nil, err
					}

					user.Password = password
				}
			}

			user.Groups = append(user.Groups, group)

			users[subject.Name] = user
		}
	}

	return users, nil
}

// reconcileUsers creates, edits, removes server users according to requested configuration.
// The groups parameters is a list of groups that are known to exist at this moment in
// time in Couchbase server, this is used to only add users to an existing group.
func (c *Cluster) reconcileUsers(groups []string) ([]string, error) {
	requestedUsers, err := c.generateUsers(groups)
	if err != nil {
		return nil, err
	}

	// reconcile with existing users
	existingUsers := &couchbaseutil.UserList{}
	if err := couchbaseutil.ListUsers(existingUsers).On(c.api, c.readyMembers()); err != nil {
		return nil, err
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
				return fmt.Errorf("%w: group `%s` with role `%s` references a bucket which does not exist: %s", errors.NewStackTracedError(errors.ErrResourceRequired), group.ID, role.Role, role.BucketName)
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
		return password, fmt.Errorf("%w: unable to find secret %s", errors.NewStackTracedError(errors.ErrResourceRequired), authSecret)
	}

	data := secret.Data
	if dataPassword, ok := data[constants.AuthSecretPasswordKey]; ok {
		password = string(dataPassword)
	} else {
		return password, fmt.Errorf("%w: rbac secret missing key %s", errors.NewStackTracedError(errors.ErrResourceAttributeRequired), constants.AuthSecretPasswordKey)
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
	specLDAPSettings := &couchbaseutil.LDAPSettings{
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
				return fmt.Errorf("%w: unable to get ldap tls secret `%s`", errors.NewStackTracedError(errors.ErrResourceRequired), tlsSecretName)
			}

			ca, ok := tlsSecret.Data[constants.LDAPSecretCACert]
			if !ok {
				return fmt.Errorf("%w: unable to find %s in tls ldap secret", errors.NewStackTracedError(errors.ErrResourceAttributeRequired), constants.LDAPSecretCACert)
			}

			specLDAPSettings.CACert = string(ca)
		}
	}

	// API will do some whack things that need fixing:
	// * Fills in the password with junk
	apiLDAPSettings.BindPass = ""

	if !reflect.DeepEqual(apiLDAPSettings, specLDAPSettings) {
		c.logUpdate(apiLDAPSettings, specLDAPSettings)

		// reconcile and set bind password if provided
		bindSecretName := c.cluster.Spec.Security.LDAP.BindSecret
		if bindSecretName != "" {
			bindSecret, found := c.k8s.Secrets.Get(bindSecretName)
			if !found {
				return fmt.Errorf("%w: unable to get ldap bind secret `%s`", errors.NewStackTracedError(errors.ErrResourceRequired), bindSecretName)
			}

			password, ok := bindSecret.Data[constants.LDAPSecretPassword]
			if !ok {
				return fmt.Errorf("%w: unable to find %s in ldap bind secret", errors.NewStackTracedError(errors.ErrResourceAttributeRequired), constants.LDAPSecretPassword)
			}

			specLDAPSettings.BindPass = string(password)
		}

		// Update ldap settings according requested spec
		return couchbaseutil.SetLDAPSettings(specLDAPSettings).On(c.api, c.readyMembers())
	}

	return nil
}
