package cluster

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

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

	err = c.reconcileTemporaryPasswords(false)
	if err != nil {
		return err
	}

	return nil
}

func (c *Cluster) createCloudNativeGatewayAdminUser() error {
	userName := fmt.Sprintf("%s@%s", k8sutil.CngAdminUserNamePrefix, c.cluster.Name)
	cngUser := couchbaseutil.User{
		ID:     userName,
		Name:   userName,
		Domain: couchbaseutil.InternalAuthDomain,
	}

	secretName := fmt.Sprintf("%s-%s", k8sutil.CngAdminUserSecretPrefix, c.cluster.Name)
	password, err := c.getRBACAuthPassword(secretName)

	if err != nil {
		return err
	}

	cngUser.Password = password
	cngUser.Roles = append(cngUser.Roles, couchbaseutil.UserRole{
		Role: string(couchbasev2.RoleFullAdmin),
	})

	return couchbaseutil.CreateUser(&cngUser).On(c.api, c.readyMembers())
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
		case couchbasev2.CouchbaseScopeKindScope:
			if scope, noErr := c.k8s.CouchbaseScopes.Get(string(scope.Name)); noErr {
				scopes = append(scopes, scope)
			}
		case couchbasev2.CouchbaseScopeKindScopeGroup:
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
		case couchbasev2.CouchbaseCollectionKindCollection:
			if collection, noErr := c.k8s.CouchbaseCollections.Get(string(collection.Name)); noErr {
				collections = append(collections, collection)
			}
		case couchbasev2.CouchbaseCollectionKindCollectionGroup:
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
			ObjectMeta: metav1.ObjectMeta{
				Name: role.Bucket,
			},
		})
	}

	return buckets
}

func getScopeList(bucket *couchbasev2.CouchbaseBucket, scopes []*couchbasev2.CouchbaseScope, c *Cluster) (*couchbaseutil.ScopeList, error) {
	// If we have no scopes, no point in making this request for every bucket.  Also
	// makes this method usable for non-7.0.0+ clusters
	scopeList := &couchbaseutil.ScopeList{}
	if len(scopes) > 0 {
		err := couchbaseutil.ListScopes(bucket.GetName(), scopeList).On(c.api, c.readyMembers())
		if err != nil {
			return nil, err
		}
	}

	return scopeList, nil
}

func (c *Cluster) getRoleCombinations(role couchbasev2.Role, buckets []*couchbasev2.CouchbaseBucket, scopes []*couchbasev2.CouchbaseScope, collections []*couchbasev2.CouchbaseCollection) ([]couchbaseutil.UserRole, error) {
	roles := []couchbaseutil.UserRole{}

	log.V(2).Info("Generated role being created", "cluster", c.cluster.NamespacedName(), "role", role)

	for _, bucket := range buckets {
		if !c.bucketExists(bucket.GetName()) {
			log.V(2).Info("skipping role due to non-existent bucket", "cluster", c.cluster.NamespacedName(), "bucket", bucket)
			continue
		}

		scopeList, err := getScopeList(bucket, scopes, c)
		if err != nil {
			return nil, err
		}

		for _, scope := range scopes {
			// Check to ensure that scope belongs to bucket
			if !scopeList.HasScope(scope.CouchbaseName()) {
				log.V(2).Info("skipping role due to non-existent scope", "cluster", c.cluster.NamespacedName(), "bucket", bucket.GetName(), "scope", scope.GetName())
				continue
			}

			cbScope := scopeList.GetScope(scope.CouchbaseName())

			for _, collection := range collections {
				// Check to ensure that collection belongs to the scope
				if !cbScope.HasCollection(collection.CouchbaseName()) {
					log.V(2).Info("skipping role due to non-existent collection", "cluster", c.cluster.NamespacedName(), "bucket", bucket.GetName(), "scope", scope.GetName(), "collection", collection.GetName())
					continue
				}

				r := couchbaseutil.UserRole{
					Role:           string(role.Name),
					BucketName:     bucket.GetName(),
					ScopeName:      scope.CouchbaseName(),
					CollectionName: collection.CouchbaseName(),
				}

				log.V(2).Info("Adding generated role to generated group", "cluster", c.cluster.NamespacedName(), "role", couchbaseutil.RoleToStr(r))

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

				log.V(2).Info("Adding generated role to generated group", "cluster", c.cluster.NamespacedName(), "role", couchbaseutil.RoleToStr(r))

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

			log.V(2).Info("Adding generated role to generated group", "cluster", c.cluster.NamespacedName(), "role", couchbaseutil.RoleToStr(r))

			roles = append(roles, r)
		}
	}

	return roles, nil
}

func (c *Cluster) bucketExists(bucket string) bool {
	bucketList := &couchbaseutil.BucketList{}

	err := couchbaseutil.ListBuckets(bucketList).On(c.api, c.readyMembers())
	if err != nil {
		log.Info("error retrieving bucket list", "cluster", c.cluster.NamespacedName())
		return false
	}

	b, err := bucketList.Get(bucket)
	if err != nil {
		log.Info("Error retrieving bucket", "cluster", c.cluster.NamespacedName(), "bucket", bucket)
		return false
	}

	return b != nil
}

func (c *Cluster) handleRole(role couchbasev2.Role) ([]couchbaseutil.UserRole, error) {
	roles := []couchbaseutil.UserRole{}

	// Adding a short circuit here for cluster roles, as we don't need to go through all the
	// ceremony to expand buckets, scopes, or collections when we don't care.
	if couchbasev2.IsClusterRole(role.Name) {
		roles = append(roles, couchbaseutil.UserRole{
			Role:           string(role.Name),
			BucketName:     "",
			ScopeName:      "",
			CollectionName: "",
		})

		return roles, nil
	}

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

		return roles, nil
	}

	available, err := c.IsAtLeastVersion("7.0.0")
	if err != nil {
		log.Error(err, "error during rbac reconciliation due to version check")
		return roles, err
	}

	buckets := c.getRoleBuckets(role)
	scopes := make([]*couchbasev2.CouchbaseScope, 0)
	collections := make([]*couchbasev2.CouchbaseCollection, 0)

	if available {
		collections = c.getRoleCollections(role)
		scopes = c.getRoleScopes(role)
	}

	roleCombinations, err := c.getRoleCombinations(role, buckets, scopes, collections)
	if err != nil {
		// handle validation errors
		return roles, err
	}

	roles = append(roles, roleCombinations...)

	return roles, nil
}

// generateGroups generates the list of groups that should exist, under the control
// of the RBAC label selector.
//
//nolint:gocognit // This function orchestrates selection, migration and construction of groups.
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
		apiGroup, found := c.k8s.CouchbaseGroups.Get(g.Name)
		if found && !couchbaseutil.ShouldReconcile(apiGroup.GetAnnotations()) {
			continue
		}

		if !selector.Matches(labels.Set(g.Labels)) {
			continue
		}

		group := couchbaseutil.Group{
			ID:           g.Name,
			LDAPGroupRef: g.Spec.LDAPGroupRef,
		}

		// Migrate deprecated roles (if any) before handling them, but only
		// when the target Couchbase Server version is Morpheus (8.0+) where
		// the deprecated names are no longer valid. For older server
		// versions, accept the old names as-is.
		migrated := g.Spec.Roles
		if ok, err := c.IsAtLeastVersion("8.0.0"); err == nil && ok {
			migrated = couchbasev2.MigrateDeprecatedRoles(g.Spec.Roles)
			// emit an event to notify the user that migration occurred
			deprecated := []string{}

			for _, r := range g.Spec.Roles {
				rn := string(r.Name)
				if rn == "security_admin_local" || rn == "security_admin_external" {
					deprecated = append(deprecated, rn)
				}
			}

			if len(deprecated) > 0 {
				details := strings.Join(deprecated, ",")
				c.raiseEvent(k8sutil.RolesMigratedEvent(g.Name, details, c.cluster))
			}
		}

		for _, role := range migrated {
			newRoles, err := c.handleRole(role)
			if err != nil {
				return groups, err
			}

			group.Roles = append(group.Roles, newRoles...)
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

		apiGroup, found := c.k8s.CouchbaseGroups.Get(e.ID)
		if found && !couchbaseutil.ShouldReconcile(apiGroup.Annotations) {
			continue
		}

		if r, ok := requestedGroups[e.ID]; ok {
			// update changed group
			rolesMatch := reflect.DeepEqual(couchbaseutil.RolesToStr(r.Roles), couchbaseutil.RolesToStr(e.Roles))
			if !rolesMatch || r.LDAPGroupRef != e.LDAPGroupRef {
				if err := c.createGroup(r); err != nil {
					return existingGroupNames, err
				}

				c.raiseEvent(k8sutil.GroupEditEvent(e.ID, c.cluster))
				log.Info("edit CouchbaseGroup", "cluster", c.cluster.NamespacedName(), "name", e.ID)
			}

			existingGroupNames = append(existingGroupNames, e.ID)
		} else {
			// delete unrequested group
			if err := couchbaseutil.DeleteGroup(&e).On(c.api, c.readyMembers()); err != nil {
				return existingGroupNames, err
			}

			c.raiseEvent(k8sutil.GroupDeleteEvent(e.ID, c.cluster))
			log.Info("delete CouchbaseGroup", "cluster", c.cluster.NamespacedName(), "name", e.ID)
		}
	}

	// create requested groups that do not exist
	for _, group := range requestedGroups {
		if _, found := couchbasev2.HasItem(group.ID, existingGroupNames); !found {
			if err := c.createGroup(group); err != nil {
				return existingGroupNames, err
			}

			c.raiseEvent(k8sutil.GroupCreateEvent(group.ID, c.cluster))
			log.Info("create CouchbaseGroup", "cluster", c.cluster.NamespacedName(), "name", group.ID)
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

func (c *Cluster) createAPIUserFromSubject(subject couchbasev2.CouchbaseUser, atLeast80 bool) (couchbaseutil.User, error) {
	user := couchbaseutil.User{
		ID:     subject.Spec.Name,
		Name:   subject.Spec.FullName,
		Domain: couchbaseutil.AuthDomain(subject.Spec.AuthDomain),
	}

	if atLeast80 {
		userLockedDefault := false
		user.Locked = &userLockedDefault

		if subject.Spec.Locked != nil {
			user.Locked = subject.Spec.Locked
		}

		// If we require an initial password change, we'll set this to true here but this should be overridden if the user already exists.
		if subject.Spec.Password != nil && subject.Spec.Password.RequireInitialChange != nil {
			user.TemporaryPassword = subject.Spec.Password.RequireInitialChange
		}
	}

	if subject.Spec.AuthDomain == couchbasev2.InternalAuthDomain {
		password, err := c.getRBACAuthPassword(subject.Spec.AuthSecret)
		if err != nil {
			return user, err
		}

		user.Password = password
	}

	return user, nil
}

// generateUsers generates all the user configurations that can be created, under the
// control of the groups parameter (e.g. the group must exist before creating the dependent
// user).
func (c *Cluster) generateUsers(groups []string) (map[string]couchbaseutil.User, map[string]bool, error) {
	users := map[string]couchbaseutil.User{}
	unReconcilableUsers := map[string]bool{}

	atLeast80, err := c.IsAtLeastVersion("8.0.0")
	if err != nil {
		return nil, nil, err
	}

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
			return nil, nil, err
		}

		// For each user we've found for the binding, either use the existing
		// internal user we have cached, or create a new one, then append the
		// group (thus accumulating multiple groups for a specific user).
		for _, subject := range subjects {
			apiUser, found := c.k8s.CouchbaseUsers.Get(subject.Name)

			if subject.Spec.Name == "" {
				subject.Spec.Name = subject.Name
			}

			if found && !couchbaseutil.ShouldReconcile(apiUser.GetAnnotations()) {
				unReconcilableUsers[subject.Name] = true
				continue
			}

			user, ok := users[subject.Name]
			if !ok {
				user, err = c.createAPIUserFromSubject(subject, atLeast80)
				if err != nil {
					return nil, nil, err
				}
			}

			user.Groups = append(user.Groups, group)

			users[subject.Spec.Name] = user
		}
	}

	// if CNG is added and users are managed we need to make sure we don't delete the cng-admin user
	if c.cluster.Spec.Networking.CloudNativeGateway != nil {
		userName := fmt.Sprintf("%s@%s", k8sutil.CngAdminUserNamePrefix, c.cluster.Name)
		cngUser := couchbaseutil.User{
			ID:     userName,
			Name:   userName,
			Domain: couchbaseutil.InternalAuthDomain,
			Groups: []string{},
		}

		secretName := fmt.Sprintf("%s-%s", k8sutil.CngAdminUserSecretPrefix, c.cluster.Name)
		password, err := c.getRBACAuthPassword(secretName)

		if err != nil {
			return nil, nil, err
		}

		cngUser.Password = password
		cngUser.Roles = append(cngUser.Roles, couchbaseutil.UserRole{
			Role: string(couchbasev2.RoleFullAdmin),
		})
		users[cngUser.Name] = cngUser
	}

	return users, unReconcilableUsers, nil
}

// reconcileUsers creates, edits, removes server users according to requested configuration.
// The groups parameters is a list of groups that are known to exist at this moment in
// time in Couchbase server, this is used to only add users to an existing group.
func (c *Cluster) reconcileUsers(groups []string) ([]string, error) {
	requestedUsers, unreconcilableUsers, err := c.generateUsers(groups)
	if err != nil {
		return nil, err
	}

	// reconcile with existing users
	existingUsers := &couchbaseutil.UserList{}
	if err := couchbaseutil.ListUsers(existingUsers).On(c.api, c.readyMembers()); err != nil {
		return nil, err
	}

	shouldUpdateUser := func(r couchbaseutil.User, e couchbaseutil.User) bool {
		requestedGroups := r.Groups
		sort.Strings(requestedGroups)

		existingGroups := e.Groups
		sort.Strings(existingGroups)

		return !reflect.DeepEqual(requestedGroups, existingGroups) || !reflect.DeepEqual(r.Name, e.Name) || !reflect.DeepEqual(r.Locked, e.Locked)
	}

	existingUserNames := []string{}

	for _, user := range *existingUsers {
		e := user

		if r, ok := requestedUsers[e.ID]; ok {
			// requested user exists
			// update user if group bindings change
			if shouldUpdateUser(r, e) {
				// On regular reconciliation, we should never change the temporary password when updating a user. This is handled in reconcileTemporaryPasswords.
				r.TemporaryPassword = e.TemporaryPassword

				// Once a user has been created, we will never change the password to avoid overriding any changes made to the password by the user.
				r.Password = ""

				if err := couchbaseutil.CreateUser(&r).On(c.api, c.readyMembers()); err != nil {
					return existingUserNames, err
				}

				c.raiseEvent(k8sutil.UserEditEvent(e.ID, c.cluster))
				log.Info("edit CouchbaseUser", "cluster", c.cluster.NamespacedName(), "name", e.ID)
			}

			existingUserNames = append(existingUserNames, e.ID)
		} else {
			// If this is a user that is not reconcilable, we should not delete it
			if _, ok := unreconcilableUsers[e.ID]; ok {
				continue
			}

			// delete unrequested user
			if err := couchbaseutil.DeleteUser(&e).On(c.api, c.readyMembers()); err != nil {
				return existingUserNames, err
			}

			c.raiseEvent(k8sutil.UserDeleteEvent(e.ID, c.cluster))
			log.Info("delete CouchbaseUser", "cluster", c.cluster.NamespacedName(), "name", e.ID)
		}
	}

	// create requested groups that do not exist
	for i := range requestedUsers {
		r := requestedUsers[i]

		if _, found := couchbasev2.HasItem(r.ID, existingUserNames); !found {
			if err := couchbaseutil.CreateUser(&r).On(c.api, c.readyMembers()); err != nil {
				return existingUserNames, err
			}

			c.raiseEvent(k8sutil.UserCreateEvent(r.ID, c.cluster))
			log.Info("create CouchbaseUser", "cluster", c.cluster.NamespacedName(), "name", r.ID)
		}
	}

	return existingUserNames, nil
}

// reconcileTemporaryPasswords reconciles the temporary password fields for users. This is only applicable for 8.0.0+ clusters.
// By setting temporary password to true, a user will be prompted to change their password on next login.
func (c *Cluster) reconcileTemporaryPasswords(policyChange bool) error {
	atLeast80, err := c.IsAtLeastVersion("8.0.0")
	if err != nil || !atLeast80 {
		return err
	}

	users := &couchbaseutil.UserList{}
	if err := couchbaseutil.ListUsers(users).On(c.api, c.readyMembers()); err != nil {
		return err
	}

	for _, user := range *users {
		userSpec, ok := c.k8s.CouchbaseUsers.Get(user.ID)

		// If we can't find an associated spec for the user, or the user already has a temporary password, we can't reconcile the password.
		if !ok || user.HasTempPassword() {
			continue
		}

		resetPassword, err := c.userRequiresPasswordReset(policyChange, userSpec, user)
		if err != nil {
			return err
		}

		if resetPassword {
			user.TemporaryPassword = &resetPassword

			if err := couchbaseutil.CreateUser(&user).On(c.api, c.readyMembers()); err != nil {
				return err
			}

			c.raiseEvent(k8sutil.UserEditEvent(user.ID, c.cluster))
			log.Info("edit CouchbaseUser password is temporary", "cluster", c.cluster.NamespacedName(), "name", user.ID)
		}
	}

	return nil
}

func (c *Cluster) userRequiresPasswordReset(policyChange bool, userSpec *couchbasev2.CouchbaseUser, user couchbaseutil.User) (bool, error) {
	if policyChange {
		reset := c.shouldResetUserPasswordOnPolicyChange(userSpec)
		return reset, nil
	}

	lastChangedDate, err := user.GetPasswordChangeDate()
	if err != nil {
		return false, err
	}

	now := time.Now()

	return passwordExpired(lastChangedDate, now, userSpec) || passwordResetDurationElapsed(lastChangedDate, now, userSpec), nil
}

func passwordExpired(lastChangedDate time.Time, now time.Time, userSpec *couchbasev2.CouchbaseUser) bool {
	pwOptions := userSpec.Spec.Password
	if pwOptions == nil || pwOptions.ExpiresAt == nil {
		return false
	}

	return lastChangedDate.Before(pwOptions.ExpiresAt.Time) && now.After(pwOptions.ExpiresAt.Time)
}

func passwordResetDurationElapsed(lastChangedDate time.Time, now time.Time, userSpec *couchbasev2.CouchbaseUser) bool {
	pwOptions := userSpec.Spec.Password
	if pwOptions == nil || pwOptions.ExpiresAfter == nil {
		return false
	}

	return now.After(lastChangedDate.Add(pwOptions.ExpiresAfter.Duration))
}

func (c *Cluster) shouldResetUserPasswordOnPolicyChange(user *couchbasev2.CouchbaseUser) bool {
	passwordPolicy := c.cluster.Spec.Security.PasswordPolicy
	if passwordPolicy == nil || passwordPolicy.RequirePasswordResetOnPolicyChange == nil || !*passwordPolicy.RequirePasswordResetOnPolicyChange {
		return false
	}

	// Check if user is in the exemption list
	for _, exemptUser := range passwordPolicy.PasswordResetOnPolicyChangeExemptUsers {
		if exemptUser != nil && *exemptUser == user.Name {
			return false
		}
	}

	return true
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
	ldap := c.cluster.Spec.Security.LDAP
	if ldap == nil {
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

	if atleast76, err := c.IsAtLeastVersion("7.6.0"); err == nil && atleast76 {
		specLDAPSettings.MiddleboxCompMode = &ldap.MiddleboxCompMode
	}

	// Get current ldap cluster spec
	apiLDAPSettings := &couchbaseutil.LDAPSettings{}
	if err := couchbaseutil.GetLDAPSettings(apiLDAPSettings).On(c.api, c.readyMembers()); err != nil {
		return err
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
