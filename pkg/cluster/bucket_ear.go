/*
Copyright 2026-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package cluster

import (
	"slices"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"

	"k8s.io/apimachinery/pkg/labels"
)

// bucketEncryptionWait records that a bucket names an encryption key the
// operator is still provisioning, so the bucket must not fall back to being
// unencrypted in the meantime.
type bucketEncryptionWait struct {
	// keyName is the CouchbaseEncryptionKey the bucket names.
	keyName string

	// reason says what the key is still missing.
	reason string

	// keyNamesBucket is set when the key lists the bucket in spec.usage.buckets.
	// The server rejects a key whose usage names a bucket it does not have, so
	// such a key cannot be created before its bucket, and a bucket that does not
	// exist yet must not wait for it.
	keyNamesBucket bool
}

// bucketsWaitingForEncryptionKey returns, by Couchbase bucket name, the couchbase
// buckets that name an encryption key the operator manages but cannot use yet which could be either due to
// the key not being on the server, or does not permit the bucket yet.
func (c *Cluster) bucketsWaitingForEncryptionKey() map[string]bucketEncryptionWait {
	if !c.IsEncryptionAtRestManaged() {
		return nil
	}

	selector, err := c.cluster.GetBucketObjectSelector()
	if err != nil {
		log.Error(err, "Failed to get bucket selector, buckets will not wait for encryption keys", "cluster", c.namespacedName())
		return nil
	}

	requestedKeys, _, err := c.gatherRequestedKeys()
	if err != nil {
		log.Error(err, "Failed to gather encryption keys, buckets will not wait for them", "cluster", c.namespacedName())
		return nil
	}

	buckets := []*couchbasev2.CouchbaseBucket{}

	for _, bucket := range c.k8s.CouchbaseBuckets.List() {
		if !selector.Matches(bucket.GetName(), labels.Set(bucket.Labels)) {
			continue
		}

		if requestedKeyFor(bucket, requestedKeys) == nil {
			continue
		}

		buckets = append(buckets, bucket)
	}

	// Saves a REST call on every reconcile when no bucket names a managed key.
	if len(buckets) == 0 {
		return nil
	}

	serverKeys := couchbaseutil.EncryptionKeyList{}
	if err := couchbaseutil.ListEncryptionKeys(&serverKeys).On(c.api, c.readyMembers()); err != nil {
		log.Error(err, "Failed to list encryption keys, buckets will not wait for them", "cluster", c.namespacedName())
		return nil
	}

	waits := map[string]bucketEncryptionWait{}

	for _, bucket := range buckets {
		if wait, ok := encryptionKeyWait(bucket, requestedKeys, serverKeys); ok {
			waits[bucket.GetCouchbaseName()] = wait
		}
	}

	return waits
}

// requestedKeyFor returns the key resource a bucket names, if it is one of the
// requested keys, or nil. Sample buckets are never encrypted, so never wait.
func requestedKeyFor(bucket *couchbasev2.CouchbaseBucket, requestedKeys []*couchbasev2.CouchbaseEncryptionKey) *couchbasev2.CouchbaseEncryptionKey {
	if bucket.Spec.SampleBucket || bucket.Spec.EncryptionAtRest == nil || bucket.Spec.EncryptionAtRest.KeyName == "" {
		return nil
	}

	i := slices.IndexFunc(requestedKeys, func(key *couchbasev2.CouchbaseEncryptionKey) bool {
		return key.Name == bucket.Spec.EncryptionAtRest.KeyName
	})
	if i < 0 {
		return nil
	}

	return requestedKeys[i]
}

// encryptionKeyWait reports whether a bucket has to wait for its key, and why.
// It does not wait when the server's key can already encrypt it, which is the
// only case where gatherCouchbaseBuckets gives the bucket that key.
func encryptionKeyWait(bucket *couchbasev2.CouchbaseBucket, requestedKeys []*couchbasev2.CouchbaseEncryptionKey, serverKeys couchbaseutil.EncryptionKeyList) (bucketEncryptionWait, bool) {
	key := requestedKeyFor(bucket, requestedKeys)
	if key == nil {
		return bucketEncryptionWait{}, false
	}

	name := bucket.GetCouchbaseName()
	usage := key.GetUsage()

	wait := bucketEncryptionWait{
		keyName:        key.Name,
		keyNamesBucket: !usage.AllBuckets && slices.Contains(usage.Buckets, name),
	}

	switch serverKey := serverKeys.GetKeyByName(key.Name); {
	case serverKey == nil:
		wait.reason = "the key is not on the server yet"
	case !serverKey.CanEncryptBucket(name):
		wait.reason = "the key does not permit this bucket on the server yet"
	default:
		return bucketEncryptionWait{}, false
	}

	return wait, true
}

// keepBucketEncryption leaves an existing bucket's encryption at rest settings as
// the server has them, so a bucket waiting for a key is not switched to no key.
// Every other setting is still updated.
func keepBucketEncryption(requested *couchbaseutil.Bucket, actual *couchbaseutil.Bucket) {
	requested.EncryptionAtRestKeyID = actual.EncryptionAtRestKeyID
	requested.EncryptionAtRestDekLifetime = actual.EncryptionAtRestDekLifetime
	requested.EncryptionAtRestDekRotationInterval = actual.EncryptionAtRestDekRotationInterval
}

// raiseBucketEncryptionPending logs and reports that a bucket is held back for
// its encryption key. The event is cached, because it repeats every reconcile
// until the key is ready.
func (c *Cluster) raiseBucketEncryptionPending(bucketName, action string, wait bucketEncryptionWait) {
	log.Info("Bucket waiting for encryption key", "cluster", c.namespacedName(), "bucket", bucketName,
		"key", wait.keyName, "reason", wait.reason, "action", action)

	c.raiseEventCached(k8sutil.BucketEncryptionPendingEvent(c.cluster, bucketName, wait.keyName, action+": "+wait.reason))
}
