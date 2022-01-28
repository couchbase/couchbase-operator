package e2eutil

import (
	"context"
	"fmt"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Scope is used to model a scope and provide a flexible and terse
// builder pattern for all your scope needs.
type Scope struct {
	name               string
	labels             map[string]string
	collections        []metav1.Object
	labelSelector      map[string]string
	isDefault          bool
	managedCollections bool
}

// NewScope returns a new scope with all required parameters.
func NewScope(name string) *Scope {
	return &Scope{
		name: name,
	}
}

// IsDefault sets this scope to be the _default one.
func (s *Scope) IsDefault() *Scope {
	s.isDefault = true

	return s
}

// WithLabels allows a scope to be polulated with labels for selection.
func (s *Scope) WithLabels(labels map[string]string) *Scope {
	s.labels = labels

	return s
}

func (s *Scope) WithManagedCollections() *Scope {
	s.managedCollections = true

	return s
}

// WithCollections tells the scope to manage collections and add the provided
// collections and collection groups with an explciit resource reference.
func (s *Scope) WithCollections(collections ...metav1.Object) *Scope {
	s.collections = collections
	s.WithManagedCollections()

	return s
}

// WithLabelSelector tells the scope to manage collections and select collections
// and collection groups with an implicit resource selector.
func (s *Scope) WithLabelSelector(labelSelector map[string]string) *Scope {
	s.labelSelector = labelSelector
	s.WithManagedCollections()

	return s
}

// Generate takes a scope and creates a concrete API type.
func (s *Scope) Generate() *couchbasev2.CouchbaseScope {
	r := &couchbasev2.CouchbaseScope{
		ObjectMeta: metav1.ObjectMeta{
			Name: s.name,
		},
	}

	if s.isDefault {
		r.Spec.DefaultScope = true
	}

	if s.labels != nil {
		r.Labels = s.labels
	}

	if s.managedCollections {
		r.Spec.Collections = &couchbasev2.CollectionSelector{
			Managed: true,
		}
	}

	if s.collections != nil {
		for _, collection := range s.collections {
			switch t := collection.(type) {
			case *couchbasev2.CouchbaseCollection:
				r.Spec.Collections.Resources = append(r.Spec.Collections.Resources, couchbasev2.CollectionLocalObjectReference{
					Kind: couchbasev2.CollectionCRDResourceKind,
					Name: couchbasev2.ScopeOrCollectionName(t.Name),
				})
			case *couchbasev2.CouchbaseCollectionGroup:
				r.Spec.Collections.Resources = append(r.Spec.Collections.Resources, couchbasev2.CollectionLocalObjectReference{
					Kind: couchbasev2.CollectionGroupCRDResourceKind,
					Name: couchbasev2.ScopeOrCollectionName(t.Name),
				})
			}
		}
	}

	if s.labelSelector != nil {
		r.Spec.Collections.Selector = &metav1.LabelSelector{
			MatchLabels: s.labelSelector,
		}
	}

	return r
}

// MustCreate generates a scope and creates it in Kubernetes.
func (s *Scope) MustCreate(t *testing.T, kubernetes *types.Cluster) *couchbasev2.CouchbaseScope {
	return MustCreateScope(t, kubernetes, s.Generate())
}

// ScopeGroup is used to model a scope group and provide a flexible and terse
// builder pattern for all your scope needs.
type ScopeGroup struct {
	name          string
	names         []string
	labels        map[string]string
	collections   []metav1.Object
	labelSelector map[string]string
}

// NewScopeGroup returns a new scope group with all required parameters.
// The name parameter is the resource name, and the names are the names of
// the scopes defined by this group.
func NewScopeGroup(name string, names ...string) *ScopeGroup {
	return &ScopeGroup{
		name:  name,
		names: names,
	}
}

// NewScopeGroupN returns a new scope group with N generated names.
func NewScopeGroupN(name string, generateName string, n int) *ScopeGroup {
	s := &ScopeGroup{
		name: name,
	}

	for i := 0; i < n; i++ {
		s.names = append(s.names, fmt.Sprintf("%s%d", generateName, i))
	}

	return s
}

// WithLabels allows a scope to be polulated with labels for selection.
func (s *ScopeGroup) WithLabels(labels map[string]string) *ScopeGroup {
	s.labels = labels

	return s
}

// WithCollections tells the scope to manage collections and add the provided
// collections and collection groups with an explciit resource reference.
func (s *ScopeGroup) WithCollections(collections ...metav1.Object) *ScopeGroup {
	s.collections = collections

	return s
}

// WithLabelSelector tells the scope to manage collections and select collections
// and collection groups with an implicit resource selector.
func (s *ScopeGroup) WithLabelSelector(labelSelector map[string]string) *ScopeGroup {
	s.labelSelector = labelSelector

	return s
}

// Generate takes a scope group and creates a concrete API type.
func (s *ScopeGroup) Generate() *couchbasev2.CouchbaseScopeGroup {
	var names []couchbasev2.ScopeOrCollectionName

	for _, name := range s.names {
		names = append(names, couchbasev2.ScopeOrCollectionName(name))
	}

	r := &couchbasev2.CouchbaseScopeGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name: s.name,
		},
		Spec: couchbasev2.CouchbaseScopeGroupSpec{
			Names: names,
		},
	}

	if s.labels != nil {
		r.Labels = s.labels
	}

	if s.collections != nil || s.labelSelector != nil {
		r.Spec.Collections = &couchbasev2.CollectionSelector{
			Managed: true,
		}
	}

	if s.collections != nil {
		for _, collection := range s.collections {
			switch t := collection.(type) {
			case *couchbasev2.CouchbaseCollection:
				r.Spec.Collections.Resources = append(r.Spec.Collections.Resources, couchbasev2.CollectionLocalObjectReference{
					Kind: couchbasev2.CollectionCRDResourceKind,
					Name: couchbasev2.ScopeOrCollectionName(t.Name),
				})
			case *couchbasev2.CouchbaseCollectionGroup:
				r.Spec.Collections.Resources = append(r.Spec.Collections.Resources, couchbasev2.CollectionLocalObjectReference{
					Kind: couchbasev2.CollectionGroupCRDResourceKind,
					Name: couchbasev2.ScopeOrCollectionName(t.Name),
				})
			}
		}
	}

	if s.labelSelector != nil {
		r.Spec.Collections.Selector = &metav1.LabelSelector{
			MatchLabels: s.labelSelector,
		}
	}

	return r
}

// MustCreate generates a scope group and creates it in Kubernetes.
func (s *ScopeGroup) MustCreate(t *testing.T, kubernetes *types.Cluster) *couchbasev2.CouchbaseScopeGroup {
	return MustCreateScopeGroup(t, kubernetes, s.Generate())
}

// Collection is used to model a single collection and provide a terse builder pattern.
type Collection struct {
	name   string
	labels map[string]string
}

// NewCollection returns a new collection with all required parameters.
func NewCollection(name string) *Collection {
	return &Collection{
		name: name,
	}
}

// WithLabels allows a collection to be polulated with labels for selection.
func (c *Collection) WithLabels(labels map[string]string) *Collection {
	c.labels = labels

	return c
}

// Generate takes a collection and creates a concrete API type.
func (c *Collection) Generate() *couchbasev2.CouchbaseCollection {
	r := &couchbasev2.CouchbaseCollection{
		ObjectMeta: metav1.ObjectMeta{
			Name: c.name,
		},
	}

	if c.labels != nil {
		r.Labels = c.labels
	}

	return r
}

// MustCreate generates a collection and creates it in Kubernetes.
func (c *Collection) MustCreate(t *testing.T, kubernetes *types.Cluster) *couchbasev2.CouchbaseCollection {
	return MustCreateCollection(t, kubernetes, c.Generate())
}

// CollectionGroup is used to model a multiple collections and provide a terse builder pattern.
type CollectionGroup struct {
	name   string
	names  []string
	labels map[string]string
}

// NewCollectionGroup returns a new collection group with all required parameters.
// The name parameter is the resource name, and the names are the names of
// the collections defined by this group.
func NewCollectionGroup(name string, names ...string) *CollectionGroup {
	return &CollectionGroup{
		name:  name,
		names: names,
	}
}

// NewiCollectionGroupN returns a new collection group with N generated names.
func NewCollectionGroupN(name string, generateName string, n int) *CollectionGroup {
	c := &CollectionGroup{
		name: name,
	}

	for i := 0; i < n; i++ {
		c.names = append(c.names, fmt.Sprintf("%s%d", generateName, i))
	}

	return c
}

// WithLabels allows a collection to be polulated with labels for selection.
func (c *CollectionGroup) WithLabels(labels map[string]string) *CollectionGroup {
	c.labels = labels

	return c
}

// Generate takes a collection group and creates a concrete API type.
func (c *CollectionGroup) Generate() *couchbasev2.CouchbaseCollectionGroup {
	var names []couchbasev2.ScopeOrCollectionName

	for _, name := range c.names {
		names = append(names, couchbasev2.ScopeOrCollectionName(name))
	}

	r := &couchbasev2.CouchbaseCollectionGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name: c.name,
		},
		Spec: couchbasev2.CouchbaseCollectionGroupSpec{
			Names: names,
		},
	}

	if c.labels != nil {
		r.Labels = c.labels
	}

	return r
}

// MustCreate generates a collection group and creates it in Kubernetes.
func (c *CollectionGroup) MustCreate(t *testing.T, kubernetes *types.Cluster) *couchbasev2.CouchbaseCollectionGroup {
	return MustCreateCollectionGroup(t, kubernetes, c.Generate())
}

// ExpectedScopesAndCollections provides a wrapper around creating an expected scopes
// and collections topology, and makes the code a lot more terse.
type ExpectedScopesAndCollections struct {
	currentScope string
	expected     map[string]map[string]interface{}
}

// NewExpectedScopesAndCollections creates a new expected scopes and collections topology.
func NewExpectedScopesAndCollections() *ExpectedScopesAndCollections {
	return &ExpectedScopesAndCollections{
		expected: map[string]map[string]interface{}{},
	}
}

// WithDefaultScope adds a default scope to the topology.
func (e *ExpectedScopesAndCollections) WithDefaultScope() *ExpectedScopesAndCollections {
	return e.WithScope(couchbasev2.DefaultScopeOrCollection)
}

// WithDefaultScopeAndCollection adds a default scope and collection to the topology.
func (e *ExpectedScopesAndCollections) WithDefaultScopeAndCollection() *ExpectedScopesAndCollections {
	return e.WithScope(couchbasev2.DefaultScopeOrCollection).WithCollections(couchbasev2.DefaultScopeOrCollection)
}

// WithScope adds the named scope to the topology.  It also implicitly sets the current scope
// internally so this call can be chained with WithCollections.
func (e *ExpectedScopesAndCollections) WithScope(name string) *ExpectedScopesAndCollections {
	e.currentScope = name

	if _, ok := e.expected[name]; !ok {
		e.expected[name] = map[string]interface{}{}
	}

	return e
}

// WithScopes allows bulk creation of empty scopes in the topology.
func (e *ExpectedScopesAndCollections) WithScopes(names ...interface{}) *ExpectedScopesAndCollections {
	for _, name := range names {
		switch t := name.(type) {
		case string:
			e.WithScope(t)
		case []string:
			for _, n := range t {
				e.WithScope(n)
			}
		}
	}

	return e
}

// WithCollection adds the named collection to the topology in the scope set by the last
// call to either WithScope or WithScopes.
func (e *ExpectedScopesAndCollections) WithCollection(name string) *ExpectedScopesAndCollections {
	e.expected[e.currentScope][name] = nil

	return e
}

// WithCollections adds the named collections to the topology in the scope set by the last
// call to either WithScope or WithScopes.
func (e *ExpectedScopesAndCollections) WithCollections(names ...interface{}) *ExpectedScopesAndCollections {
	for _, name := range names {
		switch t := name.(type) {
		case string:
			e.WithCollection(t)
		case []string:
			for _, n := range t {
				e.WithCollection(n)
			}
		}
	}

	return e
}

// LinkBucketToScopesExplicit associates any scope type with any supported bucket type.
// It will enable scopes & collections if not already done so, then append the scopes
// to the explcit list of scope resources.
// TODO: a builder pattern for buckets would solve this mess better.
func LinkBucketToScopesExplicit(bucket metav1.Object, scopes ...metav1.Object) {
	for _, scope := range scopes {
		switch b := bucket.(type) {
		case *couchbasev2.CouchbaseBucket:
			if b.Spec.Scopes == nil {
				b.Spec.Scopes = &couchbasev2.ScopeSelector{}
			}

			b.Spec.Scopes.Managed = true

			switch s := scope.(type) {
			case *couchbasev2.CouchbaseScope:
				b.Spec.Scopes.Resources = append(b.Spec.Scopes.Resources, couchbasev2.ScopeLocalObjectReference{
					Kind: couchbasev2.ScopeCRDResourceKind,
					Name: couchbasev2.ScopeOrCollectionName(s.Name),
				})
			case *couchbasev2.CouchbaseScopeGroup:
				b.Spec.Scopes.Resources = append(b.Spec.Scopes.Resources, couchbasev2.ScopeLocalObjectReference{
					Kind: couchbasev2.ScopeGroupCRDResourceKind,
					Name: couchbasev2.ScopeOrCollectionName(s.Name),
				})
			}
		case *couchbasev2.CouchbaseEphemeralBucket:
			if b.Spec.Scopes == nil {
				b.Spec.Scopes = &couchbasev2.ScopeSelector{}
			}

			b.Spec.Scopes.Managed = true

			switch s := scope.(type) {
			case *couchbasev2.CouchbaseScope:
				b.Spec.Scopes.Resources = append(b.Spec.Scopes.Resources, couchbasev2.ScopeLocalObjectReference{
					Kind: couchbasev2.ScopeCRDResourceKind,
					Name: couchbasev2.ScopeOrCollectionName(s.Name),
				})
			case *couchbasev2.CouchbaseScopeGroup:
				b.Spec.Scopes.Resources = append(b.Spec.Scopes.Resources, couchbasev2.ScopeLocalObjectReference{
					Kind: couchbasev2.ScopeGroupCRDResourceKind,
					Name: couchbasev2.ScopeOrCollectionName(s.Name),
				})
			}
		}
	}
}

// LinkBucketToScopesImplicit associates any scope type to any supported bucket type though the
// use of label selectors.  It will enable scopes on the bucket if not already done so, add
// replace the label selector if not already set.
// TODO: a builder pattern for buckets would solve this mess better.
func LinkBucketToScopesImplicit(bucket metav1.Object, labels map[string]string) {
	switch b := bucket.(type) {
	case *couchbasev2.CouchbaseBucket:
		if b.Spec.Scopes == nil {
			b.Spec.Scopes = &couchbasev2.ScopeSelector{}
		}

		b.Spec.Scopes.Managed = true

		if b.Spec.Scopes.Selector == nil {
			b.Spec.Scopes.Selector = &metav1.LabelSelector{
				MatchLabels: labels,
			}
		}
	case *couchbasev2.CouchbaseEphemeralBucket:
		if b.Spec.Scopes == nil {
			b.Spec.Scopes = &couchbasev2.ScopeSelector{}
		}

		b.Spec.Scopes.Managed = true

		if b.Spec.Scopes.Selector == nil {
			b.Spec.Scopes.Selector = &metav1.LabelSelector{
				MatchLabels: labels,
			}
		}
	}
}

// MustCreateScope creates the specified scope and returns the updated version.
func MustCreateScope(t *testing.T, kubernetes *types.Cluster, scope *couchbasev2.CouchbaseScope) *couchbasev2.CouchbaseScope {
	newScope, err := kubernetes.CRClient.CouchbaseV2().CouchbaseScopes(kubernetes.Namespace).Create(context.Background(), scope, metav1.CreateOptions{})
	if err != nil {
		Die(t, err)
	}

	return newScope
}

// MustDeleteScope deletes the specified scope.
func MustDeleteScope(t *testing.T, kubernetes *types.Cluster, name string) {
	if err := kubernetes.CRClient.CouchbaseV2().CouchbaseScopes(kubernetes.Namespace).Delete(context.Background(), name, metav1.DeleteOptions{}); err != nil {
		Die(t, err)
	}
}

// MustCreateScopeGroup creates the specified scope group and returns the updated version.
func MustCreateScopeGroup(t *testing.T, kubernetes *types.Cluster, scopeGroup *couchbasev2.CouchbaseScopeGroup) *couchbasev2.CouchbaseScopeGroup {
	newScopeGroup, err := kubernetes.CRClient.CouchbaseV2().CouchbaseScopeGroups(kubernetes.Namespace).Create(context.Background(), scopeGroup, metav1.CreateOptions{})
	if err != nil {
		Die(t, err)
	}

	return newScopeGroup
}

// MustUpdateScopeGroup updates a scope group and returns the updated version.
func MustUpdateScopeGroup(t *testing.T, kubernetes *types.Cluster, scopeGroup *couchbasev2.CouchbaseScopeGroup) *couchbasev2.CouchbaseScopeGroup {
	newScopeGroup, err := kubernetes.CRClient.CouchbaseV2().CouchbaseScopeGroups(kubernetes.Namespace).Update(context.Background(), scopeGroup, metav1.UpdateOptions{})
	if err != nil {
		Die(t, err)
	}

	return newScopeGroup
}

// MustCreateCollection creates the specified collection and returns the updated version.
func MustCreateCollection(t *testing.T, kubernetes *types.Cluster, collection *couchbasev2.CouchbaseCollection) *couchbasev2.CouchbaseCollection {
	newCollection, err := kubernetes.CRClient.CouchbaseV2().CouchbaseCollections(kubernetes.Namespace).Create(context.Background(), collection, metav1.CreateOptions{})
	if err != nil {
		Die(t, err)
	}

	return newCollection
}

// MustDeleteCollection deletes the specified collection.
func MustDeleteCollection(t *testing.T, kubernetes *types.Cluster, name string) {
	if err := kubernetes.CRClient.CouchbaseV2().CouchbaseCollections(kubernetes.Namespace).Delete(context.Background(), name, metav1.DeleteOptions{}); err != nil {
		Die(t, err)
	}
}

// MustCreateCollection creates the specified collection group and returns the updated version.
func MustCreateCollectionGroup(t *testing.T, kubernetes *types.Cluster, collectionGroup *couchbasev2.CouchbaseCollectionGroup) *couchbasev2.CouchbaseCollectionGroup {
	newCollectionGroup, err := kubernetes.CRClient.CouchbaseV2().CouchbaseCollectionGroups(kubernetes.Namespace).Create(context.Background(), collectionGroup, metav1.CreateOptions{})
	if err != nil {
		Die(t, err)
	}

	return newCollectionGroup
}

// MustUpdateCollectionGroup updates a collection group and returns the updated version.
func MustUpdateCollectionGroup(t *testing.T, kubernetes *types.Cluster, collectionGroup *couchbasev2.CouchbaseCollectionGroup) *couchbasev2.CouchbaseCollectionGroup {
	newCollectionGroup, err := kubernetes.CRClient.CouchbaseV2().CouchbaseCollectionGroups(kubernetes.Namespace).Update(context.Background(), collectionGroup, metav1.UpdateOptions{})
	if err != nil {
		Die(t, err)
	}

	return newCollectionGroup
}

// MustWaitForScopesAndCollections accepts an expected scopes & collections state for
// the specified bucket and ensures that what is meant to be there is, and what isn't
// meant to be there isn't.
func MustWaitForScopesAndCollections(t *testing.T, kubernetes *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket metav1.Object, expected *ExpectedScopesAndCollections, timeout time.Duration) {
	callback := func() error {
		client, err := CreateAdminConsoleClient(kubernetes, cluster)
		if err != nil {
			return err
		}

		var scopes couchbaseutil.ScopeList

		if err := couchbaseutil.ListScopes(bucket.GetName(), &scopes).On(client.client, client.host); err != nil {
			return err
		}

		// Iterate over the actual scopes and find any that aren't expected to be
		// there.
		for _, scope := range scopes.Scopes {
			if _, ok := expected.expected[scope.Name]; !ok {
				return fmt.Errorf("scope `%v` not expected in bucket `%v`", scope.Name, bucket.GetName())
			}
		}

		// Iterate over the expected scopes and find any that are meant to be there,
		// but aren't.
		for scope, collections := range expected.expected {
			if !scopes.HasScope(scope) {
				return fmt.Errorf("expected scope `%v` not in bucket `%v`", scope, bucket.GetName())
			}

			actualScope := scopes.GetScope(scope)

			// Iterate over the actual collections in this scope and find any aren't
			// expected to be there.
			for _, collection := range actualScope.Collections {
				if _, ok := collections[collection.Name]; !ok {
					return fmt.Errorf("collection `%v` not expected in scope `%v` for bucket `%v`", collection.Name, scope, bucket.GetName())
				}
			}

			// Iterate over the expected collections in this scope and find any that are
			// meant to be there, but aren't.
			for collection := range collections {
				if !actualScope.HasCollection(collection) {
					return fmt.Errorf("expected collection `%v` not in scope `%v` for bucket `%v`", collection, scope, bucket.GetName())
				}
			}
		}

		return nil
	}

	if err := retryutil.RetryFor(timeout, callback); err != nil {
		Die(t, err)
	}
}

// MustAssertScopesAndCollectionsFor checks that the scopes and collections state is as expected
// for a period of time (e.g. the Operator is not doing something it shouldn't).
func MustAssertScopesAndCollectionsFor(t *testing.T, kubernetes *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket interface{}, expected *ExpectedScopesAndCollections, timeout time.Duration) {
	var bucketName string

	switch typ := bucket.(type) {
	case metav1.Object:
		bucketName = typ.GetName()
	case string:
		bucketName = typ
	default:
		Die(t, fmt.Errorf("unhandled type"))
	}

	callback := func() error {
		client, err := CreateAdminConsoleClient(kubernetes, cluster)
		if err != nil {
			return err
		}

		var scopes couchbaseutil.ScopeList

		if err := couchbaseutil.ListScopes(bucketName, &scopes).On(client.client, client.host); err != nil {
			return err
		}

		// Iterate over the actual scopes and find any that aren't expected to be
		// there.
		for _, scope := range scopes.Scopes {
			if _, ok := expected.expected[scope.Name]; !ok {
				return fmt.Errorf("scope `%v` not expected in bucket `%v`", scope.Name, bucketName)
			}
		}

		// Iterate over the expected scopes and find any that are meant to be there,
		// but aren't.
		for scope, collections := range expected.expected {
			if !scopes.HasScope(scope) {
				return fmt.Errorf("expected scope `%v` not in bucket `%v`", scope, bucketName)
			}

			actualScope := scopes.GetScope(scope)

			// Iterate over the actual collections in this scope and find any aren't
			// expected to be there.
			for _, collection := range actualScope.Collections {
				if _, ok := collections[collection.Name]; !ok {
					return fmt.Errorf("collection `%v` not expected in scope `%v` for bucket `%v`", collection.Name, scope, bucketName)
				}
			}

			// Iterate over the expected collections in this scope and find any that are
			// meant to be there, but aren't.
			for collection := range collections {
				if !actualScope.HasCollection(collection) {
					return fmt.Errorf("expected collection `%v` not in scope `%v` for bucket `%v`", collection, scope, bucketName)
				}
			}
		}

		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := retryutil.Assert(ctx, time.Second, callback); err != nil {
		Die(t, err)
	}
}

// MustCreateScopeManually allows manual creation of scopes.
func MustCreateScopeManually(t *testing.T, kubernetes *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket, scope string) {
	client, err := CreateAdminConsoleClient(kubernetes, cluster)
	if err != nil {
		Die(t, err)
	}

	if err := couchbaseutil.CreateScope(bucket, scope).On(client.client, client.host); err != nil {
		Die(t, err)
	}
}

// MustCreateCollectionManually allows manual creation of collections.
func MustCreateCollectionManually(t *testing.T, kubernetes *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket, scope, collection string) {
	client, err := CreateAdminConsoleClient(kubernetes, cluster)
	if err != nil {
		Die(t, err)
	}

	c := couchbaseutil.Collection{
		Name: collection,
	}

	if err := couchbaseutil.CreateCollection(bucket, scope, c).On(client.client, client.host); err != nil {
		Die(t, err)
	}
}

// MustCreateCollectionManually allows manual creation of collections.
func MustDeleteCollectionManually(t *testing.T, kubernetes *types.Cluster, cluster *couchbasev2.CouchbaseCluster, bucket, scope, collection string) {
	client, err := CreateAdminConsoleClient(kubernetes, cluster)
	if err != nil {
		Die(t, err)
	}

	if err := couchbaseutil.DeleteCollection(bucket, scope, collection).On(client.client, client.host); err != nil {
		Die(t, err)
	}
}
