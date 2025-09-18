package cluster

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util"
	"github.com/couchbase/couchbase-operator/pkg/util/annotations"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

func (c *Cluster) reconcileEncryptionAtRest() error {
	if c.cluster.Spec.Security.EncryptionAtRest == nil || !c.cluster.Spec.Security.EncryptionAtRest.Managed {
		return nil
	}

	encryptionKeys := &couchbaseutil.EncryptionKeyList{}
	if err := couchbaseutil.ListEncryptionKeys(encryptionKeys).On(c.api, c.readyMembers()); err != nil {
		return err
	}

	if err := c.reconcileConfigEncryptionAtRestSettings(*encryptionKeys); err != nil {
		return err
	}

	if err := c.reconcileAuditEncryptionAtRestSettings(*encryptionKeys); err != nil {
		return err
	}

	if err := c.reconcileLogEncryptionAtRestSettings(*encryptionKeys); err != nil {
		return err
	}

	return nil
}

// reconcileEncryptionAtRestSettings is a generic function that handles the common logic
// for reconciling encryption at rest settings (configuration, audit, and log).
func (c *Cluster) reconcileEncryptionAtRestSettings(
	encryptionKeys couchbaseutil.EncryptionKeyList,
	settings *couchbasev2.EncryptionAtRestUsageConfiguration,
	getCurrentSettings func(*couchbaseutil.EncryptionAtRestSettings) *couchbaseutil.Request,
	updateSettings func(*couchbaseutil.EncryptionAtRestSettings) *couchbaseutil.Request,
	settingsDescription string,
) error {
	// Get current settings
	currSettings := couchbaseutil.EncryptionAtRestSettings{}
	if err := getCurrentSettings(&currSettings).On(c.api, c.readyMembers()); err != nil {
		return err
	}

	// Initialize requested settings with disabled settings as defaults,
	// using current lifetime + rotation interval as defaults
	requestedSettings := couchbaseutil.EncryptionAtRestSettings{
		EncryptionMethod: couchbaseutil.EncryptionMethodDisabled,
		EncryptionKeyID:  nil,

		DEKLifetime:         currSettings.DEKLifetime,
		DEKRotationInterval: currSettings.DEKRotationInterval,
	}

	// Apply settings from spec if enabled
	if settings != nil && settings.Enabled {
		if settings.KeyName == "" {
			requestedSettings.EncryptionMethod = couchbaseutil.EncryptionMethodNodeSecretManager
		} else {
			key := encryptionKeys.GetKeyByName(settings.KeyName)
			if key == nil {
				return errors.ErrEncryptionKeyNotFound
			}

			requestedSettings.EncryptionMethod = couchbaseutil.EncryptionMethodEncryptionKey
			requestedSettings.EncryptionKeyID = &key.ID
		}

		requestedSettings.DEKLifetime = int(settings.KeyLifetime.Seconds())
		requestedSettings.DEKRotationInterval = int(settings.RotationInterval.Seconds())
	}

	// Server will return -1 if the key is not set but won't accept it so we need to set it to nil
	if currSettings.EncryptionKeyID != nil && *currSettings.EncryptionKeyID == -1 {
		currSettings.EncryptionKeyID = nil
	}

	// Update settings if they have changed
	if !reflect.DeepEqual(requestedSettings, currSettings) {
		if err := updateSettings(&requestedSettings).On(c.api, c.readyMembers()); err != nil {
			return err
		}

		log.Info("Encryption settings updated", "cluster", c.namespacedName(), "type", settingsDescription)
		c.raiseEvent(k8sutil.ClusterSettingsEditedEvent(settingsDescription, c.cluster))
	}

	return nil
}

func (c *Cluster) reconcileConfigEncryptionAtRestSettings(encryptionKeys couchbaseutil.EncryptionKeyList) error {
	return c.reconcileEncryptionAtRestSettings(
		encryptionKeys,
		c.cluster.Spec.Security.EncryptionAtRest.Configuration,
		couchbaseutil.GetConfigurationEncryptionSettings,
		couchbaseutil.UpdateConfigurationEncryptionSettings,
		"configuration encryption at rest",
	)
}

func (c *Cluster) reconcileAuditEncryptionAtRestSettings(encryptionKeys couchbaseutil.EncryptionKeyList) error {
	return c.reconcileEncryptionAtRestSettings(
		encryptionKeys,
		c.cluster.Spec.Security.EncryptionAtRest.Audit,
		couchbaseutil.GetAuditEncryptionSettings,
		couchbaseutil.UpdateAuditEncryptionSettings,
		"audit encryption at rest",
	)
}

func (c *Cluster) reconcileLogEncryptionAtRestSettings(encryptionKeys couchbaseutil.EncryptionKeyList) error {
	return c.reconcileEncryptionAtRestSettings(
		encryptionKeys,
		c.cluster.Spec.Security.EncryptionAtRest.Log,
		couchbaseutil.GetLogEncryptionSettings,
		couchbaseutil.UpdateLogEncryptionSettings,
		"log encryption at rest",
	)
}

func (c *Cluster) reconcileEncryptionKeys() error {
	if c.cluster.Spec.Security.EncryptionAtRest == nil || !c.cluster.Spec.Security.EncryptionAtRest.Managed {
		return nil
	}

	// Gather encryption key resources
	requestedKeys, deletedKeys, err := c.gatherRequestedKeys()
	if err != nil {
		return err
	}

	// Delete keys that are in actualKeys but not in requestedKeys also handle removing finalizers from keys that are deleted
	if err := c.deleteRemovedEncryptionKeys(requestedKeys, deletedKeys); err != nil {
		return err
	}

	// Create + update keys that are in the spec but not in the cluster
	if err := c.createAndUpdateEncryptionKeys(requestedKeys); err != nil {
		return err
	}

	return nil
}

// deleteRemovedEncryptionKeys deletes encryption keys that exist in the cluster but are not in the requested keys.
func (c *Cluster) deleteRemovedEncryptionKeys(requestedKeys []*couchbasev2.CouchbaseEncryptionKey, deletedKeys []*couchbasev2.CouchbaseEncryptionKey) error {
	actualKeys := couchbaseutil.EncryptionKeyList{}

	err := couchbaseutil.ListEncryptionKeys(&actualKeys).On(c.api, c.readyMembers())
	if err != nil {
		return err
	}

	// Convert the actual keys to a map for faster lookup + deletion
	actualKeysMap := make(map[string]couchbaseutil.EncryptionKeyInfo)
	for _, k := range actualKeys {
		actualKeysMap[k.Name] = k
	}

	// First handle the deleted keys (keys with finalizers on them + deletionTimestamp set)
	for _, k := range deletedKeys {
		actualKey, keyExists := actualKeysMap[k.Name]

		// If the key is not found then it is already deleted so just remove the finalizer
		if !keyExists {
			c.removeFinalizer(k)
			continue
		}

		// Remove the key from the map so we don't try to delete it again
		delete(actualKeysMap, actualKey.Name)

		// Attempt to delete the key
		if err := couchbaseutil.DeleteEncryptionKey(actualKey.ID).On(c.api, c.readyMembers()); err == nil {
			// Remove the finalizer from the key if deleted successfully
			c.removeFinalizer(k)

			log.Info("Encryption key deleted", "cluster", c.namespacedName(), "name", actualKey.Name)
			c.raiseEvent(k8sutil.EncryptionKeyDeletedEvent(c.cluster, actualKey.Name))
		} // If the key could not be deleted then it is probably still being used somewhere
	}

	for _, actualKey := range actualKeysMap {
		found := false

		for _, k := range requestedKeys {
			if actualKey.Name == k.Name {
				found = true
				break
			}
		}

		if !found {
			// Key exists in cluster but not in spec, so delete it
			if err := couchbaseutil.DeleteEncryptionKey(actualKey.ID).On(c.api, c.readyMembers()); err != nil {
				return err
			}

			log.Info("Encryption key deleted", "cluster", c.namespacedName(), "name", actualKey.Name)
			c.raiseEvent(k8sutil.EncryptionKeyDeletedEvent(c.cluster, actualKey.Name))
		}
	}

	return nil
}

func (c *Cluster) createAndUpdateEncryptionKeys(requestedKeys []*couchbasev2.CouchbaseEncryptionKey) error {
	actualKeys := couchbaseutil.EncryptionKeyList{}
	updateActualKeys := func() error {
		err := couchbaseutil.ListEncryptionKeys(&actualKeys).On(c.api, c.readyMembers())
		if err != nil {
			return err
		}

		return nil
	}

	err := updateActualKeys()
	if err != nil {
		return err
	}

	keyToBucketsMap, err := c.getkeyToBucketsMap()
	if err != nil {
		return err
	}

	for _, requestedKey := range requestedKeys {
		var actualKey *couchbaseutil.EncryptionKeyInfo

		for i := range actualKeys {
			key := actualKeys[i]
			if key.Name == requestedKey.Name {
				actualKey = &key
				break
			}
		}

		apiRequestedKey, err := c.convertEncryptionKey(requestedKey, &actualKeys, updateActualKeys, keyToBucketsMap)
		if err != nil {
			return err
		}

		if actualKey == nil {
			// Key does not exist in cluster, so create it
			if err := couchbaseutil.CreateEncryptionKey(apiRequestedKey).On(c.api, c.readyMembers()); err != nil {
				return err
			}

			log.Info("Encryption key created", "cluster", c.namespacedName(), "name", apiRequestedKey.Name)
			c.raiseEvent(k8sutil.EncryptionKeyCreatedEvent(c.cluster, apiRequestedKey.Name))

			c.applyFinalizer(requestedKey)
		} else {
			if apiRequestedKey.AutoGenerated != nil && apiRequestedKey.AutoGenerated.NextRotationTime == "" && actualKey.EncryptionKey.AutoGenerated != nil {
				apiRequestedKey.AutoGenerated.NextRotationTime = actualKey.EncryptionKey.AutoGenerated.NextRotationTime
			}

			if reflect.DeepEqual(*apiRequestedKey, actualKey.EncryptionKey) {
				continue
			}

			if err := couchbaseutil.UpdateEncryptionKey(apiRequestedKey, actualKey.ID).On(c.api, c.readyMembers()); err != nil {
				return err
			}

			log.Info("Encryption key updated", "cluster", c.namespacedName(), "name", apiRequestedKey.Name)
			c.raiseEvent(k8sutil.EncryptionKeyUpdatedEvent(c.cluster, apiRequestedKey.Name))
		}
	}

	return nil
}

func (c *Cluster) applyFinalizer(key *couchbasev2.CouchbaseEncryptionKey) {
	if key == nil {
		return
	}

	if key.HasClusterFinalizer(c.cluster) {
		return
	}

	key.Finalizers = append(key.Finalizers, c.GetEncryptionKeyFinalizer())

	var err error
	if key, err = c.k8s.CouchbaseClient.CouchbaseV2().CouchbaseEncryptionKeys(c.cluster.Namespace).Update(context.Background(), key, metav1.UpdateOptions{}); err != nil {
		// Not worth stopping the reconciliation for this
		log.Info("[WARN] failed to apply finalizer to encryption key", "cluster", c.namespacedName(), "name", key.Name, "error", err)
	}
}

func (c *Cluster) removeFinalizer(key *couchbasev2.CouchbaseEncryptionKey) {
	if !key.HasClusterFinalizer(c.cluster) {
		return
	}

	key.Finalizers = slices.DeleteFunc(key.Finalizers, func(f string) bool {
		return f == c.GetEncryptionKeyFinalizer()
	})

	var err error
	if key, err = c.k8s.CouchbaseClient.CouchbaseV2().CouchbaseEncryptionKeys(c.cluster.Namespace).Update(context.Background(), key, metav1.UpdateOptions{}); err != nil {
		// Not worth stopping the reconciliation for this
		log.Info("[WARN] failed to remove finalizer from encryption key", "cluster", c.namespacedName(), "name", key.Name, "error", err)
	}
}

func (c *Cluster) gatherRequestedKeys() ([]*couchbasev2.CouchbaseEncryptionKey, []*couchbasev2.CouchbaseEncryptionKey, error) {
	selector := labels.Everything()

	deletedKeys := []*couchbasev2.CouchbaseEncryptionKey{}

	if c.cluster.Spec.Security.EncryptionAtRest.Selector != nil {
		var err error
		selector, err = metav1.LabelSelectorAsSelector(c.cluster.Spec.Security.EncryptionAtRest.Selector)

		if err != nil {
			return nil, nil, err
		}
	}

	allKeys := c.k8s.CouchbaseEncryptionKeys.List()
	keys := []*couchbasev2.CouchbaseEncryptionKey{}

	for _, key := range allKeys {
		if !selector.Matches(labels.Set(key.Labels)) {
			continue
		}

		if key.DeletionTimestamp != nil {
			deletedKeys = append(deletedKeys, key)
			continue
		}

		keys = append(keys, key)
	}

	orderedKeys := orderKeysForCreation(keys)

	return orderedKeys, deletedKeys, nil
}

// orderKeysForCreation orders the keys for creation in the correct order.
// This is needed because some keys are dependent on others due to the fact that they can be encrypted with other keys.
// For example, if key A is encrypted with key B, key B must be created first.
func orderKeysForCreation(keys []*couchbasev2.CouchbaseEncryptionKey) []*couchbasev2.CouchbaseEncryptionKey {
	keyMap := make(map[string]*couchbasev2.CouchbaseEncryptionKey)

	for _, key := range keys {
		keyMap[key.Name] = key
	}

	orderedKeys := make([]*couchbasev2.CouchbaseEncryptionKey, 0, len(keys))

	getAnyKeyFromMapOrNil := func() *couchbasev2.CouchbaseEncryptionKey {
		for _, k := range keyMap {
			return k
		}

		return nil
	}

	key := getAnyKeyFromMapOrNil()

	// Start with the first key
	keyChain := []*couchbasev2.CouchbaseEncryptionKey{}
	for key != nil {
		keyChain = append(keyChain, key)

		// If the key is encrypted with another key then follow the chain down and add that "root" key to the list (The key that is not encrypted with another key)
		if key.Spec.KeyType == couchbasev2.CouchbaseEncryptionKeyTypeAutoGenerated && key.Spec.AutoGenerated != nil && key.Spec.AutoGenerated.EncryptWithKey != "" {
			k := keyMap[key.Spec.AutoGenerated.EncryptWithKey]
			if k != nil {
				key = k
				continue
			}
		}

		// Add keys in reverse order (from root key to dependent keys)
		for i := len(keyChain) - 1; i >= 0; i-- {
			orderedKeys = append(orderedKeys, keyChain[i])
			delete(keyMap, keyChain[i].Name)
		}

		keyChain = keyChain[:0] // Empty the key chain
		key = getAnyKeyFromMapOrNil()
	}

	return orderedKeys
}

func (c *Cluster) convertEncryptionKey(key *couchbasev2.CouchbaseEncryptionKey, currentKeys *couchbaseutil.EncryptionKeyList, updateActualKeys func() error, keyToBucketsMap map[string][]string) (*couchbaseutil.EncryptionKey, error) {
	encryptionKey := &couchbaseutil.EncryptionKey{
		Name:  key.Name,
		Usage: c.getUsageList(key, keyToBucketsMap),
	}

	configureAutoGeneratedKey := func(encryptionKey *couchbaseutil.EncryptionKey) error {
		encryptionKey.Type = couchbaseutil.EncryptionKeyTypeCBServerManagedAES256

		// Use the same defaults as server
		encryptionKey.AutoGenerated = &couchbaseutil.AutoGeneratedKeyData{
			CanBeCached:      true,
			EncryptWith:      couchbaseutil.EncryptionKeyEncryptWithNodeSecretManager,
			EncryptWithKeyID: util.IntPtr(-1),
		}

		if key.Spec.AutoGenerated == nil {
			return nil
		}

		encryptionKey.AutoGenerated.CanBeCached = key.Spec.AutoGenerated.CanBeCached

		if key.Spec.AutoGenerated.Rotation == nil {
			encryptionKey.AutoGenerated.AutoRotation = false
		} else {
			encryptionKey.AutoGenerated.AutoRotation = true
			encryptionKey.AutoGenerated.RotationIntervalInDays = key.Spec.AutoGenerated.Rotation.IntervalDays
			if key.Spec.AutoGenerated.Rotation.StartTime != nil && key.Spec.AutoGenerated.Rotation.StartTime.Time.After(time.Now()) {
				encryptionKey.AutoGenerated.NextRotationTime = key.Spec.AutoGenerated.Rotation.StartTime.Format(time.RFC3339)
			}
		}

		if key.Spec.AutoGenerated.EncryptWithKey != "" {
			encryptWithKey := currentKeys.GetKeyByName(key.Spec.AutoGenerated.EncryptWithKey)
			if encryptWithKey == nil {
				// If the key is not found, update the actual keys to get the latest state and try again
				err := updateActualKeys()
				if err != nil {
					return err
				}

				encryptWithKey = currentKeys.GetKeyByName(key.Spec.AutoGenerated.EncryptWithKey)
			}

			// If the key is still not found, return an error
			if encryptWithKey == nil {
				log.Error(errors.ErrEncryptionKeyNotFound, "cluster", c.namespacedName(), "key-name", key.Spec.AutoGenerated.EncryptWithKey)
				return errors.ErrEncryptionKeyNotFound
			}

			encryptionKey.AutoGenerated.EncryptWithKeyID = &encryptWithKey.ID
			encryptionKey.AutoGenerated.EncryptWith = couchbaseutil.EncryptionKeyEncryptWithEncryptionKey
		}

		return nil
	}

	switch key.Spec.KeyType {
	case couchbasev2.CouchbaseEncryptionKeyTypeAutoGenerated:
		err := configureAutoGeneratedKey(encryptionKey)
		if err != nil {
			return nil, err
		}
	default:
		return nil, errors.ErrUnsupportedEncryptionKeyType
	}

	return encryptionKey, nil
}

func (c *Cluster) getUsageList(key *couchbasev2.CouchbaseEncryptionKey, keyToBucketsMap map[string][]string) []string {
	usage := key.GetUsage()

	usageList := make([]string, 0)

	if usage.AllBuckets {
		usageList = append(usageList, couchbaseutil.EncryptionKeyUsageBucketEncryptionAll)
	} else if usage.ManagedBucketSelection && c.cluster.Spec.Buckets.Managed {
		for _, bucketName := range keyToBucketsMap[key.Name] {
			usageList = append(usageList, strings.Join([]string{constants.EncryptionKeyUsageBucketEncryptionPrefix, bucketName}, "-"))
		}
	}

	if usage.Configuration {
		usageList = append(usageList, couchbaseutil.EncryptionKeyUsageConfigEncryption)
	}

	if usage.Key {
		usageList = append(usageList, couchbaseutil.EncryptionKeyUsageKEKEncryption)
	}

	if usage.Log {
		usageList = append(usageList, couchbaseutil.EncryptionKeyUsageLogsEncryption)
	}

	if usage.Audit {
		usageList = append(usageList, couchbaseutil.EncryptionKeyUsageAuditEncryption)
	}

	return usageList
}

// getkeyToBucketsMap gets a map of key names to the buckets that use it for encryption at rest.
func (c *Cluster) getkeyToBucketsMap() (map[string][]string, error) {
	couchbaseBuckets := c.k8s.CouchbaseBuckets.List()

	// Get all the K8s buckets
	bucketSelector, err := c.cluster.GetBucketLabelSelector()
	if err != nil {
		return nil, err
	}

	keyToBucketsMap := make(map[string][]string)

	for _, bucket := range couchbaseBuckets {
		if c.k8s != nil {
			bucketA, found := c.k8s.CouchbaseBuckets.Get(bucket.Name)
			if found && !couchbaseutil.ShouldReconcile(bucketA.Annotations) {
				continue
			}
		}

		err := annotations.Populate(&bucket.Spec, bucket.Annotations)
		if err != nil {
			// we failed but its not worth stopping. log the error and continue
			log.Error(err, "failed to populate bucket with annotation")
		}

		if !bucketSelector.Matches(labels.Set(bucket.Labels)) {
			continue
		}

		if bucket.Spec.EncryptionAtRest != nil && bucket.Spec.EncryptionAtRest.KeyName != "" {
			keyToBucketsMap[bucket.Spec.EncryptionAtRest.KeyName] = append(keyToBucketsMap[bucket.Spec.EncryptionAtRest.KeyName], bucket.GetCouchbaseName())
		}
	}

	return keyToBucketsMap, nil
}
