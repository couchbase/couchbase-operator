package cluster

import (
	"reflect"
	"sort"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/annotations"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"

	"k8s.io/apimachinery/pkg/labels"
)

type SupportedFeature int

const (
	SupportedDurability SupportedFeature = iota
	SupportedBackendCouchstore
	SupportedBackendMagma
	SupportedHistoryRetention
)

type SupportedFeatureMap map[SupportedFeature]bool

// gatherCouchbaseBuckets gathers all K8s CB buckets and marshalls them into canonical form.
func gatherCouchbaseBuckets(supportedFeatures SupportedFeatureMap, selector labels.Selector, k8sBuckets []*couchbasev2.CouchbaseBucket, outputBuckets []couchbaseutil.Bucket) []couchbaseutil.Bucket {
	durablitySupported := supportedFeatures[SupportedDurability]
	storageBackendSupported := supportedFeatures[SupportedBackendCouchstore]
	magmaStorageBackendSupported := supportedFeatures[SupportedBackendMagma]
	SupportedHistoryRetention := supportedFeatures[SupportedHistoryRetention]

	for _, bucket := range k8sBuckets {
		err := annotations.Populate(&bucket.Spec, bucket.Annotations)
		if err != nil {
			// we failed but its not worth stopping. log the error and continue
			log.Error(err, "failed to populate bucket with annotation")
		}

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

		applyBucketBackend(&b, bucket, storageBackendSupported, magmaStorageBackendSupported)

		// CDC is only supported on Magma
		if b.BucketStorageBackend == couchbaseutil.CouchbaseStorageBackendMagma && SupportedHistoryRetention && bucket.Spec.HistoryRetentionSettings != nil {
			b.HistoryRetentionCollectionDefault = bucket.Spec.HistoryRetentionSettings.CollectionDefault
			b.HistoryRetentionBytes = bucket.Spec.HistoryRetentionSettings.Bytes
			b.HistoryRetentionSeconds = bucket.Spec.HistoryRetentionSettings.Seconds
		}

		outputBuckets = append(outputBuckets, b)
	}

	return outputBuckets
}

func applyBucketBackend(b *couchbaseutil.Bucket, bucket *couchbasev2.CouchbaseBucket, storageBackendCouchstore, storageBackendMagma bool) {
	// cb server version below 7.0.0.
	if !storageBackendCouchstore && bucket.Spec.StorageBackend != "" {
		// warning log for user why spec.StorageBackend is ignored.
		log.Info("[WARN] spec.storageBackend cannot be present for server version below 7.0.0", "CB image version", "lesser than 7.0.0")
	}

	// cb server version greater or equal to 7.0.0 but less than 7.1.0.
	// default is "couchstore"
	if storageBackendCouchstore && !storageBackendMagma && bucket.Spec.StorageBackend == "" {
		log.Info("[WARN] spec.storageBackend cannot be empty for server version below 7.1.0 - default to couchstore")

		b.BucketStorageBackend = couchbaseutil.CouchbaseStorageBackendCouchstore
	}

	// cb server version greater or equal to 7.0.0 but less than 7.1.0.
	// magma is not supported.
	// default is "couchstore"
	if storageBackendCouchstore && !storageBackendMagma && bucket.Spec.StorageBackend == couchbasev2.CouchbaseStorageBackend(couchbaseutil.CouchbaseStorageBackendMagma) {
		log.Info("[WARN] spec.storageBackend cannot be magma for server version below 7.1.0 - default to couchstore")

		b.BucketStorageBackend = couchbaseutil.CouchbaseStorageBackendCouchstore
	}

	// cb server version greater or equal to 7.1.0 and bucket.Spec.StorageBackend not specified.
	if storageBackendMagma && bucket.Spec.StorageBackend == "" {
		b.BucketStorageBackend = couchbaseutil.CouchbaseStorageBackendCouchstore
	}

	// cb server version greater or equal to 7.1.0 and bucket.Spec.StorageBackend specified.
	if storageBackendMagma && bucket.Spec.StorageBackend != "" {
		b.BucketStorageBackend = couchbaseutil.CouchbaseStorageBackend(bucket.Spec.StorageBackend)
	}
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

	supportedFeatures := make(map[SupportedFeature]bool)

	durablitySupported, err := c.IsAtLeastVersion("6.6.0")
	if err != nil {
		return nil, err
	}

	supportedFeatures[SupportedDurability] = durablitySupported

	// // storageBackend is only allowed above CB version 7.0.0.
	storageBackendSupported, err := c.IsAtLeastVersion("7.0.0")
	if err != nil {
		return nil, err
	}

	supportedFeatures[SupportedBackendCouchstore] = storageBackendSupported
	// // magma storageBackend is only allowed above CB version 7.1.0.
	magmaStorageBackendSupported, err := c.IsAtLeastVersion("7.1.0")
	if err != nil {
		return nil, err
	}

	supportedFeatures[SupportedBackendMagma] = magmaStorageBackendSupported

	historyRetentionSupported, err := c.IsAtLeastVersion("7.2.0")
	if err != nil {
		return nil, err
	}

	supportedFeatures[SupportedHistoryRetention] = historyRetentionSupported

	allBuckets := []couchbaseutil.Bucket{}

	allBuckets = gatherCouchbaseBuckets(supportedFeatures, selector, c.k8s.CouchbaseBuckets.List(), allBuckets)
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
				// Since, BucketStorageBackend is non-editable, once created.
				// This avoids running any update reconcile loop,
				// if BucketStorageBackend seems to be the only one different.
				r.BucketStorageBackend = a.BucketStorageBackend
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
