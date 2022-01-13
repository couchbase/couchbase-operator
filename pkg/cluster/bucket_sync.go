package cluster

import (
	"context"
	"crypto/sha256"
	"fmt"
	"reflect"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/conversion"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// bucketSyncNamer is an abstract type to generate Kubernetes resource names in the
// context of bucket synchronization.  Conforms to the conversion.BucketNameGenerator
// interface.
type bucketSyncNamer struct {
	// c is a cluster reference so we have access to the cluster name to uniquely
	// name resources based on cluster (as we may have multiple in the same namespace)
	c *Cluster
}

// generateSuffix generates a unique fixed length, and DNS compatible, suffix.
func (n *bucketSyncNamer) generateSuffix(input string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(input)))
}

// generateBucketSuffix generates a unique fixed length, and DNS compatible, bucket suffix
// based on cluster and bucket name.
func (n *bucketSyncNamer) generateBucketSuffix(bucket *couchbaseutil.Bucket) string {
	input := fmt.Sprintf("%s-%s", n.c.cluster.Name, bucket.BucketName)

	return n.generateSuffix(input)
}

// GenerateBucketName generates a unique, but deterministic, bucket name.
func (n *bucketSyncNamer) GenerateBucketName(bucket *couchbaseutil.Bucket) string {
	return fmt.Sprintf("bucket-%s", n.generateBucketSuffix(bucket))
}

// GenerateEphemeralBucketName generates a unique, but deterministic, bucket name.
func (n *bucketSyncNamer) GenerateEphemeralBucketName(bucket *couchbaseutil.Bucket) string {
	return fmt.Sprintf("ephemeralbucket-%s", n.generateBucketSuffix(bucket))
}

// GenerateMemcachedBucketName generates a unique, but deterministic, bucket name.
func (n *bucketSyncNamer) GenerateMemcachedBucketName(bucket *couchbaseutil.Bucket) string {
	return fmt.Sprintf("memcachedbucket-%s", n.generateBucketSuffix(bucket))
}

// GenerateScopeName generates a unique, but deterministic, scope name.
func (n *bucketSyncNamer) GenerateScopeName(bucket *couchbaseutil.Bucket, scope *couchbaseutil.Scope) string {
	input := fmt.Sprintf("%s-%s-%s", n.c.cluster.Name, bucket.BucketName, scope.Name)

	return fmt.Sprintf("scope-%s", n.generateSuffix(input))
}

// GenerateCollectionName generates a unique, but deterministic, collection name.
func (n *bucketSyncNamer) GenerateCollectionName(bucket *couchbaseutil.Bucket, scope *couchbaseutil.Scope, collection *couchbaseutil.Collection) string {
	input := fmt.Sprintf("%s-%s-%s-%s", n.c.cluster.Name, bucket.BucketName, scope.Name, collection.Name)

	return fmt.Sprintf("collection-%s", n.generateSuffix(input))
}

// gatherDataTopologyResources does exactly that, it polls Couchbase -- as the source
// or truth -- for all buckets on the system.  It then recusively descends down the
// forest, visiting all scopes and collections.  The resources returned are a slice of
// Kubernetes custom resources that can be used to manage the unmanaged.  All resource
// names are unique and deterministic, all resources are linked together by name.
// Note: when we read out resources to diff later, they may have gone through the DAC
// and had defaults applied, and thus will not match, so be very, very verbose when
// creating resources here to avoid defaulting.
func (c *Cluster) gatherDataTopologyResources() ([]runtime.Object, error) {
	// First up, gather all buckets, scopes and collection direct from Couchbase and
	// turn then into resources.
	apiBuckets := couchbaseutil.BucketList{}
	if err := couchbaseutil.ListBuckets(&apiBuckets).On(c.api, c.readyMembers()); err != nil {
		return nil, err
	}

	var resources []runtime.Object

	namer := &bucketSyncNamer{
		c: c,
	}

	for _, apiBucket := range apiBuckets {
		// For each bucket, convert it into a Kubernetes API bucket.  These will have a
		// name generated for them that is unique to this cluster within this namespace,
		// and also deterministic, so we can hit this loop many times and ignore resources
		// that already exist.
		runtimeBucket, err := conversion.ConvertAbstractBucketToAPIBucket(&apiBucket, namer)
		if err != nil {
			return nil, err
		}

		resources = append(resources, runtimeBucket)

		bucket, ok := runtimeBucket.(couchbasev2.AbstractBucket)
		if !ok {
			return nil, fmt.Errorf("%w: unable to assert bucket type", errors.NewStackTracedError(errors.ErrInternalError))
		}

		// Wire up the bucket to the provided selector.
		bucket.SetLabels(c.cluster.Spec.Buckets.Selector.MatchLabels)

		apiScopes := couchbaseutil.ScopeList{}
		if err := couchbaseutil.ListScopes(apiBucket.BucketName, &apiScopes).On(c.api, c.readyMembers()); err != nil {
			return nil, err
		}

		for _, apiScope := range apiScopes.Scopes {
			// For each scope within the bucket, convert it to a Kubernetes API
			// scope.  Again, these will have names unique to this cluster and
			// bucket, and be deterministic.  Link the scope to the bucket to
			// indicate ownership.
			scope := conversion.ConvertCouchbaseScopeToAPIScope(&apiBucket, &apiScope, namer)

			scopeReference := couchbasev2.ScopeLocalObjectReference{
				Kind: couchbasev2.CouchbaseScopeKindScope,
				Name: couchbasev2.ScopeOrCollectionName(scope.Name),
			}

			bucket.AddScopeResource(scopeReference)

			resources = append(resources, scope)

			for _, apiCollection := range apiScope.Collections {
				if apiCollection.Name == constants.DefaultScopeOrCollectionName {
					// Default scopes and collections are special snowflakes, so
					// handle accordingly.
					scope.Spec.Collections.PreserveDefaultCollection = true
				} else {
					// Generate a collection for each within the scope, you know the
					// rules by now.
					collection := conversion.ConvertCouchbaseCollectionToAPICollection(&apiBucket, &apiScope, &apiCollection, namer)

					collectionReference := couchbasev2.CollectionLocalObjectReference{
						Kind: couchbasev2.CouchbaseCollectionKindCollection,
						Name: couchbasev2.ScopeOrCollectionName(collection.Name),
					}

					scope.Spec.Collections.Resources = append(scope.Spec.Collections.Resources, collectionReference)

					resources = append(resources, collection)
				}
			}
		}
	}

	return resources, nil
}

// unstructuredSpecsEqual checks to see if the resource specifications are exactly
// the same.  Note usually, DeepEqual goes wrong, as in Go a nil slice and an empty
// slice aren't the same, I'm _hoping_ the unstructured conversion does the right
// thing...
func (c *Cluster) unstructuredSpecsEqual(a, b *unstructured.Unstructured) bool {
	// Extract the unstructured specifications.
	aSpec, aOK, _ := unstructured.NestedFieldNoCopy(a.Object, "spec")
	bSpec, bOK, _ := unstructured.NestedFieldNoCopy(b.Object, "spec")

	// If one has a spec, and the other doesn't, unequal.
	if aOK != bOK {
		log.V(1).Info("Spec existence differs", "cluster", c.namespacedName(), "a", a.Object, "b", b.Object)

		return false
	}

	// Neither has a spec, equal.
	if !aOK {
		return true
	}

	ok := reflect.DeepEqual(aSpec, bSpec)
	if !ok {
		log.V(1).Info("Specs differ", "cluster", c.namespacedName(), "a", aSpec, "b", bSpec)

		return false
	}

	return true
}

// applyDataTopologyResources takes a slice of resources and then creates them in Kubernetes.
// This will run in a loop until the user tells it to stop, so it must be idempotent, thus
// if the resource already exists, and it's exactly as we expect, then that's fine.  If it's
// not then it's safe to assume the user hasn't tidied up, or has changed the data topology
// during synchronization.
func (c *Cluster) applyDataTopologyResources(resources []runtime.Object) error {
	for _, resource := range resources {
		o, err := runtime.DefaultUnstructuredConverter.ToUnstructured(resource)
		if err != nil {
			return errors.NewStackTracedError(err)
		}

		object := &unstructured.Unstructured{
			Object: o,
		}

		gvk := object.GroupVersionKind()

		// Checking if the resource exists is free, thus avoiding an API call, so
		// do that first and check for conflicts.
		if existingResource, ok := c.k8s.Get(gvk, object.GetName()); ok {
			log.V(1).Info("Synchronized resource already exists", "cluster", c.namespacedName(), "resource", object.GetName())

			eo, err := runtime.DefaultUnstructuredConverter.ToUnstructured(existingResource)
			if err != nil {
				return errors.NewStackTracedError(err)
			}

			existingObject := &unstructured.Unstructured{
				Object: eo,
			}

			if !c.unstructuredSpecsEqual(object, existingObject) {
				log.Info("Synchronized resource already exists, but differs", "cluster", c.namespacedName(), "resource", object.GetName())

				return fmt.Errorf("%w: resource reports as existing, but differs", errors.NewStackTracedError(errors.ErrInternalError))
			}

			continue
		}

		// Resource doesn't exist, so create it.
		mapping, err := c.k8s.RESTMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return errors.NewStackTracedError(err)
		}

		if _, err := c.k8s.DynamicClient.Resource(mapping.Resource).Namespace(c.cluster.Namespace).Create(context.Background(), object, metav1.CreateOptions{}); err != nil {
			return err
		}

		log.Info("Synchronized resource", "cluster", c.namespacedName(), "resource", object.GetName())
	}

	return nil
}

// synchronizeBuckets is the main meat of the synchronization process, it
// generates resources necessary to manage existing unmanaged data topology, then
// idemoptently applies them to the Kubernetes cluster.
func (c *Cluster) synchronizeBuckets() error {
	// Perform any necessary sanity checks to prevent nil pointer dereferences,
	// and subsequent crashes, the DAC, if in use, should stop these before hand.
	if c.cluster.Spec.Buckets.Selector == nil || c.cluster.Spec.Buckets.Selector.MatchLabels == nil {
		return fmt.Errorf("%w: bucket label selector not set", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	// Poll Couchbase and get all resources that should be there to maintain
	// the current unmanaged configuration.
	resources, err := c.gatherDataTopologyResources()
	if err != nil {
		return err
	}

	// Next up create the resources in Kubernetes.
	if err := c.applyDataTopologyResources(resources); err != nil {
		return err
	}

	return nil
}

// reconcileSynchronizeBuckets is a top level wrapper around synchronization, it handles UX.
func (c *Cluster) reconcileSynchronizeBuckets() error {
	// Always update the status, on error we want to report that enabling management
	// will most likely result in data loss, on success report that it can be switched
	// to fully managed.  If we are doing nothing, then we need to remove any condition.
	defer func() {
		if err := c.updateCRStatus(); err != nil {
			log.Info("Unable to update cluster status", "cluster", c.namespacedName(), "error", err)
		}
	}()

	// If buckets are managed, or we are not synchronizing, then do nothing
	// and remove any stale conditions.
	if c.cluster.Spec.Buckets.Managed || !c.cluster.Spec.Buckets.Synchronize {
		c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionSynchronized)

		return nil
	}

	if err := c.synchronizeBuckets(); err != nil {
		c.cluster.Status.SetSynchronizationFailedCondition()

		return err
	}

	c.cluster.Status.SetSynchronizedCondition()

	return nil
}
