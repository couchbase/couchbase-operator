/*
Copyright 2021-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package cluster

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/client"
	"github.com/couchbase/couchbase-operator/pkg/unreconcilable"
	"github.com/couchbase/couchbase-operator/pkg/util"
	"github.com/couchbase/couchbase-operator/pkg/util/annotations"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/go-openapi/errors"

	"k8s.io/apimachinery/pkg/labels"
)

// recordBucketRef ties a Couchbase-side bucket name back to the CR that declares
// it. The gather functions are the one place that can see both names at once,
// since they part ways whenever spec.name is set and the bucket validators only
// ever get told the Couchbase-side one.
func recordBucketRef(tracker *unreconcilable.Tracker, kind, bucketName, crName string) {
	tracker.SetBucketRef(bucketName, unreconcilable.Ref{Kind: kind, Name: crName})
}

type SupportedFeature int

const (
	SupportedDurability SupportedFeature = iota
	SupportedBackendCouchstore
	SupportedBackendMagma
	SupportedHistoryRetention
	SupportedRank
	SupportedBlockSize
	SupportedCrossClusterVersioning
	Additional80Settings
	SupportedKVThrottle
	SupportedFileBasedRebalance
)

type SupportedFeatureMap map[SupportedFeature]bool

// gatherCouchbaseBuckets gathers all K8s CB buckets and marshalls them into canonical form.
//
//nolint:gocognit,gocyclo
func gatherCouchbaseBuckets(supportedFeatures SupportedFeatureMap, selector *couchbasev2.ObjectSelectorAsSelector, k8sBuckets []*couchbasev2.CouchbaseBucket, outputBuckets []couchbaseutil.Bucket, cluster *couchbasev2.CouchbaseCluster, client *client.Client, encryptionKeys couchbaseutil.EncryptionKeyList, tracker *unreconcilable.Tracker) []couchbaseutil.Bucket {
	durablitySupported := supportedFeatures[SupportedDurability]
	storageBackendSupported := supportedFeatures[SupportedBackendCouchstore]
	magmaStorageBackendSupported := supportedFeatures[SupportedBackendMagma]
	supportedHistoryRetention := supportedFeatures[SupportedHistoryRetention]
	supportedRank := supportedFeatures[SupportedRank]
	supportedBlockSize := supportedFeatures[SupportedBlockSize]
	supportedCrossClusterVersioning := supportedFeatures[SupportedCrossClusterVersioning]
	supportedAdditional80Settings := supportedFeatures[Additional80Settings]
	supportedKVThrottle := supportedFeatures[SupportedKVThrottle]
	supportedFileBasedRebalance := supportedFeatures[SupportedFileBasedRebalance]

	for _, bucket := range k8sBuckets {
		// There is deliberately no skip here. Gather builds the desired set, and
		// anything missing from that set reads further down as "the user no
		// longer wants this", which deletes the bucket from Couchbase Server.
		// Unreconcilable buckets get skipped at the create and update decision
		// instead, where the stakes are much lower.
		err := annotations.Populate(&bucket.Spec, bucket.Annotations)
		if err != nil {
			// we failed but its not worth stopping. log the error and continue
			log.Error(err, "failed to populate bucket with annotation", "cluster", cluster.NamespacedName())
		}

		if !selector.Matches(bucket.GetName(), labels.Set(bucket.Labels)) {
			continue
		}

		name := bucket.Name

		if bucket.Spec.Name != "" {
			name = string(bucket.Spec.Name)
		}

		recordBucketRef(tracker, couchbasev2.BucketCRDResourceKind, name, bucket.Name)

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

		// If eviction policy is not explicitly set in the CRD, fill in the server default so that
		// status always stores a resolved value (never ""), mirroring how storage backend behaves.
		if b.EvictionPolicy == "" {
			if b.BucketStorageBackend == couchbaseutil.CouchbaseStorageBackendMagma {
				b.EvictionPolicy = string(couchbasev2.CouchbaseBucketEvictionPolicyFullEviction)
			} else {
				b.EvictionPolicy = string(couchbasev2.CouchbaseBucketEvictionPolicyValueOnly)
			}
		}

		// Defaults to true, when bucket is magma.
		// Hence, setting it to true to avoid false reconciliation updates.
		if b.BucketStorageBackend == couchbaseutil.CouchbaseStorageBackendMagma && supportedHistoryRetention {
			historyRetentionCollectionDefaultTrue := true
			b.HistoryRetentionCollectionDefault = &historyRetentionCollectionDefaultTrue
		}

		// Although, the API doesn't need us to pass default values
		// but, our reconciler comparison fails, when nil. So, setting default values.

		if b.BucketStorageBackend == couchbaseutil.CouchbaseStorageBackendMagma {
			// MagmaSeqTreeDataBlockSize/MagmaKeyTreeDataBlockSize only supported on Magma
			if supportedBlockSize {
				b.MagmaSeqTreeDataBlockSize = notNilOrDefault(bucket.Spec.MagmaSeqTreeDataBlockSize, constants.MagmaSeqTreeDataDefaultBlockSize)
				b.MagmaKeyTreeDataBlockSize = notNilOrDefault(bucket.Spec.MagmaKeyTreeDataBlockSize, constants.MagmaKeyTreeDataDefaultBlockSize)
			}

			// CDC is only supported on Magma
			if supportedHistoryRetention && bucket.Spec.HistoryRetentionSettings != nil {
				// Only override the collection default when the user explicitly
				// sets it.  When nil the earlier magma default (true) is kept so
				// that a magma bucket always has collectionHistoryDefault=true
				// unless the user explicitly opts out.
				if bucket.Spec.HistoryRetentionSettings.CollectionDefault != nil {
					b.HistoryRetentionCollectionDefault = bucket.Spec.HistoryRetentionSettings.CollectionDefault
				}
				b.HistoryRetentionBytes = bucket.Spec.HistoryRetentionSettings.Bytes
				b.HistoryRetentionSeconds = bucket.Spec.HistoryRetentionSettings.Seconds
			}
		}

		if supportedRank {
			b.Rank = &bucket.Spec.Rank
		}

		if supportedCrossClusterVersioning {
			if bucket.Spec.EnableCrossClusterVersioning != nil {
				b.EnableCrossClusterVersioning = bucket.Spec.EnableCrossClusterVersioning
			} else {
				defaultCrossClusterVersioning := false
				b.EnableCrossClusterVersioning = &defaultCrossClusterVersioning
			}

			// NumVBuckets can only be changed after 8.0, but is returned for 7.6 as well.
			b.NumVBuckets = util.IntPtr(bucket.GetNumVBuckets(cluster))

			b.VersionPruningWindowHrs = notNilOrDefault(bucket.Spec.VersionPruningWindowHrs, constants.VersionPruningWindowHrsDefault)
		}

		if supportedAdditional80Settings {
			if bucket.Spec.AccessScannerEnabled != nil {
				b.AccessScannerEnabled = bucket.Spec.AccessScannerEnabled
			} else {
				defaultAccessScannerEnabled := true
				b.AccessScannerEnabled = &defaultAccessScannerEnabled
			}

			if bucket.Spec.ExpiryPagerSleepTime != nil {
				expiryPagerSleepTime := uint64(bucket.Spec.ExpiryPagerSleepTime.Duration.Seconds())
				b.ExpiryPagerSleepTime = &expiryPagerSleepTime
			} else {
				defaultExpiryPagerSleepTime := uint64(constants.ExpiryPagerSleepTimeDefaultSeconds * time.Second)
				b.ExpiryPagerSleepTime = &defaultExpiryPagerSleepTime
			}

			if bucket.Spec.WarmupBehavior != "" {
				b.WarmupBehavior = string(bucket.Spec.WarmupBehavior)
			} else {
				b.WarmupBehavior = constants.BucketWarmupBehaviorDefault
			}

			if bucket.Spec.MemoryLowWatermark != nil {
				b.MemoryLowWatermark = bucket.Spec.MemoryLowWatermark
			} else {
				defaultMemoryLowWatermark := constants.MemoryLowWatermarkDefault
				b.MemoryLowWatermark = &defaultMemoryLowWatermark
			}

			if bucket.Spec.MemoryHighWatermark != nil {
				b.MemoryHighWatermark = bucket.Spec.MemoryHighWatermark
			} else {
				defaultMemoryHighWatermark := constants.MemoryHighWatermarkDefault
				b.MemoryHighWatermark = &defaultMemoryHighWatermark
			}

			if bucket.Spec.DurabilityImpossibleFallback != "" {
				b.DurabilityImpossibleFallback = couchbaseutil.DurabilityImpossibleFallback(bucket.Spec.DurabilityImpossibleFallback)
			} else {
				b.DurabilityImpossibleFallback = constants.DurabilityImpossibleFallbackDefault
			}

			noRestart := bucket.Spec.OnlineEvictionPolicyChange
			b.NoRestart = &noRestart

			b.EncryptionAtRestDekRotationInterval = util.IntPtr(constants.DefaultEncryptionAtRestRotationInterval)
			b.EncryptionAtRestDekLifetime = util.IntPtr(constants.DefaultEncryptionAtRestKeyLifetime)
			b.EncryptionAtRestKeyID = util.IntPtr(constants.DefaultEncryptionAtRestKeyID)

			if bucket.Spec.EncryptionAtRest != nil && bucket.Spec.EncryptionAtRest.KeyName != "" {
				if key := encryptionKeys.GetKeyByName(bucket.Spec.EncryptionAtRest.KeyName); key == nil {
					log.Info("Encryption key not found for bucket", "cluster", cluster.NamespacedName(), "bucket", bucket.Name, "key-name", bucket.Spec.EncryptionAtRest.KeyName)
				} else if !key.CanEncryptBucket(bucket.Name) {
					log.Info("Encryption key cannot encrypt bucket", "cluster", cluster.NamespacedName(), "bucket", bucket.Name, "key-name", bucket.Spec.EncryptionAtRest.KeyName)
				} else {
					b.EncryptionAtRestKeyID = util.IntPtr(key.ID)

					if bucket.Spec.EncryptionAtRest.RotationInterval != nil {
						b.EncryptionAtRestDekRotationInterval = util.IntPtr(int(bucket.Spec.EncryptionAtRest.RotationInterval.Seconds()))
					}
					if bucket.Spec.EncryptionAtRest.KeyLifetime != nil {
						b.EncryptionAtRestDekLifetime = util.IntPtr(int(bucket.Spec.EncryptionAtRest.KeyLifetime.Seconds()))
					}
				}
			}
		}

		if supportedKVThrottle {
			setBucketThrottleSettings(&b, bucket.Spec.ThrottleReserved, bucket.Spec.ThrottleHardLimit)
		}

		if supportedFileBasedRebalance {
			setBucketRebalanceType(&b, bucket.Spec.DataServiceRebalanceType)
		}

		autoCompactionSettings, purgeInterval := gatherBucketAutoCompactionSettings(bucket.Spec.AutoCompaction, b.BucketStorageBackend, cluster.Spec.ClusterSettings.AutoCompaction)
		b.AutoCompactionSettings = autoCompactionSettings
		b.PurgeInterval = purgeInterval

		outputBuckets = append(outputBuckets, b)
	}

	return outputBuckets
}

// setBucketRebalanceType sets the bucket's Data Service rebalance type to the value from the CR
// or to the server default if the CR omits it.
func setBucketRebalanceType(b *couchbaseutil.Bucket, rebalanceType couchbasev2.DataServiceRebalanceType) {
	if rebalanceType != "" {
		b.DataServiceRebalanceType = string(rebalanceType)
	} else {
		b.DataServiceRebalanceType = constants.BucketDataServiceRebalanceTypeDefault
	}
}

// setBucketThrottleSettings copies the bucket's KV rate limiting values from the CR onto the
// bucket we send to the server. When the user omits a value we fill in the same default the
// server uses, so that what we send matches what the server reports back. If we left it unset
// instead, our value would never match the server's default and the reconciler would keep seeing
// a difference and updating the bucket on every loop.
func setBucketThrottleSettings(b *couchbaseutil.Bucket, reserved, hardLimit *int64) {
	// Reserved defaults to 0, the reservation is a bucket's guaranteed minimum share of node
	// capacity, so 0 means reserve nothing and the bucket simply competes for whatever capacity
	// is free. This is also the server's own default.
	throttleReserved := uint64(0)
	if reserved != nil {
		throttleReserved = uint64(*reserved)
	}

	b.ThrottleReserved = &throttleReserved

	// Hard limit defaults to the maximum uint64, which the server treats as unlimited, the hard
	// limit is a bucket's maximum allowed throughput, so the largest possible value means the
	// bucket has no upper cap of its own. This is also the server's own default.
	throttleHardLimit := uint64(math.MaxUint64)
	if hardLimit != nil {
		throttleHardLimit = uint64(*hardLimit)
	}

	b.ThrottleHardLimit = &throttleHardLimit
}

func notNilOrDefault(val *uint64, defaultVal uint64) *uint64 {
	if val != nil {
		return val
	}

	return &defaultVal
}

func applyBucketStorageBackend(b *couchbaseutil.Bucket, bucket *couchbasev2.CouchbaseBucket, storageBackendCouchstoreSupported, storageBackendMagmaSupported bool, cluster *couchbasev2.CouchbaseCluster) {
	b.BucketStorageBackend = couchbaseutil.CouchbaseStorageBackend(k8sutil.GetBucketStorageBackend(bucket, storageBackendCouchstoreSupported, storageBackendMagmaSupported, cluster))
}

// gatherEphemeralBuckets gathers all K8s CB Ephemeral buckets and marshalls them into canonical form.
//
//nolint:gocognit
func gatherEphemeralBuckets(supportedFeatures SupportedFeatureMap, selector *couchbasev2.ObjectSelectorAsSelector, k8sEphemeralBuckets []*couchbasev2.CouchbaseEphemeralBucket, outputBuckets []couchbaseutil.Bucket, client *client.Client, cluster *couchbasev2.CouchbaseCluster, tracker *unreconcilable.Tracker) []couchbaseutil.Bucket {
	durablitySupported := supportedFeatures[SupportedDurability]
	supportedRank := supportedFeatures[SupportedRank]
	supportedCrossClusterVersioning := supportedFeatures[SupportedCrossClusterVersioning]
	supportedAdditional80Settings := supportedFeatures[Additional80Settings]
	supportedKVThrottle := supportedFeatures[SupportedKVThrottle]
	supportedFileBasedRebalance := supportedFeatures[SupportedFileBasedRebalance]

	for _, bucket := range k8sEphemeralBuckets {
		err := annotations.Populate(&bucket.Spec, bucket.Annotations)
		if err != nil {
			// we failed but its not worth stopping. log the error and continue
			log.Error(err, "failed to populate bucket with annotation")
		}

		if !selector.Matches(bucket.GetName(), labels.Set(bucket.Labels)) {
			continue
		}

		name := bucket.Name

		if bucket.Spec.Name != "" {
			name = string(bucket.Spec.Name)
		}

		recordBucketRef(tracker, couchbasev2.EphemeralBucketCRDResourceKind, name, bucket.Name)

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

		// If eviction policy is not explicitly set in the CRD, fill in the server default so that
		// status always stores a resolved value (never ""), mirroring how storage backend behaves.
		if b.EvictionPolicy == "" {
			b.EvictionPolicy = string(couchbasev2.CouchbaseEphemeralBucketEvictionPolicyNoEviction)
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

		if supportedCrossClusterVersioning {
			if bucket.Spec.EnableCrossClusterVersioning != nil {
				b.EnableCrossClusterVersioning = bucket.Spec.EnableCrossClusterVersioning
			} else {
				defaultCrossClusterVersioning := false
				b.EnableCrossClusterVersioning = &defaultCrossClusterVersioning
			}

			b.NumVBuckets = util.IntPtr(1024)

			b.VersionPruningWindowHrs = notNilOrDefault(bucket.Spec.VersionPruningWindowHrs, constants.VersionPruningWindowHrsDefault)

			b.NumVBuckets = util.IntPtr(1024)
		}

		if supportedAdditional80Settings {
			b.NumVBuckets = util.IntPtr(1024)

			apply80Settings(&b, bucket)
		}

		if supportedKVThrottle {
			setBucketThrottleSettings(&b, bucket.Spec.ThrottleReserved, bucket.Spec.ThrottleHardLimit)
		}

		if supportedFileBasedRebalance {
			setBucketRebalanceType(&b, bucket.Spec.DataServiceRebalanceType)
		}

		outputBuckets = append(outputBuckets, b)
	}

	return outputBuckets
}

func apply80Settings(b *couchbaseutil.Bucket, bucket *couchbasev2.CouchbaseEphemeralBucket) {
	if bucket.Spec.ExpiryPagerSleepTime != nil {
		expiryPagerSleepTime := uint64(bucket.Spec.ExpiryPagerSleepTime.Duration.Seconds())
		b.ExpiryPagerSleepTime = &expiryPagerSleepTime
	} else {
		defaultExpiryPagerSleepTime := uint64(constants.ExpiryPagerSleepTimeDefaultSeconds * time.Second)
		b.ExpiryPagerSleepTime = &defaultExpiryPagerSleepTime
	}

	if bucket.Spec.WarmupBehavior != "" {
		b.WarmupBehavior = string(bucket.Spec.WarmupBehavior)
	} else {
		b.WarmupBehavior = constants.BucketWarmupBehaviorDefault
	}

	if bucket.Spec.MemoryLowWatermark != nil {
		b.MemoryLowWatermark = bucket.Spec.MemoryLowWatermark
	} else {
		defaultMemoryLowWatermark := constants.MemoryLowWatermarkDefault
		b.MemoryLowWatermark = &defaultMemoryLowWatermark
	}

	if bucket.Spec.MemoryHighWatermark != nil {
		b.MemoryHighWatermark = bucket.Spec.MemoryHighWatermark
	} else {
		defaultMemoryHighWatermark := constants.MemoryHighWatermarkDefault
		b.MemoryHighWatermark = &defaultMemoryHighWatermark
	}

	b.NumVBuckets = util.IntPtr(1024)

	b.DurabilityImpossibleFallback = couchbaseutil.DurabilityImpossibleFallback(bucket.Spec.DurabilityImpossibleFallback)
}

// gatherMemcachedBuckets gathers all K8s CB Memcached buckets and marshalls them into canonical form.
func gatherMemcachedBuckets(selector *couchbasev2.ObjectSelectorAsSelector, k8sMemcachedBuckets []*couchbasev2.CouchbaseMemcachedBucket, outputBuckets []couchbaseutil.Bucket, client *client.Client, tracker *unreconcilable.Tracker) []couchbaseutil.Bucket {
	for _, bucket := range k8sMemcachedBuckets {
		if !selector.Matches(bucket.GetName(), labels.Set(bucket.Labels)) {
			continue
		}

		name := bucket.Name

		if bucket.Spec.Name != "" {
			name = string(bucket.Spec.Name)
		}

		recordBucketRef(tracker, couchbasev2.MemcachedBucketCRDResourceKind, name, bucket.Name)

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
	selector, err := c.cluster.GetBucketObjectSelector()
	if err != nil {
		return nil, err
	}

	supportedFeatures := make(map[SupportedFeature]bool)

	durablitySupported := c.SupportsVersionFeatures("6.6.0")

	supportedFeatures[SupportedDurability] = durablitySupported

	// // storageBackend is only allowed above CB version 7.0.0.
	storageBackendSupported := c.SupportsVersionFeatures("7.0.0")

	supportedFeatures[SupportedBackendCouchstore] = storageBackendSupported
	// // magma storageBackend is only allowed above CB version 7.1.0.
	magmaStorageBackendSupported := c.SupportsVersionFeatures("7.1.0")

	supportedFeatures[SupportedBackendMagma] = magmaStorageBackendSupported

	atleast720 := c.SupportsVersionFeatures("7.2.0")

	supportedFeatures[SupportedHistoryRetention] = atleast720
	supportedFeatures[SupportedBlockSize] = atleast720

	rankSupported := c.SupportsVersionFeatures("7.6.0")

	supportedFeatures[SupportedRank] = rankSupported

	atleast76 := c.SupportsVersionFeatures("7.6.0")

	atleast80 := c.SupportsVersionFeatures("8.0.0")
	supportedFeatures[SupportedCrossClusterVersioning] = atleast76

	supportedFeatures[Additional80Settings] = atleast80

	// KV rate limiting (per bucket throttleReserved and throttleHardLimit) is available in 8.1.0+.
	supportedFeatures[SupportedKVThrottle] = c.SupportsVersionFeatures("8.1.0")

	// Per bucket Data Service rebalance type (dataServiceRebalanceType) is available in 8.1.0+.
	supportedFeatures[SupportedFileBasedRebalance] = c.SupportsVersionFeatures("8.1.0")

	allBuckets := []couchbaseutil.Bucket{}

	couchbaseBuckets := c.k8s.CouchbaseBuckets.List()
	ephemeralBuckets := c.k8s.CouchbaseEphemeralBuckets.List()

	encryptionKeys := couchbaseutil.EncryptionKeyList{}
	if atleast80 {
		err = couchbaseutil.ListEncryptionKeys(&encryptionKeys).On(c.api, c.readyMembers())
		if err != nil {
			return nil, err
		}
	}

	allBuckets = gatherCouchbaseBuckets(supportedFeatures, selector, couchbaseBuckets, allBuckets, c.cluster, c.k8s, encryptionKeys, c.unreconcilable)
	allBuckets = gatherEphemeralBuckets(supportedFeatures, selector, ephemeralBuckets, allBuckets, c.k8s, c.cluster, c.unreconcilable)
	allBuckets = gatherMemcachedBuckets(selector, c.k8s.CouchbaseMemcachedBuckets.List(), allBuckets, c.k8s, c.unreconcilable)

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
				// If the server-reported eviction policy differs from the last-reconciled value
				// in cluster status, the server was externally patched (drift). Reset
				// a.EvictionPolicy to the status value so the validation runner never sees a
				// spurious diff.
				if statusEviction := c.cluster.Status.GetBucketEvictionPolicyFromStatus(a.BucketName); statusEviction != "" &&
					statusEviction != a.EvictionPolicy {
					a.EvictionPolicy = statusEviction
				}

				// If the server-reported backend differs from the last-reconciled value
				// in cluster status, the server was externally patched (drift). Reset
				// a.BucketStorageBackend to the status value so that
				// ConvertAbstractBucketToAPIBucket sets Spec.StorageBackend to the
				// last-reconciled backend. CheckChangeConstraintsBucket then sees
				// prevBackend == currBackend and migration validators don't fire.
				if statusBackend := c.cluster.Status.GetBucketStorageBackendFromStatus(a.BucketName); statusBackend != "" &&
					couchbaseutil.CouchbaseStorageBackend(statusBackend) != a.BucketStorageBackend {
					a.BucketStorageBackend = couchbaseutil.CouchbaseStorageBackend(statusBackend)
				}

				if !reflect.DeepEqual(r, a) {
					updateBuckets[a] = r
				}

				break
			}
		}
	}

	return updateBuckets, nil
}

// bucketUpdate holds the CBS-actual (from) and CRD-desired (to) representations of a
// bucket that requires an update.  Carrying both values allows the reconcile loop to
// detect field-level transitions (e.g. collectionHistoryDefault true→false) without
// an additional CBS REST call.
type bucketUpdate struct {
	from couchbaseutil.Bucket
	to   couchbaseutil.Bucket
}

// inspectBuckets compares Kubernetes buckets with Couchbase buckets and returns lists
// of buckets to create, update or remove and the requested set for status updates.
//
//nolint:gocognit,gocyclo
func (c *Cluster) inspectBuckets() ([]couchbaseutil.Bucket, []bucketUpdate, []couchbaseutil.Bucket, []couchbaseutil.Bucket, []couchbaseutil.Bucket, error) {
	unfilteredRequested, err := c.gatherBuckets()
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	actual := couchbaseutil.BucketList{}
	if err := couchbaseutil.ListBuckets(&actual).On(c.api, c.readyMembers()); err != nil {
		return nil, nil, nil, nil, nil, err
	}

	isOver71, err := c.IsAtLeastVersion("7.1.0")
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	targetVersion80, err := c.IsAtLeastVersion("8.0.0")
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	requested := []couchbaseutil.Bucket{}
	for _, bucket := range unfilteredRequested {
		if bucket.BucketType == constants.BucketTypeMemcached && targetVersion80 {
			log.Info("Memcached buckets are not supported on Couchbase Server versions 8.0.0+ and will be ignored.", "cluster", c.namespacedName(), "bucket-name", bucket.BucketName)
			continue
		}

		requested = append(requested, bucket)
	}

	atLeast80 := c.SupportsVersionFeatures("8.0.0")

	create := []couchbaseutil.Bucket{}
	update := []bucketUpdate{}
	updateDuringMigration := []couchbaseutil.Bucket{}
	remove := []couchbaseutil.Bucket{}

	// Do an exhaustive search of requested buckets in the actual list, creating and
	// updating as necessary.
	for _, r := range requested {
		found := false

		// Skip buckets that failed change-constraint validation — they must
		// not be created, updated, or deleted until the user fixes the CRD.
		if c.unreconcilable.IsSkipped(unreconcilable.KindBucketName, r.BucketName) {
			continue
		}

		for _, a := range actual {
			if r.BucketName == a.BucketName {
				// If the bucket is a sample bucket, we don't update it until this field is false or removed to avoid unnecessary updates.
				if found = r.SampleBucket; found {
					continue
				}

				doCrossClusterVersioningChecks(&r, &a, c.cluster)

				// We should never set the numVBuckets field for buckets pre 8.0.0.
				// This shouldn't get through the dac but we're adding some extra protection.
				if !atLeast80 {
					r.NumVBuckets = nil
					a.NumVBuckets = nil
				}

				// Normalize a.NoRestart when CBS reports no per-node overrides (nil).
				// If overrides exist (a=&true) and user wants offline (r=&false), the
				// diff is kept so UpdateBucket sends noRestart=false to trigger a restart.
				if r.NoRestart != nil && a.NoRestart == nil {
					a.NoRestart = r.NoRestart
				}

				// If eviction policy is not explicitly set in the CRD, the operator enforces its
				// resolved default (valueOnly for couchstore, fullEviction for magma). Log when
				// the server currently differs so the change is visible in operator logs.
				if !isEvictionPolicyExplicitlySet(c, &r) {
					if r.EvictionPolicy != a.EvictionPolicy {
						log.Info("Bucket eviction policy not explicitly set in CRD, defaulting to requested value.", "cluster", c.namespacedName(), "bucket-name", r.BucketName, "requested-eviction-policy", r.EvictionPolicy)
					}
				}

				// If storage backend is not explicitly set in the CRD, the operator enforces its
				// resolved default. Log when the server currently differs.
				if !isStorageBackendExplicitlySet(c, &r) {
					if r.BucketStorageBackend != a.BucketStorageBackend {
						log.Info("Bucket storage backend not explicitly set in CRD, defaulting to requested value.", "cluster", c.namespacedName(), "bucket-name", r.BucketName, "requested-backend", r.BucketStorageBackend)
					}
				}

				if a.BucketType != r.BucketType {
					log.Info("Bucket type cannot be changed so recreating with requested type", "cluster", c.namespacedName(), "bucket-name", r.BucketName, "current-type", a.BucketType, "requested-type", r.BucketType)
					remove = append(remove, a)
					create = append(create, r)
				} else if !reflect.DeepEqual(r, a) {
					setBucketFieldsForEncoding(&r, isOver71)

					// During storage backend migration, use specialized API that only sends
					// fields CBS allows during migration. For eviction-only migration
					// (BucketEvictionMigrating), use the normal path so all fields are sent.
					if c.cluster.HasCondition(couchbasev2.ClusterConditionBucketMigration) {
						updateDuringMigration = append(updateDuringMigration, r)
					} else {
						update = append(update, bucketUpdate{from: a, to: r})
					}
					c.logUpdate(a, r)
				}

				found = true

				break
			}
		}

		if !found {
			if !atLeast80 {
				// Dac should prevent this from getting through but we're adding some extra protection.
				r.NumVBuckets = nil
			}

			setBucketFieldsForEncoding(&r, isOver71)

			create = append(create, r)
		}
	}

	// Do an exhaustive search of actual buckets in the requested list, deleting
	// as necessary.
	for _, a := range actual {
		found := false

		for _, r := range unfilteredRequested {
			if a.BucketName == r.BucketName {
				matchBackendsIfBefore76(&r, &a, c.cluster)

				found = true

				break
			}
		}

		if !found {
			remove = append(remove, a)
		}
	}

	return create, update, updateDuringMigration, remove, requested, nil
}

func isStorageBackendExplicitlySet(c *Cluster, r *couchbaseutil.Bucket) bool {
	couchbaseBuckets := c.k8s.CouchbaseBuckets.List()
	for _, bucket := range couchbaseBuckets {
		if bucket.GetCouchbaseName() == r.BucketName {
			_, explicit := bucket.GetStorageBackend(c.cluster)
			return explicit
		}
	}

	return true
}

func isEvictionPolicyExplicitlySet(c *Cluster, r *couchbaseutil.Bucket) bool {
	switch r.BucketType {
	case constants.BucketTypeCouchbase:
		for _, bucket := range c.k8s.CouchbaseBuckets.List() {
			if bucket.GetCouchbaseName() == r.BucketName {
				return bucket.Spec.EvictionPolicy != ""
			}
		}
	case constants.BucketTypeEphemeral:
		for _, bucket := range c.k8s.CouchbaseEphemeralBuckets.List() {
			if bucket.GetCouchbaseName() == r.BucketName {
				return bucket.Spec.EvictionPolicy != ""
			}
		}
	}

	return true
}

// Since, BucketStorageBackend is non-editable, once created for CB version < 7.6.0.
// This avoids running any update reconcile loop,
// if BucketStorageBackend seems to be the only one different.
func matchBackendsIfBefore76(r, a *couchbaseutil.Bucket, cluster *couchbasev2.CouchbaseCluster) {
	if isAtleast76, err := cluster.IsAtLeastVersion("7.6.0"); err == nil && !isAtleast76 {
		r.BucketStorageBackend = a.BucketStorageBackend
		if r.BucketStorageBackend != a.BucketStorageBackend {
			r.BucketStorageBackend = a.BucketStorageBackend

			log.Info("[WARN] spec.storageBackend cannot be changed for server version below 7.6.0", "cluster", cluster.NamespacedName())
		}
	}
}

func doCrossClusterVersioningChecks(r, a *couchbaseutil.Bucket, cluster *couchbasev2.CouchbaseCluster) {
	if isAtleast762, err := cluster.IsAtLeastVersion("7.6.2"); err != nil {
		log.Error(err, "Failed to check server version for cross cluster versioning", "cluster", cluster.NamespacedName())
		return
	} else if !isAtleast762 {
		return
	}

	if a.EnableCrossClusterVersioning != nil && *a.EnableCrossClusterVersioning {
		if r.EnableCrossClusterVersioning == nil || !*r.EnableCrossClusterVersioning {
			log.Info("[WARN] spec.enableCrossClusterVersioning cannot be disabled once enabled", "cluster", cluster.NamespacedName())
		}

		// For some reason, the API doesn't like us setting this to ever again if it's true.
		r.EnableCrossClusterVersioning = nil
		a.EnableCrossClusterVersioning = nil
	}
}

// applyBucketUpdates applies a set of bucket updates, patching _default/_default
// history before the CBS call when collectionHistoryDefault transitions true→false.
func (c *Cluster) applyBucketUpdates(updates []bucketUpdate) error {
	for i := range updates {
		u := &updates[i]

		// If collectionHistoryDefault is transitioning from true to false, patch
		// _default/_default to history=false before the bucket update.
		chdChangingToFalse := u.from.HistoryRetentionCollectionDefault != nil &&
			*u.from.HistoryRetentionCollectionDefault &&
			u.to.HistoryRetentionCollectionDefault != nil &&
			!*u.to.HistoryRetentionCollectionDefault
		if chdChangingToFalse {
			if err := c.patchDefaultCollectionHistory(u.to.BucketName); err != nil {
				return err
			}
		}

		if err := couchbaseutil.UpdateBucket(&u.to).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		log.Info("Bucket updated", "cluster", c.namespacedName(), "name", u.to.BucketName)
		c.raiseEvent(k8sutil.BucketEditEvent(u.to.BucketName, c.cluster))
	}

	return nil
}

// reconcile buckets by adding or removing
// buckets one at a time based on comparison
// of existing buckets to cluster spec.
func (c *Cluster) reconcileBuckets() error {
	if !c.cluster.Spec.Buckets.Managed {
		return nil
	}

	// Defer all bucket operations until pending init pods have CBS-added.
	if pendingInit := c.getPendingInitPods(); len(pendingInit) > 0 {
		log.V(1).Info("Deferring bucket reconciliation until pending pods are CBS-initialized",
			"cluster", c.namespacedName(), "pending", k8sutil.GetPodNames(pendingInit))
		return nil
	}

	create, updates, updateDuringMigration, remove, requested, err := c.inspectBuckets()
	if err != nil {
		return err
	}

	for _, bucket := range remove {
		if err := couchbaseutil.DeleteBucket(bucket.BucketName).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		log.Info("Bucket deleted", "cluster", c.namespacedName(), "name", bucket.BucketName)
		c.raiseEvent(k8sutil.BucketDeleteEvent(bucket.BucketName, c.cluster))
	}

	for i := range create {
		bucket := &create[i]

		if bucket.SampleBucket {
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

	if err := c.applyBucketUpdates(updates); err != nil {
		return err
	}

	for i := range updateDuringMigration {
		bucket := &updateDuringMigration[i]
		if err := couchbaseutil.UpdateBucketDuringMigration(bucket).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		log.Info("Bucket updated during migration", "cluster", c.namespacedName(), "name", bucket.BucketName)
		c.raiseEvent(k8sutil.BucketEditEvent(bucket.BucketName, c.cluster))
	}

	// To avoid API updates, we record the name of each bucket on the system (this will
	// be lexically sorted), and we add buckets to the status in a deterministic order.
	names := make([]string, len(requested))
	statuses := map[string]couchbasev2.BucketStatus{}

	// Snapshot the existing status so that failed-validation buckets keep
	// their last-known-good status rather than being overwritten with the
	// (invalid) desired state from the CRD.
	existingStatuses := make(map[string]couchbasev2.BucketStatus, len(c.cluster.Status.Buckets))
	for _, s := range c.cluster.Status.Buckets {
		existingStatuses[s.BucketName] = s
	}

	for i, bucket := range requested {
		names[i] = bucket.BucketName

		if c.unreconcilable.IsSkipped(unreconcilable.KindBucketName, bucket.BucketName) {
			if existing, ok := existingStatuses[bucket.BucketName]; ok {
				statuses[bucket.BucketName] = existing

				continue
			}
		}

		statuses[bucket.BucketName] = bucketToClusterStatus(bucket)
	}

	sort.Strings(names)

	c.cluster.Status.Buckets = []couchbasev2.BucketStatus{}

	for _, name := range names {
		c.cluster.Status.Buckets = append(c.cluster.Status.Buckets, statuses[name])
	}

	return nil
}

func bucketToClusterStatus(b couchbaseutil.Bucket) couchbasev2.BucketStatus {
	return couchbasev2.BucketStatus{
		BucketName:           b.BucketName,
		BucketType:           b.BucketType,
		BucketStorageBackend: string(b.BucketStorageBackend),
		NumVBuckets:          b.NumVBuckets,
		BucketMemoryQuota:    b.BucketMemoryQuota,
		BucketReplicas:       b.BucketReplicas,
		IoPriority:           string(b.IoPriority),
		EvictionPolicy:       b.EvictionPolicy,
		ConflictResolution:   b.ConflictResolution,
		EnableFlush:          b.EnableFlush,
		EnableIndexReplica:   b.EnableIndexReplica,
		CompressionMode:      string(b.CompressionMode),
	}
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
			log.Info("[WARN] Cannot migrate bucket as it doesn't meet requirements for backend change.", "cluster", c.namespacedName(), "bucket-name", bucket.BucketName, "reason", reason, "target-backend", targetBackend)
			continue
		}

		log.Info("Updating storage backend of unmanaged bucket", "cluster", c.namespacedName(), "bucket-name", bucket.BucketName, "target-backend", targetBackend)

		bucket.BucketStorageBackend = couchbaseutil.CouchbaseStorageBackend(targetBackend)
		if err := couchbaseutil.UpdateBucketDuringMigration(&bucket).On(c.api, c.readyMembers()); err != nil {
			log.Error(err, "Bucket update failed", "cluster", c.namespacedName(), "bucket-name", bucket.BucketName, "target-backend", targetBackend)
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

// patchDefaultCollectionHistory disables history retention on the _default scope's
// _default collection and is called as part of a magma → couchstore backend migration.
// That collection is never managed by a user CRD (it is preserved implicitly), so the
// operator is responsible for clearing its history flag before the backend change.
// The Couchbase Server does not retroactively propagate a bucket-level
// collectionHistoryDefault change to existing collections.
func (c *Cluster) patchDefaultCollectionHistory(bucketName string) error {
	scopes := couchbaseutil.ScopeList{}
	if err := couchbaseutil.ListScopes(bucketName, &scopes).On(c.api, c.readyMembers()); err != nil {
		return fmt.Errorf("failed to list scopes for bucket %s: %w", bucketName, err)
	}

	defaultCollection := scopes.GetScope("_default").GetCollection("_default")
	if defaultCollection.Name == "" || defaultCollection.History == nil || !*defaultCollection.History {
		return nil
	}

	falseVal := false
	defaultCollection.History = &falseVal
	if err := couchbaseutil.PatchCollection(bucketName, "_default", defaultCollection).On(c.api, c.readyMembers()); err != nil {
		return fmt.Errorf("failed to disable history retention on _default/_default in bucket %s: %w", bucketName, err)
	}

	log.Info("Disabled history retention on _default/_default collection for storage backend migration",
		"cluster", c.namespacedName(), "bucket", bucketName)

	return nil
}

// gatherBucketAutoCompactionSettings will convert auto-compaction settings defined on bucket CRD's into auto-compaction settings that can be recognised
// and mapped to by the couchbase server. The enabled flag is set to true if any auto-compaction settings are set at a bucket level.
func gatherBucketAutoCompactionSettings(crdSettings *couchbasev2.AutoCompactionSpecBucket, storageBackend couchbaseutil.CouchbaseStorageBackend, clusterSettings *couchbasev2.AutoCompaction) (couchbaseutil.BucketAutoCompactionSettings, *float64) {
	if crdSettings == nil || clusterSettings == nil {
		return couchbaseutil.BucketAutoCompactionSettings{Enabled: false, Settings: nil}, nil
	}

	settings := couchbaseutil.AutoCompactionAutoCompactionSettings{
		// ParallelDBAndViewCompaction is a global settings and required for setting bucket level auto-compaction settings, so we should just use the cluster level value
		ParallelDBAndViewCompaction: clusterSettings.ParallelCompaction,
	}

	// Whether auto-compaction is enabled for the bucket. We only care about magma fields when a magma storage backend is used and vice versa for couchstore buckets and therefore
	// only want to set the auto-compaction settings at a bucket level when the correct fields are being used.
	enabled := false

	switch storageBackend {
	case couchbaseutil.CouchbaseStorageBackendCouchstore:
		enabled = configureCouchstoreAutoCompactionSettings(crdSettings, &settings)
	case couchbaseutil.CouchbaseStorageBackendMagma:
		enabled = configureMagmaAutoCompactionSettings(crdSettings, &settings, clusterSettings)
	}

	var purgeInterval *float64

	// If the bucket CRD has not set a value for the purge interval, we should use the cluster level value.
	if crdSettings.TombstonePurgeInterval != nil {
		pi := crdSettings.TombstonePurgeInterval.Hours() / 24.0
		purgeInterval = &pi
		enabled = true
	} else if clusterSettings.TombstonePurgeInterval != nil {
		pi := clusterSettings.TombstonePurgeInterval.Hours() / 24.0
		purgeInterval = &pi
	}

	// If no relevant auto-compaciton fields have been set in the CRD, we can ignore the bucket level auto-compaction settings
	if !enabled {
		return couchbaseutil.BucketAutoCompactionSettings{Enabled: false, Settings: nil}, nil
	}

	return couchbaseutil.BucketAutoCompactionSettings{
		Enabled:  enabled,
		Settings: &settings,
	}, purgeInterval
}

// configureCouchstoreAutoCompactionSettings handles auto-compaction settings that are unique to Couchstore buckets.
func configureCouchstoreAutoCompactionSettings(crdSettings *couchbasev2.AutoCompactionSpecBucket, settings *couchbaseutil.AutoCompactionAutoCompactionSettings) bool {
	enabled := false

	if crdSettings.DatabaseFragmentationThreshold != nil {
		if crdSettings.DatabaseFragmentationThreshold.Percent != nil {
			settings.DatabaseFragmentationThreshold.Percentage = *crdSettings.DatabaseFragmentationThreshold.Percent
			enabled = true
		}

		if crdSettings.DatabaseFragmentationThreshold.Size != nil {
			settings.DatabaseFragmentationThreshold.Size = crdSettings.DatabaseFragmentationThreshold.Size.Value()
			enabled = true
		}
	}

	if crdSettings.ViewFragmentationThreshold != nil {
		if crdSettings.ViewFragmentationThreshold.Percent != nil {
			settings.ViewFragmentationThreshold.Percentage = *crdSettings.ViewFragmentationThreshold.Percent
			enabled = true
		}

		if crdSettings.ViewFragmentationThreshold.Size != nil {
			settings.ViewFragmentationThreshold.Size = crdSettings.ViewFragmentationThreshold.Size.Value()
			enabled = true
		}
	}

	// Time window should only be provided if fully specified by a user
	if crdSettings.TimeWindow != nil && crdSettings.TimeWindow.Start != nil && crdSettings.TimeWindow.End != nil {
		autoCompactionTimePeriod := couchbaseutil.AutoCompactionAllowedTimePeriod{
			AbortOutside: crdSettings.TimeWindow.AbortCompactionOutsideWindow,
		}
		parts := strings.Split(*crdSettings.TimeWindow.Start, ":")
		autoCompactionTimePeriod.FromHour, _ = strconv.Atoi(parts[0])
		autoCompactionTimePeriod.FromMinute, _ = strconv.Atoi(parts[1])
		parts = strings.Split(*crdSettings.TimeWindow.End, ":")
		autoCompactionTimePeriod.ToHour, _ = strconv.Atoi(parts[0])
		autoCompactionTimePeriod.ToMinute, _ = strconv.Atoi(parts[1])
		settings.AllowedTimePeriod = &autoCompactionTimePeriod
		enabled = true
	}

	return enabled
}

// configureMagmaAutoCompactionSettings handles auto-compaction settings that are unique to Magma buckets.
func configureMagmaAutoCompactionSettings(crdSettings *couchbasev2.AutoCompactionSpecBucket, settings *couchbaseutil.AutoCompactionAutoCompactionSettings, defaults *couchbasev2.AutoCompaction) bool {
	switch {
	case crdSettings.MagmaFragmentationThresholdPercentage != nil:
		settings.MagmaFragmentationThresholdPercentage = *crdSettings.MagmaFragmentationThresholdPercentage
		return true
	case defaults.MagmaFragmentationThresholdPercentage != nil:
		settings.MagmaFragmentationThresholdPercentage = *defaults.MagmaFragmentationThresholdPercentage
	default:
		// If not defined in CRD, use cluster level or default value of 50.
		settings.MagmaFragmentationThresholdPercentage = 50
	}

	return false
}

func setBucketFieldsForEncoding(b *couchbaseutil.Bucket, isOver71 bool) {
	if b.AutoCompactionSettings.Enabled {
		b.AutoCompactionSettings.Settings.SetAutoCompactionUndefinedFieldsForEncoding(isOver71)
	}
}
