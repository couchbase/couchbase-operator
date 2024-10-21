package cluster

import (
	"fmt"
	"reflect"
	"sort"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/client"
	"github.com/couchbase/couchbase-operator/pkg/util/annotations"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/go-openapi/errors"

	"k8s.io/apimachinery/pkg/labels"
)

type SupportedFeature int

const (
	SupportedDurability SupportedFeature = iota
	SupportedBackendCouchstore
	SupportedBackendMagma
	SupportedHistoryRetention
	SupportedRank
)

type SupportedFeatureMap map[SupportedFeature]bool

// gatherCouchbaseBuckets gathers all K8s CB buckets and marshalls them into canonical form.
//
//nolint:gocognit
func gatherCouchbaseBuckets(supportedFeatures SupportedFeatureMap, selector labels.Selector, k8sBuckets []*couchbasev2.CouchbaseBucket, outputBuckets []couchbaseutil.Bucket, cluster *couchbasev2.CouchbaseCluster, client *client.Client) []couchbaseutil.Bucket {
	durablitySupported := supportedFeatures[SupportedDurability]
	storageBackendSupported := supportedFeatures[SupportedBackendCouchstore]
	magmaStorageBackendSupported := supportedFeatures[SupportedBackendMagma]
	supportedHistoryRetention := supportedFeatures[SupportedHistoryRetention]
	supportedRank := supportedFeatures[SupportedRank]

	for _, bucket := range k8sBuckets {
		if client != nil {
			bucketA, found := client.CouchbaseBuckets.Get(bucket.Name)
			if found && !couchbaseutil.ShouldReconcile(bucketA.Annotations) {
				continue
			}
		}

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
			SampleBucket:       bucket.Spec.SampleBucket,
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

		applyBucketStorageBackend(&b, bucket, storageBackendSupported, magmaStorageBackendSupported, cluster)

		// Defaults to true, when bucket is magma.
		// Hence, setting it to true to avoid false reconciliation updates.
		if b.BucketStorageBackend == couchbaseutil.CouchbaseStorageBackendMagma && supportedHistoryRetention {
			historyRetentionCollectionDefaultTrue := true
			b.HistoryRetentionCollectionDefault = &historyRetentionCollectionDefaultTrue
		}

		// Although, the API doesn't need us to pass default values
		// but, our reconciler comparison fails, when nil. So, setting default values.
		notNilOrDefault := func(val *uint64, defaultVal uint64) *uint64 {
			if val != nil {
				return val
			}

			return &defaultVal
		}

		if b.BucketStorageBackend == couchbaseutil.CouchbaseStorageBackendMagma {
			// MagmaSeqTreeDataBlockSize/MagmaKeyTreeDataBlockSize only supported on Magma
			b.MagmaSeqTreeDataBlockSize = notNilOrDefault(bucket.Spec.MagmaSeqTreeDataBlockSize, constants.MagmaSeqTreeDataDefaultBlockSize)
			b.MagmaKeyTreeDataBlockSize = notNilOrDefault(bucket.Spec.MagmaKeyTreeDataBlockSize, constants.MagmaKeyTreeDataDefaultBlockSize)

			// CDC is only supported on Magma
			if supportedHistoryRetention && bucket.Spec.HistoryRetentionSettings != nil {
				b.HistoryRetentionCollectionDefault = bucket.Spec.HistoryRetentionSettings.CollectionDefault
				b.HistoryRetentionBytes = bucket.Spec.HistoryRetentionSettings.Bytes
				b.HistoryRetentionSeconds = bucket.Spec.HistoryRetentionSettings.Seconds
			}
		}

		if supportedRank {
			b.Rank = &bucket.Spec.Rank
		}

		outputBuckets = append(outputBuckets, b)
	}

	return outputBuckets
}

func applyBucketStorageBackend(b *couchbaseutil.Bucket, bucket *couchbasev2.CouchbaseBucket, storageBackendCouchstore, storageBackendMagma bool, cluster *couchbasev2.CouchbaseCluster) {
	// cb server version below 7.0.0.
	if !storageBackendCouchstore && bucket.Spec.StorageBackend != "" {
		// warning log for user why spec.StorageBackend is ignored.
		log.Info("[WARN] spec.storageBackend cannot be present for server version below 7.0.0", "CB image version", "lesser than 7.0.0")
	}

	// cb server version greater or equal to 7.0.0 but less than 7.1.0.
	// default is "couchstore"
	if storageBackendCouchstore && !storageBackendMagma && bucket.Spec.StorageBackend == "" {
		log.Info("[WARN] spec.storageBackend cannot be empty for server version below 7.1.0 - default to couchstore")
	}

	// cb server version greater or equal to 7.0.0 but less than 7.1.0.
	// magma is not supported.
	// default is "couchstore"
	if storageBackendCouchstore && !storageBackendMagma && bucket.Spec.StorageBackend == couchbasev2.CouchbaseStorageBackend(couchbaseutil.CouchbaseStorageBackendMagma) {
		log.Info("[WARN] spec.storageBackend cannot be magma for server version below 7.1.0 - default to couchstore")
	}

	b.BucketStorageBackend = couchbaseutil.CouchbaseStorageBackend(bucket.GetStorageBackend(cluster))
}

// gatherEphemeralBuckets gathers all K8s CB Ephemeral buckets and marshalls them into canonical form.
func gatherEphemeralBuckets(supportedFeatures SupportedFeatureMap, selector labels.Selector, k8sEphemeralBuckets []*couchbasev2.CouchbaseEphemeralBucket, outputBuckets []couchbaseutil.Bucket, client *client.Client) []couchbaseutil.Bucket {
	durablitySupported := supportedFeatures[SupportedDurability]
	supportedRank := supportedFeatures[SupportedRank]

	for _, bucket := range k8sEphemeralBuckets {
		bucketA, found := client.CouchbaseEphemeralBuckets.Get(bucket.Name)
		if found && !couchbaseutil.ShouldReconcile(bucketA.Annotations) {
			continue
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
			SampleBucket:       bucket.Spec.SampleBucket,
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

		if supportedRank {
			b.Rank = &bucket.Spec.Rank
		}

		outputBuckets = append(outputBuckets, b)
	}

	return outputBuckets
}

// gatherMemcachedBuckets gathers all K8s CB Memcached buckets and marshalls them into canonical form.
func gatherMemcachedBuckets(selector labels.Selector, k8sMemcachedBuckets []*couchbasev2.CouchbaseMemcachedBucket, outputBuckets []couchbaseutil.Bucket, client *client.Client) []couchbaseutil.Bucket {
	for _, bucket := range k8sMemcachedBuckets {
		bucketA, found := client.CouchbaseMemcachedBuckets.Get(bucket.Name)
		if found && !couchbaseutil.ShouldReconcile(bucketA.Annotations) {
			continue
		}

		if !selector.Matches(labels.Set(bucket.Labels)) {
			continue
		}

		name := bucket.Name

		if bucket.Spec.Name != "" {
			name = string(bucket.Spec.Name)
		}

		b := couchbaseutil.Bucket{
			BucketName:        name,
			SampleBucket:      bucket.Spec.SampleBucket,
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

	rankSupported, err := c.IsAtLeastVersion("7.6.0")
	if err != nil {
		return nil, err
	}

	supportedFeatures[SupportedRank] = rankSupported

	allBuckets := []couchbaseutil.Bucket{}

	allBuckets = gatherCouchbaseBuckets(supportedFeatures, selector, c.k8s.CouchbaseBuckets.List(), allBuckets, c.cluster, c.k8s)
	allBuckets = gatherEphemeralBuckets(supportedFeatures, selector, c.k8s.CouchbaseEphemeralBuckets.List(), allBuckets, c.k8s)
	allBuckets = gatherMemcachedBuckets(selector, c.k8s.CouchbaseMemcachedBuckets.List(), allBuckets, c.k8s)

	return allBuckets, nil
}

func (c *Cluster) GetBucketsToUpdate() (map[couchbaseutil.Bucket]couchbaseutil.Bucket, error) {
	updateBuckets := make(map[couchbaseutil.Bucket]couchbaseutil.Bucket)

	requested, err := c.gatherBuckets()
	if err != nil {
		return nil, err
	}

	actual := couchbaseutil.BucketList{}
	if err := couchbaseutil.ListBuckets(&actual).On(c.api, c.readyMembers()); err != nil {
		return nil, err
	}

	for _, r := range requested {
		for _, a := range actual {
			if r.BucketName == a.BucketName {
				if !reflect.DeepEqual(r, a) {
					updateBuckets[a] = r
				}

				break
			}
		}
	}

	return updateBuckets, nil
}

func (c *Cluster) checkUnreconilableBucket(bucket couchbaseutil.Bucket) bool {
	var annotations = make(map[string]string)

	switch bucket.BucketType {
	case constants.BucketTypeCouchbase:
		apiBucket, ok := c.GetK8sClient().CouchbaseBuckets.Get(bucket.BucketName)
		if ok {
			annotations = apiBucket.Annotations
		}
	case constants.BucketTypeMemcached:
		apiBucket, ok := c.GetK8sClient().CouchbaseMemcachedBuckets.Get(bucket.BucketName)
		if ok {
			annotations = apiBucket.Annotations
		}
	case constants.BucketTypeEphemeral:
		apiBucket, ok := c.GetK8sClient().CouchbaseEphemeralBuckets.Get(bucket.BucketName)
		if ok {
			annotations = apiBucket.Annotations
		}
	}

	if !couchbaseutil.ShouldReconcile(annotations) {
		return true
	}

	return false
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
				if found = c.checkUnreconilableBucket(a); found {
					continue
				}

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
				if found = c.checkUnreconilableBucket(a); found {
					continue
				}

				matchBackendsIfBefore76(&r, &a, c.cluster)

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

// Since, BucketStorageBackend is non-editable, once created for CB version < 7.6.0.
// This avoids running any update reconcile loop,
// if BucketStorageBackend seems to be the only one different.
func matchBackendsIfBefore76(r, a *couchbaseutil.Bucket, cluster *couchbasev2.CouchbaseCluster) {
	if isAtleast76, err := cluster.IsAtLeastVersion("7.6.0"); err == nil && !isAtleast76 {
		r.BucketStorageBackend = a.BucketStorageBackend
		if r.BucketStorageBackend != a.BucketStorageBackend {
			r.BucketStorageBackend = a.BucketStorageBackend

			log.Info("[WARN] spec.storageBackend cannot be changed for server version below 7.6.0")
		}
	}
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

		if bucket.SampleBucket == true {
			if err := couchbaseutil.CreateSampleBucket(bucket.BucketName).On(c.api, c.readyMembers()); err != nil {
				return err
			}

			log.Info("Bucket created", "cluster", c.namespacedName(), "name", bucket.BucketName)
			c.raiseEvent(k8sutil.BucketCreateEvent(bucket.BucketName, c.cluster))

			continue
		}

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

func (c *Cluster) reconcileUnmanagedBucketsBackends() error {
	if c.cluster.Spec.Buckets.TargetUnmanagedBucketStorageBackend == nil || c.cluster.Spec.Buckets.Managed {
		return nil
	}

	if isAtleast76, err := c.IsAtLeastVersion("7.6.0"); err == nil && !isAtleast76 {
		return nil
	}

	targetBackend := *c.cluster.Spec.Buckets.TargetUnmanagedBucketStorageBackend

	buckets := couchbaseutil.BucketList{}
	if err := couchbaseutil.ListBuckets(&buckets).On(c.api, c.readyMembers()); err != nil {
		return err
	}

	var errs []error

	for _, bucket := range buckets {
		if string(bucket.BucketStorageBackend) == string(targetBackend) {
			continue
		}

		if ok, reason := c.canBucketBeMigrated(bucket, couchbaseutil.CouchbaseStorageBackend(targetBackend)); !ok {
			log.Info("[WARN] Cannot migrate bucket as it doesn't meet requirements for backend change.", "bucket-name", bucket.BucketName, "reason", reason, "target-backend", targetBackend)
			continue
		}

		log.Info("Updating storage backend of unmanaged bucket", "bucket-name", bucket.BucketName, "target-backend", targetBackend)

		bucket.BucketStorageBackend = couchbaseutil.CouchbaseStorageBackend(targetBackend)
		if err := couchbaseutil.UpdateBucket(&bucket).On(c.api, c.readyMembers()); err != nil {
			log.Error(err, "Bucket update failed", "bucket-name", bucket.BucketName, "target-backend", targetBackend)
			errs = append(errs, err)
		}
	}

	if len(errs) != 0 {
		return errors.CompositeValidationError(errs...)
	}

	return nil
}

func (c *Cluster) canBucketBeMigrated(b couchbaseutil.Bucket, backend couchbaseutil.CouchbaseStorageBackend) (bool, string) {
	if backend == couchbaseutil.CouchbaseStorageBackendMagma {
		if b.BucketMemoryQuota < 1024 {
			return false, fmt.Sprintf("memory quota (%v) below minimum %v", b.BucketMemoryQuota, 1024)
		}
	}

	if backend == couchbaseutil.CouchbaseStorageBackendCouchstore {
		scopes := couchbaseutil.ScopeList{}

		if err := couchbaseutil.ListScopes(b.BucketName, &scopes).On(c.api, c.readyMembers()); err != nil {
			return false, fmt.Sprintf("error when fetching scopes: %s", err.Error())
		}

		for _, scope := range scopes.Scopes {
			for _, collection := range scope.Collections {
				if collection.History != nil && *collection.History == true {
					return false, fmt.Sprintf("collection %s in scope %s has history enabled", collection.Name, scope.Name)
				}
			}
		}
	}

	return true, ""
}
