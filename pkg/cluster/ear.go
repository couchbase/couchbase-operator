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
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	corev1 "k8s.io/api/core/v1"
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

	for _, requestedKey := range requestedKeys {
		var actualKey *couchbaseutil.EncryptionKeyInfo

		for i := range actualKeys {
			key := actualKeys[i]
			if key.Name == requestedKey.Name {
				actualKey = &key
				break
			}
		}

		apiRequestedKey, err := c.convertEncryptionKey(requestedKey, &actualKeys, updateActualKeys)
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

			// Server doesn't return the ACTUAL key passphrase (security and all that), so copy it to ignore it during the comparison
			if apiRequestedKey.KMIP != nil && actualKey.EncryptionKey.KMIP != nil {
				actualKey.EncryptionKey.KMIP.KeyPassphrase = apiRequestedKey.KMIP.KeyPassphrase
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
	if !c.IsEncryptionAtRestManaged() {
		return nil, nil, nil
	}

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

func (c *Cluster) configureAutoGeneratedKey(encryptionKey *couchbaseutil.EncryptionKey, requestedKey *couchbasev2.CouchbaseEncryptionKey, currentKeys *couchbaseutil.EncryptionKeyList, updateActualKeys func() error) error {
	encryptionKey.Type = couchbaseutil.EncryptionKeyTypeCBServerManagedAES256

	// Use the same defaults as server
	encryptionKey.AutoGenerated = &couchbaseutil.AutoGeneratedKeyData{
		CanBeCached:      true,
		EncryptWith:      couchbaseutil.EncryptionKeyEncryptWithNodeSecretManager,
		EncryptWithKeyID: util.IntPtr(-1),
	}

	if requestedKey.Spec.AutoGenerated == nil {
		return nil
	}

	encryptionKey.AutoGenerated.CanBeCached = requestedKey.Spec.AutoGenerated.CanBeCached

	if requestedKey.Spec.AutoGenerated.Rotation == nil {
		encryptionKey.AutoGenerated.AutoRotation = false
	} else {
		encryptionKey.AutoGenerated.AutoRotation = true
		encryptionKey.AutoGenerated.RotationIntervalInDays = requestedKey.Spec.AutoGenerated.Rotation.IntervalDays
		if requestedKey.Spec.AutoGenerated.Rotation.StartTime != nil && requestedKey.Spec.AutoGenerated.Rotation.StartTime.Time.After(time.Now()) {
			encryptionKey.AutoGenerated.NextRotationTime = requestedKey.Spec.AutoGenerated.Rotation.StartTime.Format(time.RFC3339)
		} else {
			// If the start time is not set (it should be as it's validated by the DAC) or if its before the time now then we will
			// add the interval days to time now and .
			days := encryptionKey.AutoGenerated.RotationIntervalInDays

			encryptionKey.AutoGenerated.NextRotationTime = time.Now().AddDate(0, 0, days).Format(time.RFC3339)
		}
	}

	if requestedKey.Spec.AutoGenerated.EncryptWithKey != "" {
		encryptWithKey := currentKeys.GetKeyByName(requestedKey.Spec.AutoGenerated.EncryptWithKey)
		if encryptWithKey == nil {
			// If the key is not found, update the actual keys to get the latest state and try again
			err := updateActualKeys()
			if err != nil {
				return err
			}

			encryptWithKey = currentKeys.GetKeyByName(requestedKey.Spec.AutoGenerated.EncryptWithKey)
		}

		// If the key is still not found, return an error
		if encryptWithKey == nil {
			log.Error(errors.ErrEncryptionKeyNotFound, "cluster", c.namespacedName(), "key-name", requestedKey.Spec.AutoGenerated.EncryptWithKey)
			return errors.ErrEncryptionKeyNotFound
		}

		encryptionKey.AutoGenerated.EncryptWithKeyID = &encryptWithKey.ID
		encryptionKey.AutoGenerated.EncryptWith = couchbaseutil.EncryptionKeyEncryptWithEncryptionKey
	}

	return nil
}

func (c *Cluster) configureAWSKey(encryptionKey *couchbaseutil.EncryptionKey, requestedKey *couchbasev2.CouchbaseEncryptionKey) {
	encryptionKey.Type = couchbaseutil.EncryptionKeyTypeAWSKMSSymmetric
	encryptionKey.AWS = &couchbaseutil.AWSKeyData{
		KeyARN:  requestedKey.Spec.AWSKey.KeyARN,
		Region:  requestedKey.Spec.AWSKey.KeyRegion,
		UseIMDS: requestedKey.Spec.AWSKey.UseIMDS,
		Profile: requestedKey.Spec.AWSKey.ProfileName,
	}

	if requestedKey.Spec.AWSKey.CredentialsSecret != "" {
		fileName := strings.Join([]string{requestedKey.Spec.AWSKey.CredentialsSecret, constants.AWSCredentialsSecretKey}, "-")
		filePath := getShadowSecretFilePath(fileName)
		encryptionKey.AWS.CredentialsFile = filePath
	}
}

func (c *Cluster) configureKMIPKey(encryptionKey *couchbaseutil.EncryptionKey, requestedKey *couchbasev2.CouchbaseEncryptionKey) error {
	encryptionKey.Type = couchbaseutil.EncryptionKeyTypeKMIPAES256
	encryptionKey.KMIP = &couchbaseutil.KMIPKeyData{
		Host:         requestedKey.Spec.KMIPKey.Host,
		Port:         requestedKey.Spec.KMIPKey.Port,
		ReqTimeoutMs: requestedKey.Spec.KMIPKey.TimeoutInMs,
		ActiveKey: couchbaseutil.KMIPActiveKey{
			KMIPID: requestedKey.Spec.KMIPKey.KeyID,
		},
	}

	switch {
	case requestedKey.Spec.KMIPKey.VerifyWithSystemCA && requestedKey.Spec.KMIPKey.VerifyWithCouchbaseCA:
		encryptionKey.KMIP.CASelection = couchbaseutil.CASelectionBoth
	case requestedKey.Spec.KMIPKey.VerifyWithSystemCA:
		encryptionKey.KMIP.CASelection = couchbaseutil.CASelectionSystem
	case requestedKey.Spec.KMIPKey.VerifyWithCouchbaseCA:
		encryptionKey.KMIP.CASelection = couchbaseutil.CASelectionCouchbase
	default:
		encryptionKey.KMIP.CASelection = couchbaseutil.CASelectionSkip
	}

	// Default to local encrypt
	if requestedKey.Spec.KMIPKey.EncryptionApproach == couchbasev2.CouchbaseEncryptionApproachNativeEncryptDecrypt {
		encryptionKey.KMIP.EncryptionApproach = couchbaseutil.EncryptionApproachNativeEncryptDecrypt
	} else {
		encryptionKey.KMIP.EncryptionApproach = couchbaseutil.EncryptionApproachLocalEncrypt
	}

	secret, found := c.k8s.Secrets.Get(requestedKey.Spec.KMIPKey.ClientSecret)
	if !found {
		log.Error(errors.ErrResourceRequired, "secret not found", "secretName", requestedKey.Spec.KMIPKey.ClientSecret)
		return errors.ErrResourceRequired
	}

	keyFileName := getShadowSecretKMIPKeyFileName(requestedKey.Spec.KMIPKey.ClientSecret)
	certFileName := getShadowSecretKMIPCertFileName(requestedKey.Spec.KMIPKey.ClientSecret)
	encryptionKey.KMIP.KeyPath = getShadowSecretFilePath(keyFileName)
	encryptionKey.KMIP.CertPath = getShadowSecretFilePath(certFileName)

	passphraseData, ok := secret.Data[constants.KMIPClientSecretPassphraseKey]

	if !ok {
		log.Error(errors.ErrResourceAttributeRequired, "secret missing passphrase data key", "secretName", requestedKey.Spec.KMIPKey.ClientSecret)
		return errors.ErrResourceAttributeRequired
	}

	encryptionKey.KMIP.KeyPassphrase = string(passphraseData)

	return nil
}
func (c *Cluster) convertEncryptionKey(key *couchbasev2.CouchbaseEncryptionKey, currentKeys *couchbaseutil.EncryptionKeyList, updateActualKeys func() error) (*couchbaseutil.EncryptionKey, error) {
	encryptionKey := &couchbaseutil.EncryptionKey{
		Name:  key.Name,
		Usage: c.getUsageList(key),
	}

	switch key.Spec.KeyType {
	case couchbasev2.CouchbaseEncryptionKeyTypeAutoGenerated:
		err := c.configureAutoGeneratedKey(encryptionKey, key, currentKeys, updateActualKeys)
		if err != nil {
			return nil, err
		}
	case couchbasev2.CouchbaseEncryptionKeyTypeAWS:
		c.configureAWSKey(encryptionKey, key)
	case couchbasev2.CouchbaseEncryptionKeyTypeKMIP:
		err := c.configureKMIPKey(encryptionKey, key)
		if err != nil {
			return nil, err
		}
	default:
		return nil, errors.ErrUnsupportedEncryptionKeyType
	}

	return encryptionKey, nil
}

func (c *Cluster) getUsageList(key *couchbasev2.CouchbaseEncryptionKey) []string {
	usage := key.GetUsage()

	usageList := make([]string, 0)

	if usage.AllBuckets {
		usageList = append(usageList, couchbaseutil.EncryptionKeyUsageBucketEncryptionAll)
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

func (c *Cluster) refreshKeyShadowSecret() error {
	// If the cluster spec is at least 8.0, refresh the shadow secrets as during upgrades to 8.0, we want to create the
	// shadow secrets for the new version so we can mount them while upgrading the pods.
	earSupported, err := c.cluster.IsAtLeastVersion("8.0.0")
	if err != nil || !earSupported {
		return err
	}

	name := k8sutil.KeyShadowSecretName(c.cluster)

	requestedShadowSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: k8sutil.LabelsForCluster(c.cluster),
			OwnerReferences: []metav1.OwnerReference{
				c.cluster.AsOwner(),
			},
		},
		Data: map[string][]byte{},
	}

	keys, _, err := c.gatherRequestedKeys()
	if err != nil {
		return err
	}

	credentialsSecretsToAdd := map[string]bool{}
	kmipSecretsToAdd := map[string]bool{}

	for _, key := range keys {
		if key.Spec.KeyType == couchbasev2.CouchbaseEncryptionKeyTypeAWS && key.Spec.AWSKey != nil && key.Spec.AWSKey.CredentialsSecret != "" {
			credentialsSecretsToAdd[key.Spec.AWSKey.CredentialsSecret] = true
		}

		if key.Spec.KeyType == couchbasev2.CouchbaseEncryptionKeyTypeKMIP && key.Spec.KMIPKey != nil && key.Spec.KMIPKey.ClientSecret != "" {
			kmipSecretsToAdd[key.Spec.KMIPKey.ClientSecret] = true
		}
	}

	for secretName := range credentialsSecretsToAdd {
		secret, ok := c.k8s.Secrets.Get(secretName)
		if !ok {
			log.Error(errors.ErrResourceRequired, "secret not found", "secretName", secret)
			return errors.ErrResourceRequired
		}

		if secret.Data[constants.AWSCredentialsSecretKey] == nil {
			log.Error(errors.ErrResourceAttributeRequired, "secret missing credentials data key", "secretName", secret)
			return errors.ErrResourceAttributeRequired
		}

		requestedShadowSecret.Data[strings.Join([]string{secret.Name, constants.AWSCredentialsSecretKey}, "-")] = secret.Data[constants.AWSCredentialsSecretKey]
	}

	for secretName := range kmipSecretsToAdd {
		secret, ok := c.k8s.Secrets.Get(secretName)
		if !ok {
			log.Error(errors.ErrResourceRequired, "secret not found", "secretName", secret)
			return errors.ErrResourceRequired
		}

		if secret.Data[constants.KMIPClientSecretKeyKey] == nil {
			log.Error(errors.ErrResourceAttributeRequired, "secret missing key data key", "secretName", secret)
			return errors.ErrResourceAttributeRequired
		}

		if secret.Data[constants.KMIPClientSecretCertKey] == nil {
			log.Error(errors.ErrResourceAttributeRequired, "secret missing cert data key", "secretName", secret)
			return errors.ErrResourceAttributeRequired
		}

		requestedShadowSecret.Data[getShadowSecretKMIPKeyFileName(secret.Name)] = secret.Data[constants.KMIPClientSecretKeyKey]
		requestedShadowSecret.Data[getShadowSecretKMIPCertFileName(secret.Name)] = secret.Data[constants.KMIPClientSecretCertKey]
	}

	// If the secret doesn't exist, create it
	currentSecret, ok := c.k8s.Secrets.Get(requestedShadowSecret.Name)
	if !ok {
		log.Info("Creating Key Shadow secret", "secretName", requestedShadowSecret.Name, "cluster", c.namespacedName())

		if _, err := c.k8s.KubeClient.CoreV1().Secrets(c.cluster.Namespace).Create(context.Background(), requestedShadowSecret, metav1.CreateOptions{}); err != nil {
			return errors.NewStackTracedError(err)
		}

		return nil
	}

	// There is a difference between empty and nil in Go...
	if len(requestedShadowSecret.Data) == 0 && len(currentSecret.Data) == 0 {
		return nil
	}

	if reflect.DeepEqual(requestedShadowSecret.Data, currentSecret.Data) {
		return nil
	}

	log.Info("Updating Key Shadow secret", "secretName", requestedShadowSecret.Name, "cluster", c.namespacedName())

	updatedSecret := currentSecret.DeepCopy()
	updatedSecret.Data = requestedShadowSecret.Data

	if _, err := c.k8s.KubeClient.CoreV1().Secrets(c.cluster.Namespace).Update(context.Background(), updatedSecret, metav1.UpdateOptions{}); err != nil {
		return errors.NewStackTracedError(err)
	}

	return nil
}

func (c *Cluster) needsKeyShadowSecret() (bool, error) {
	if !c.IsEncryptionAtRestManaged() {
		return false, nil
	}

	requestedKeys, _, err := c.gatherRequestedKeys()
	if err != nil {
		return false, err
	}

	// Check for any keys that need a key shadow secret
	for _, key := range requestedKeys {
		if key.Spec.KeyType == couchbasev2.CouchbaseEncryptionKeyTypeAWS && key.Spec.AWSKey != nil && key.Spec.AWSKey.CredentialsSecret != "" {
			return true, nil
		}

		if key.Spec.KeyType == couchbasev2.CouchbaseEncryptionKeyTypeKMIP && key.Spec.KMIPKey != nil && key.Spec.KMIPKey.ClientSecret != "" {
			return true, nil
		}
	}

	return false, nil
}

func getShadowSecretKMIPKeyFileName(secretName string) string {
	return strings.Join([]string{secretName, constants.KMIPClientSecretKeyKey}, "-")
}

func getShadowSecretKMIPCertFileName(secretName string) string {
	return strings.Join([]string{secretName, constants.KMIPClientSecretCertKey}, "-")
}

func getShadowSecretFilePath(fileName string) string {
	return strings.Join([]string{k8sutil.CouchbaseKeyShadowVolumeMountDir, fileName}, "/")
}
