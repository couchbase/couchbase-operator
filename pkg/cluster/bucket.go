package cluster

import (
	"reflect"
	"sort"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"

	"k8s.io/apimachinery/pkg/labels"
)

// gatherCouchbaseBuckets gathers all K8s CB buckets and marshalls them into canonical form.
func gatherCouchbaseBuckets(durablitySupported, storageBackendSupported bool, selector labels.Selector, k8sBuckets []*couchbasev2.CouchbaseBucket, outputBuckets []couchbaseutil.Bucket) []couchbaseutil.Bucket {
	for _, bucket := range k8sBuckets {
		if !selector.Matches(labels.Set(bucket.Labels)) {
			continue
		}

		name := bucket.Name

		if bucket.Spec.Name != "" {
			name = string(bucket.Spec.Name)
		}

		b := couchbaseutil.Bucket{
			BucketName:         name,
			BucketType:         constants.BucketTypeCouchbase,
			BucketMemoryQuota:  k8sutil.Megabytes(bucket.Spec.MemoryQuota),
			BucketReplicas:     bucket.Spec.Replicas,
			IoPriority:         couchbaseutil.IoPriorityType(bucket.Spec.IoPriority),
			EvictionPolicy:     string(bucket.Spec.EvictionPolicy),
			ConflictResolution: string(bucket.Spec.ConflictResolution),
			EnableFlush:        bucket.Spec.EnableFlush,
			EnableIndexReplica: bucket.Spec.EnableIndexReplica,
			CompressionMode:    couchbaseutil.CompressionMode(bucket.Spec.CompressionMode),
		}

		if durablitySupported {
			b.DurabilityMinLevel = couchbaseutil.Durability(bucket.GetMinimumDurability())
		}

		if bucket.Spec.MaxTTL != nil {
			b.MaxTTL = int(bucket.Spec.MaxTTL.Duration.Seconds())
		}

		if storageBackendSupported {
			// default is "couchstore" (validated).
			b.BucketStorageBackend = couchbaseutil.CouchbaseStorageBackend(bucket.Spec.StorageBackend)
		}

		outputBuckets = append(outputBuckets, b)
	}

	return outputBuckets
}

// gatherEphemeralBuckets gathers all K8s CB Ephemeral buckets and marshalls them into canonical form.
func gatherEphemeralBuckets(durablitySupported bool, selector labels.Selector, k8sEphemeralBuckets []*couchbasev2.CouchbaseEphemeralBucket, outputBuckets []couchbaseutil.Bucket) []couchbaseutil.Bucket {
	for _, bucket := range k8sEphemeralBuckets {
		if !selector.Matches(labels.Set(bucket.Labels)) {
			continue
		}

		name := bucket.Name

		if bucket.Spec.Name != "" {
			name = string(bucket.Spec.Name)
		}

		b := couchbaseutil.Bucket{
			BucketName:         name,
			BucketType:         constants.BucketTypeEphemeral,
			BucketMemoryQuota:  k8sutil.Megabytes(bucket.Spec.MemoryQuota),
			BucketReplicas:     bucket.Spec.Replicas,
			IoPriority:         couchbaseutil.IoPriorityType(bucket.Spec.IoPriority),
			EvictionPolicy:     string(bucket.Spec.EvictionPolicy),
			ConflictResolution: string(bucket.Spec.ConflictResolution),
			EnableFlush:        bucket.Spec.EnableFlush,
			CompressionMode:    couchbaseutil.CompressionMode(bucket.Spec.CompressionMode),
		}

		if durablitySupported {
			b.DurabilityMinLevel = couchbaseutil.Durability(bucket.GetMinimumDurability())
		}

		if bucket.Spec.MaxTTL != nil {
			b.MaxTTL = int(bucket.Spec.MaxTTL.Duration.Seconds())
		}

		outputBuckets = append(outputBuckets, b)
	}

	return outputBuckets
}

// gatherMemcachedBuckets gathers all K8s CB Memcached buckets and marshalls them into canonical form.
func gatherMemcachedBuckets(selector labels.Selector, k8sMemcachedBuckets []*couchbasev2.CouchbaseMemcachedBucket, outputBuckets []couchbaseutil.Bucket) []couchbaseutil.Bucket {
	for _, bucket := range k8sMemcachedBuckets {
		if !selector.Matches(labels.Set(bucket.Labels)) {
			continue
		}

		name := bucket.Name

		if bucket.Spec.Name != "" {
			name = string(bucket.Spec.Name)
		}

		b := couchbaseutil.Bucket{
			BucketName:        name,
			BucketType:        constants.BucketTypeMemcached,
			BucketMemoryQuota: k8sutil.Megabytes(bucket.Spec.MemoryQuota),
			EnableFlush:       bucket.Spec.EnableFlush,
		}

		outputBuckets = append(outputBuckets, b)
	}

	return outputBuckets
}

// gatherBuckets loads up bucket configurations from Kubernetes and marshalls them into canonical form.
func (c *Cluster) gatherBuckets() ([]couchbaseutil.Bucket, error) {
	selector, err := c.cluster.GetBucketLabelSelector()
	if err != nil {
		return nil, err
	}

	tag, err := k8sutil.CouchbaseVersion(c.cluster.Spec.CouchbaseImage())
	if err != nil {
		return nil, err
	}

	durablitySupported, err := couchbaseutil.VersionAfter(tag, "6.6.0")
	if err != nil {
		return nil, err
	}

	// storageBackend is only allowed above CB version 7.1.0.
	storageBackendSupported, err := couchbaseutil.VersionAfter(tag, "7.1.0")
	if err != nil {
		return nil, err
	}

	allBuckets := []couchbaseutil.Bucket{}

	allBuckets = gatherCouchbaseBuckets(durablitySupported, storageBackendSupported, selector, c.k8s.CouchbaseBuckets.List(), allBuckets)
	allBuckets = gatherEphemeralBuckets(durablitySupported, selector, c.k8s.CouchbaseEphemeralBuckets.List(), allBuckets)
	allBuckets = gatherMemcachedBuckets(selector, c.k8s.CouchbaseMemcachedBuckets.List(), allBuckets)

	return allBuckets, nil
}

// inspectBuckets compares Kubernetes buckets with Couchbase buckets and returns lists
// of buckets to create, update or remove and the requested set for status updates.
func (c *Cluster) inspectBuckets() ([]couchbaseutil.Bucket, []couchbaseutil.Bucket, []couchbaseutil.Bucket, []couchbaseutil.Bucket, error) {
	requested, err := c.gatherBuckets()
	if err != nil {
		return nil, nil, nil, nil, err
	}

	actual := couchbaseutil.BucketList{}
	if err := couchbaseutil.ListBuckets(&actual).On(c.api, c.readyMembers()); err != nil {
		return nil, nil, nil, nil, err
	}

	create := []couchbaseutil.Bucket{}
	update := []couchbaseutil.Bucket{}
	remove := []couchbaseutil.Bucket{}

	// Do an exhaustive search of requested buckets in the actual list, creating and
	// updating as necessary.
	for _, r := range requested {
		found := false

		for _, a := range actual {
			if r.BucketName == a.BucketName {
				if !reflect.DeepEqual(r, a) {
					update = append(update, r)
					c.logUpdate(a, r)
				}

				found = true

				break
			}
		}

		if !found {
			create = append(create, r)
		}
	}

	// Do an exhaustive search of actual buckets in the requested list, deleting
	// as necessary.
	for _, a := range actual {
		found := false

		for _, r := range requested {
			if a.BucketName == r.BucketName {
				found = true
				break
			}
		}

		if !found {
			remove = append(remove, a)
		}
	}

	return create, update, remove, requested, nil
}

// reconcile buckets by adding or removing
// buckets one at a time based on comparison
// of existing buckets to cluster spec.
func (c *Cluster) reconcileBuckets() error {
	if !c.cluster.Spec.Buckets.Managed {
		return nil
	}

	create, update, remove, requested, err := c.inspectBuckets()
	if err != nil {
		return err
	}

	for i := range create {
		bucket := &create[i]

		if err := couchbaseutil.CreateBucket(bucket).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		log.Info("Bucket created", "cluster", c.namespacedName(), "name", bucket.BucketName)
		c.raiseEvent(k8sutil.BucketCreateEvent(bucket.BucketName, c.cluster))
	}

	for i := range update {
		bucket := &update[i]

		if err := couchbaseutil.UpdateBucket(bucket).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		log.Info("Bucket updated", "cluster", c.namespacedName(), "name", bucket.BucketName)
		c.raiseEvent(k8sutil.BucketEditEvent(bucket.BucketName, c.cluster))
	}

	for _, bucket := range remove {
		if err := couchbaseutil.DeleteBucket(bucket.BucketName).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		log.Info("Bucket deleted", "cluster", c.namespacedName(), "name", bucket.BucketName)
		c.raiseEvent(k8sutil.BucketDeleteEvent(bucket.BucketName, c.cluster))
	}

	// To avoid API updates, we record the name of each bucket on the system (this will
	// be lexically sorted), and we add buckets to the status in a deterministic order.
	names := make([]string, len(requested))
	statuses := map[string]couchbasev2.BucketStatus{}

	for i, bucket := range requested {
		names[i] = bucket.BucketName

		statuses[bucket.BucketName] = couchbasev2.BucketStatus{
			BucketName:           bucket.BucketName,
			BucketType:           bucket.BucketType,
			BucketStorageBackend: string(bucket.BucketStorageBackend),
			BucketMemoryQuota:    bucket.BucketMemoryQuota,
			BucketReplicas:       bucket.BucketReplicas,
			IoPriority:           string(bucket.IoPriority),
			EvictionPolicy:       bucket.EvictionPolicy,
			ConflictResolution:   bucket.ConflictResolution,
			EnableFlush:          bucket.EnableFlush,
			EnableIndexReplica:   bucket.EnableIndexReplica,
			CompressionMode:      string(bucket.CompressionMode),
		}
	}

	sort.Strings(names)

	c.cluster.Status.Buckets = []couchbasev2.BucketStatus{}

	for _, name := range names {
		c.cluster.Status.Buckets = append(c.cluster.Status.Buckets, statuses[name])
	}

	return nil
}
