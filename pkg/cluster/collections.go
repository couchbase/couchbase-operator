package cluster

import (
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// reconcileScopesAndCollections manages scopes and collections where managed
// for all buckets.  This is the entry point from the main reconcile loop.
func (c *Cluster) reconcileScopesAndCollections() error {
	// Handle legacy versions...
	tag, err := k8sutil.CouchbaseVersion(c.cluster.Spec.Image)
	if err != nil {
		return err
	}

	available, err := couchbaseutil.VersionAfter(tag, "7.0.0")
	if err != nil {
		return err
	}

	if !available {
		return nil
	}

	// Reconcile each bucket individually.
	buckets, err := c.listScopedBuckets()
	if err != nil {
		return err
	}

	for _, bucket := range buckets {
		if err := c.reconcileScopes(bucket); err != nil {
			return err
		}
	}

	return nil
}

// listScopedBuckets returns any buckets that are part of the cluster and are scopes
// and collections enabled.
// TODO: There is a bit of cross over between this and normal bucket handling, so
// this can be shared in future.
func (c *Cluster) listScopedBuckets() ([]couchbasev2.AbstractBucket, error) {
	// Only couchbase buckets support scopes and collections.
	// All buckets are generic from this point onward.
	// Annoyingly, golang wont let you splat and up-cast at the same
	// time, grrr.
	var buckets []couchbasev2.AbstractBucket

	for _, bucket := range c.k8s.CouchbaseBuckets.List() {
		buckets = append(buckets, bucket)
	}

	for _, bucket := range c.k8s.CouchbaseEphemeralBuckets.List() {
		buckets = append(buckets, bucket)
	}

	// Filter out any buckets that aren't selected for cluster inclusion or
	// are not scopes and collections enabled.
	selector, err := c.cluster.GetBucketLabelSelector()
	if err != nil {
		return nil, err
	}

	var filtered []couchbasev2.AbstractBucket

	for _, bucket := range buckets {
		if !selector.Matches(labels.Set(bucket.GetLabels())) {
			continue
		}

		spec := bucket.GetScopes()
		if spec == nil || !spec.Managed {
			continue
		}

		filtered = append(filtered, bucket)
	}

	return filtered, nil
}

// reconcileScopes reconciles scope and collection state for
// the provided bucket.
func (c *Cluster) reconcileScopes(bucket couchbasev2.AbstractBucket) error {
	// Get the current state of the system.
	current := &couchbaseutil.ScopeList{}

	if err := couchbaseutil.ListScopes(bucket.GetName(), current).On(c.api, c.readyMembers()); err != nil {
		return err
	}

	scopes, err := c.gatherScopes(bucket)
	if err != nil {
		return err
	}

	// Transform the list into a map for no other reason than O(1) lookups.
	requestedScopes := map[string]interface{}{}

	for _, scope := range scopes {
		requestedScopes[scope.CouchbaseName()] = nil
	}

	// We only raise one event per bucket if anything happens, flooding etcd
	// will only cause production downtime...
	var raiseEvent bool

	// Delete scopes using an exhaustive search.
	for _, scope := range current.Scopes {
		if _, ok := requestedScopes[scope.Name]; ok {
			continue
		}

		log.Info("Deleting scope", "cluster", c.namespacedName(), "bucket", bucket.GetName(), "scope", scope.Name)

		if err := couchbaseutil.DeleteScope(bucket.GetName(), scope.Name).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		raiseEvent = true
	}

	// Create scopes using an exhaustive search.
	for scope := range requestedScopes {
		if current.HasScope(scope) {
			continue
		}

		log.Info("Creating scope", "cluster", c.namespacedName(), "bucket", bucket.GetName(), "scope", scope)

		if err := couchbaseutil.CreateScope(bucket.GetName(), scope).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		raiseEvent = true
	}

	// Refresh the current scopes/collections to include new scopes as
	// the collections reconciliation will expect them to be populated.
	if err := couchbaseutil.ListScopes(bucket.GetName(), current).On(c.api, c.readyMembers()); err != nil {
		return err
	}

	// Reconcile collections.
	for _, scope := range scopes {
		if scope.Spec.Collections == nil || !scope.Spec.Collections.Managed {
			continue
		}

		if err := c.reconcileCollections(bucket, scope, current, &raiseEvent); err != nil {
			return err
		}
	}

	// Notify any watchers that something has happened.
	if raiseEvent {
		c.raiseEvent(k8sutil.ScopesAndCollectionsUpdated(c.cluster, bucket.GetName()))
	}

	return nil
}

// gatherScopes looks up all scopes that are referenced by a bucket and returns
// a list of scopes that should exist.  Only active resources that are referenced are
// considered.  This is the point where we also worry about the indestructable default
// collection.
func (c *Cluster) gatherScopes(bucket couchbasev2.AbstractBucket) ([]*couchbasev2.CouchbaseScope, error) {
	var scopes []*couchbasev2.CouchbaseScope

	var defaultScopeDefined bool

	c.gatherScopesExplicit(bucket, &scopes, &defaultScopeDefined)

	if err := c.gatherScopesImplicit(bucket, &scopes, &defaultScopeDefined); err != nil {
		return nil, err
	}

	// Couchbase mandates that a default scope exist, to make our life easier
	// algorithmically, we implicitly add one if not defined rather than
	// special case all over the place.
	if !defaultScopeDefined {
		scopes = append(scopes, &couchbasev2.CouchbaseScope{
			Spec: couchbasev2.CouchbaseScopeSpec{
				DefaultScope: true,
			},
		})
	}

	return scopes, nil
}

// gatherScopesExplicit collects all scopes that are explicitly referenced by
// the bucket, and appends then to the list.  Scope groups are expanded into individual
// scopes for data consistency.
func (c *Cluster) gatherScopesExplicit(bucket couchbasev2.AbstractBucket, scopes *[]*couchbasev2.CouchbaseScope, defaultScopeDefined *bool) {
	spec := bucket.GetScopes()

	if spec == nil {
		return
	}

	for _, resource := range spec.Resources {
		// CRD validation has made sure that only the following
		// are valid, and are always populated though defaulting.
		switch resource.Kind {
		case couchbasev2.ScopeCRDResourceKind:
			scope, ok := c.k8s.CouchbaseScopes.Get(resource.StrName())
			if !ok {
				log.V(1).Info("Unable to find scope resource", "kind", couchbasev2.ScopeCRDResourceKind, "name", resource.Name)
				break
			}

			if scope.Spec.DefaultScope {
				*defaultScopeDefined = true
			}

			*scopes = append(*scopes, scope)
		case couchbasev2.ScopeGroupCRDResourceKind:
			// Expand groups into individual scopes to make handing easier
			// e.g. you're only dealing with one type.
			scopeGroup, ok := c.k8s.CouchbaseScopeGroups.Get(resource.StrName())
			if !ok {
				log.V(1).Info("Unable to find scope resource", "kind", couchbasev2.ScopeGroupCRDResourceKind, "name", resource.Name)
				break
			}

			for _, name := range scopeGroup.Spec.Names {
				*scopes = append(*scopes, &couchbasev2.CouchbaseScope{
					Spec: couchbasev2.CouchbaseScopeSpec{
						Name: name,
						CouchbaseScopeSpecCommon: couchbasev2.CouchbaseScopeSpecCommon{
							Collections: scopeGroup.Spec.Collections,
						},
					},
				})
			}
		}
	}
}

// gatherScopesImplicit collects all scopes that are implicitly referenced by
// the bucket, and appends them to the list.  Scope groups are expanded into individual
// scopes for data consistency.
func (c *Cluster) gatherScopesImplicit(bucket couchbasev2.AbstractBucket, scopes *[]*couchbasev2.CouchbaseScope, defaultScopeDefined *bool) error {
	spec := bucket.GetScopes()

	if spec.Selector == nil {
		return nil
	}

	selector, err := metav1.LabelSelectorAsSelector(spec.Selector)
	if err != nil {
		return err
	}

	for _, scope := range c.k8s.CouchbaseScopes.List() {
		if !selector.Matches(labels.Set(scope.Labels)) {
			continue
		}

		if scope.Spec.DefaultScope {
			*defaultScopeDefined = true
		}

		*scopes = append(*scopes, scope)
	}

	for _, scopeGroup := range c.k8s.CouchbaseScopeGroups.List() {
		if !selector.Matches(labels.Set(scopeGroup.Labels)) {
			continue
		}

		for _, name := range scopeGroup.Spec.Names {
			*scopes = append(*scopes, &couchbasev2.CouchbaseScope{
				Spec: couchbasev2.CouchbaseScopeSpec{
					Name: name,
					CouchbaseScopeSpecCommon: couchbasev2.CouchbaseScopeSpecCommon{
						Collections: scopeGroup.Spec.Collections,
					},
				},
			})
		}
	}

	return nil
}

// reconcileCollections magaes collection state for a specific scope within
// a specific bucket.
func (c *Cluster) reconcileCollections(bucket couchbasev2.AbstractBucket, scope *couchbasev2.CouchbaseScope, current *couchbaseutil.ScopeList, raiseEvent *bool) error {
	collections, err := c.gatherCollections(scope)
	if err != nil {
		return err
	}

	// Transform the list into a map for no other reason than O(1) lookups.
	requestedCollections := map[string]interface{}{}

	for _, collection := range collections {
		requestedCollections[collection.CouchbaseName()] = nil
	}

	// Delete collections using an exhaustive search.
	for _, collection := range current.GetScope(scope.CouchbaseName()).Collections {
		if _, ok := requestedCollections[collection.Name]; ok {
			continue
		}

		log.Info("Deleting collection", "bucket", bucket.GetName(), "scope", scope.CouchbaseName(), "collection", collection.Name)

		if err := couchbaseutil.DeleteCollection(bucket.GetName(), scope.CouchbaseName(), collection.Name).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		*raiseEvent = true
	}

	// Create collections using an exhaustive search.
	for _, collection := range collections {
		if current.GetScope(scope.CouchbaseName()).HasCollection(collection.CouchbaseName()) {
			continue
		}

		log.Info("Creating collection", "bucket", bucket.GetName(), "scope", scope.CouchbaseName(), "collection", collection.CouchbaseName())

		apiCollection := couchbaseutil.Collection{
			Name: collection.CouchbaseName(),
		}

		if collection.Spec.MaxTTL != nil {
			apiCollection.MaxTTL = int(collection.Spec.MaxTTL.Duration.Seconds())
		}

		if err := couchbaseutil.CreateCollection(bucket.GetName(), scope.CouchbaseName(), apiCollection).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		*raiseEvent = true
	}

	return nil
}

// gatherCollections looks up all collections that are referenced by this scope.  We need to
// be aware that the default scope may have a default collection, and we should keep it alive
// if the user has indicated it should be.
func (c *Cluster) gatherCollections(scope *couchbasev2.CouchbaseScope) ([]*couchbasev2.CouchbaseCollection, error) {
	var collections []*couchbasev2.CouchbaseCollection

	c.gatherCollectionsExplicit(scope, &collections)

	if err := c.gatherCollectionsImplicit(scope, &collections); err != nil {
		return nil, err
	}

	// When Couchbase creates a bucket, it will also create a default scope, and a
	// default collection.  The collection can be deleted, so we have to explicitly
	// preserve it otherwise it will be irrecovably deleted.
	if scope.Spec.DefaultScope && scope.Spec.Collections != nil && scope.Spec.Collections.Managed && scope.Spec.Collections.PreserveDefaultCollection {
		collections = append(collections, &couchbasev2.CouchbaseCollection{
			Spec: couchbasev2.CouchbaseCollectionSpec{
				Name: couchbasev2.DefaultScopeOrCollection,
			},
		})
	}

	return collections, nil
}

// gatherCollectionsExplicit collects all collections that are explicitly referenced by
// the scope, and appends then to the list.  Collection groups are expanded into individual
// collections for data consistency.
func (c *Cluster) gatherCollectionsExplicit(scope *couchbasev2.CouchbaseScope, collections *[]*couchbasev2.CouchbaseCollection) {
	if scope.Spec.Collections == nil {
		return
	}

	for _, resource := range scope.Spec.Collections.Resources {
		// CRD validation has made sure that only the following
		// are valid, and are always populated though defaulting.
		switch resource.Kind {
		case couchbasev2.CollectionCRDResourceKind:
			collection, ok := c.k8s.CouchbaseCollections.Get(resource.StrName())
			if !ok {
				log.V(1).Info("Unable to find collection resource", "kind", couchbasev2.CollectionCRDResourceKind, "name", resource.Name)
				continue
			}

			*collections = append(*collections, collection)
		case couchbasev2.CollectionGroupCRDResourceKind:
			// Expand groups into individual collections to make handing easier
			// e.g. you're only dealing with one type.
			collectionGroup, ok := c.k8s.CouchbaseCollectionGroups.Get(resource.StrName())
			if !ok {
				log.V(1).Info("Unable to find collection resource", "kind", couchbasev2.CollectionGroupCRDResourceKind, "name", resource.Name)
				continue
			}

			for _, name := range collectionGroup.Spec.Names {
				*collections = append(*collections, &couchbasev2.CouchbaseCollection{
					Spec: couchbasev2.CouchbaseCollectionSpec{
						Name: name,
						CouchbaseCollectionSpecCommon: couchbasev2.CouchbaseCollectionSpecCommon{
							MaxTTL: collectionGroup.Spec.MaxTTL,
						},
					},
				})
			}
		}
	}
}

// gatherCollectionsImplicit collects all collections that are implicitly referenced by
// the scope, and appends them to the list.  Collection groups are expanded into individual
// collections for data consistency.
func (c *Cluster) gatherCollectionsImplicit(scope *couchbasev2.CouchbaseScope, collections *[]*couchbasev2.CouchbaseCollection) error {
	if scope.Spec.Collections == nil || scope.Spec.Collections.Selector == nil {
		return nil
	}

	selector, err := metav1.LabelSelectorAsSelector(scope.Spec.Collections.Selector)
	if err != nil {
		return err
	}

	for _, collection := range c.k8s.CouchbaseCollections.List() {
		if !selector.Matches(labels.Set(collection.Labels)) {
			continue
		}

		*collections = append(*collections, collection)
	}

	for _, collectionGroup := range c.k8s.CouchbaseCollectionGroups.List() {
		if !selector.Matches(labels.Set(collectionGroup.Labels)) {
			continue
		}

		for _, name := range collectionGroup.Spec.Names {
			*collections = append(*collections, &couchbasev2.CouchbaseCollection{
				Spec: couchbasev2.CouchbaseCollectionSpec{
					Name: name,
					CouchbaseCollectionSpecCommon: couchbasev2.CouchbaseCollectionSpecCommon{
						MaxTTL: collectionGroup.Spec.MaxTTL,
					},
				},
			})
		}
	}

	return nil
}
