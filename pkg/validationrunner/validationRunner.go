package validationrunner

import (
	ctx "context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/pkg/admission"
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/client"
	"github.com/couchbase/couchbase-operator/pkg/cluster"
	"github.com/couchbase/couchbase-operator/pkg/conversion"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/validator"
	"github.com/couchbase/couchbase-operator/pkg/validator/types"
	"github.com/couchbase/couchbase-operator/pkg/validator/util"
	validationv2 "github.com/couchbase/couchbase-operator/pkg/validator/v2"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type bucketNamer struct {
	// c is a cluster reference so we have access to the cluster name to uniquely
	// name resources based on cluster (as we may have multiple in the same namespace)
	c *cluster.Cluster
}

var (
	options = types.ValidatorOptions{
		ValidateSecrets:        true,
		ValidateStorageClasses: true,
		DefaultFileSystemGroup: true,
	}

	v = validator.New(admission.GetClient(), admission.GetCouchbaseClient(), &options)

	errResourceNotFound = errors.New("resource not found in cache")

	skipDacAnnotationFalse = map[string]string{constants.AnnotationDisableAdmissionController: "false"}
)

// generateSuffix generates a unique fixed length, and DNS compatible, suffix.
func (n *bucketNamer) generateSuffix(input string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(input)))
}

// generateBucketSuffix generates a unique fixed length, and DNS compatible, bucket suffix
// based on cluster and bucket name.
func (n *bucketNamer) generateBucketSuffix(bucket *couchbaseutil.Bucket) string {
	input := fmt.Sprintf("%s-%s", n.c.GetCouchbaseCluster().Name, bucket.BucketName)

	return n.generateSuffix(input)
}

// GenerateBucketName generates a unique, but deterministic, bucket name.
func (n *bucketNamer) GenerateBucketName(bucket *couchbaseutil.Bucket) string {
	return fmt.Sprintf("bucket-%s", n.generateBucketSuffix(bucket))
}

// GenerateEphemeralBucketName generates a unique, but deterministic, bucket name.
func (n *bucketNamer) GenerateEphemeralBucketName(bucket *couchbaseutil.Bucket) string {
	return fmt.Sprintf("ephemeralbucket-%s", n.generateBucketSuffix(bucket))
}

// GenerateMemcachedBucketName generates a unique, but deterministic, bucket name.
func (n *bucketNamer) GenerateMemcachedBucketName(bucket *couchbaseutil.Bucket) string {
	return fmt.Sprintf("memcachedbucket-%s", n.generateBucketSuffix(bucket))
}

// GenerateScopeName generates a unique, but deterministic, scope name.
func (n *bucketNamer) GenerateScopeName(bucket *couchbaseutil.Bucket, scope *couchbaseutil.Scope) string {
	input := fmt.Sprintf("%s-%s-%s", n.c.GetCouchbaseCluster().Name, bucket.BucketName, scope.Name)

	return fmt.Sprintf("scope-%s", n.generateSuffix(input))
}

// GenerateCollectionName generates a unique, but deterministic, collection name.
func (n *bucketNamer) GenerateCollectionName(bucket *couchbaseutil.Bucket, scope *couchbaseutil.Scope, collection *couchbaseutil.Collection) string {
	input := fmt.Sprintf("%s-%s-%s-%s", n.c.GetCouchbaseCluster().Name, bucket.BucketName, scope.Name, collection.Name)

	return fmt.Sprintf("collection-%s", n.generateSuffix(input))
}

func ValidateImmutableFields(currentCluster *cluster.Cluster) []error {
	var errs []error

	errs = append(errs, validateBucketsImmutableFields(currentCluster)...)
	errs = append(errs, validateReplicationsImmutableFields(currentCluster)...)
	errs = append(errs, validateBackupsImmutableFields(currentCluster)...)
	errs = append(errs, validateAutoscalersImmutableField(currentCluster)...)
	errs = append(errs, validateCollectionGroupsImmutableFields(currentCluster)...)

	return errs
}

func validateCollectionGroupsImmutableFields(currentCluster *cluster.Cluster) []error {
	var errs []error

	updates, err := currentCluster.GatherCollectionGroupUpdates()
	if err != nil {
		return append(errs, err)
	}

	for current, update := range updates {
		if err := validationv2.CheckImmutableFieldsCollectionGroup(current, update); err != nil {
			update.SetAnnotations(map[string]string{constants.AnnotationUnreconcilable: "true"})

			if updateErr := currentCluster.GetK8sClient().CouchbaseCollectionGroups.Update(update); updateErr != nil {
				errs = append(errs, updateErr)
			}

			errs = append(errs, err)
		}
	}

	return errs
}

func validateAutoscalersImmutableField(currentCluster *cluster.Cluster) []error {
	var errs []error

	autoscalerUpdates, err := currentCluster.GatherAutoscalerUpdates()
	if err != nil {
		return append(errs, err)
	}

	for current, update := range autoscalerUpdates {
		if err := validationv2.CheckImmutableFieldsAutoscaler(current, update); err != nil {
			update.SetAnnotations(map[string]string{constants.AnnotationUnreconcilable: "true"})

			if updateErr := currentCluster.GetK8sClient().CouchbaseAutoscalers.Update(update); updateErr != nil {
				errs = append(errs, updateErr)
			}

			errs = append(errs, err)
		}
	}

	return errs
}

func validateBackupsImmutableFields(currentCluster *cluster.Cluster) []error {
	var errs []error

	backupUpdates, err := currentCluster.GatherBackupUpdates()
	if err != nil {
		return append(errs, err)
	}

	for actual, update := range backupUpdates {
		if err := validationv2.CheckImmutableFieldsBackup(actual, update); err != nil {
			update.SetAnnotations(map[string]string{constants.AnnotationUnreconcilable: "true"})

			if updateErr := currentCluster.GetK8sClient().CouchbaseBackups.Update(update); updateErr != nil {
				errs = append(errs, updateErr)
			}

			errs = append(errs, err)
		}
	}

	return errs
}

func validateReplicationsImmutableFields(currentCluster *cluster.Cluster) []error {
	var errs []error

	replicationChanges, err := currentCluster.GatherReplicationChanges()
	if err != nil {
		return append(errs, err)
	}

	for actual, update := range replicationChanges {
		if actual.FromBucket != update.FromBucket {
			errs = append(errs, util.NewUpdateError("spec.bucket", "body"))
		}

		if actual.ToBucket != update.ToBucket {
			errs = append(errs, util.NewUpdateError("spec.remoteBucket", "body"))
		}

		if actual.FilterExpression != update.FilterExpression {
			errs = append(errs, util.NewUpdateError("spec.filterExpression", "body"))
		}
	}

	return errs
}

func validateBucketsImmutableFields(currentCluster *cluster.Cluster) []error {
	var errs []error

	updateBuckets, err := currentCluster.GetBucketsToUpdate()
	if err != nil {
		return append(errs, err)
	}

	namer := &bucketNamer{
		c: currentCluster,
	}

	for actual, update := range updateBuckets {
		oldBucket, err := conversion.ConvertAbstractBucketToAPIBucket(&actual, namer)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		newBucket, err := conversion.ConvertAbstractBucketToAPIBucket(&update, namer)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		if err := validator.CheckImmutableFields(oldBucket, newBucket); err != nil {
			switch t := newBucket.(type) {
			case *couchbasev2.CouchbaseBucket:
				t.SetAnnotations(map[string]string{constants.AnnotationUnreconcilable: "true"})

				if updateErr := currentCluster.GetK8sClient().CouchbaseBuckets.Update(t); updateErr != nil {
					errs = append(errs, updateErr)
				}
			case *couchbasev2.CouchbaseEphemeralBucket:
				t.SetAnnotations(map[string]string{constants.AnnotationUnreconcilable: "true"})

				_, updateErr := currentCluster.GetK8sClient().CouchbaseClient.CouchbaseV2().CouchbaseEphemeralBuckets(currentCluster.GetCouchbaseCluster().Namespace).Update(ctx.Background(), t, metav1.UpdateOptions{})
				if updateErr != nil {
					errs = append(errs, updateErr)
				}
			case *couchbasev2.CouchbaseMemcachedBucket:
				t.SetAnnotations(map[string]string{constants.AnnotationUnreconcilable: "true"})

				_, updateErr := currentCluster.GetK8sClient().CouchbaseClient.CouchbaseV2().CouchbaseMemcachedBuckets(currentCluster.GetCouchbaseCluster().Namespace).Update(ctx.Background(), t, metav1.UpdateOptions{})
				if updateErr != nil {
					errs = append(errs, updateErr)
				}
			}
			currentCluster.GetK8sClient().CouchbaseClient.CouchbaseV2().CouchbaseBuckets(currentCluster.GetCouchbaseCluster().Namespace)

			errs = append(errs, err)
		}
	}

	return errs
}

func CheckChangeConstraints(currentCluster *cluster.Cluster) []error {
	var errs []error

	errs = append(errs, validateBucketsChangeConstraints(currentCluster)...)

	return errs
}

func validateBucketsChangeConstraints(currentCluster *cluster.Cluster) []error {
	var errs []error

	namer := &bucketNamer{
		c: currentCluster,
	}

	updateBuckets, err := currentCluster.GetBucketsToUpdate()
	if err != nil {
		return append(errs, err)
	}

	for actual, update := range updateBuckets {
		oldBucket, err := conversion.ConvertAbstractBucketToAPIBucket(&actual, namer)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		newBucket, err := conversion.ConvertAbstractBucketToAPIBucket(&update, namer)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		if err := validator.CheckChangeConstraints(v, oldBucket, newBucket); err != nil {
			cbBucket, found := currentCluster.GetK8sClient().CouchbaseBuckets.Get(update.BucketName)
			if !found {
				errs = append(errs, errResourceNotFound)
			}

			cbBucket.SetAnnotations(map[string]string{constants.AnnotationUnreconcilable: "true"})

			if updateErr := currentCluster.GetK8sClient().CouchbaseBuckets.Update(cbBucket); updateErr != nil {
				errs = append(errs, err)
			}

			errs = append(errs, err)
		}
	}

	return errs
}

func CheckConstraints(k8s *client.Client) []error {
	var errs []error

	errs = append(errs, validateBuckets(k8s)...)
	errs = append(errs, validateCouchbaseUsers(k8s)...)
	errs = append(errs, validateCouchbaseGroupsConstraints(k8s)...)
	errs = append(errs, validateCouchbaseBackupsConstraints(k8s)...)
	errs = append(errs, validateBackupRestores(k8s)...)
	errs = append(errs, validateCollections(k8s)...)
	errs = append(errs, validateCollectionGroups(k8s)...)
	errs = append(errs, validateScopes(k8s)...)
	errs = append(errs, validateScopeGroups(k8s)...)

	return errs
}

func validateScopeGroups(client *client.Client) []error {
	var errs []error

	for _, scopeGroup := range client.CouchbaseScopeGroups.List() {
		if shouldSkipValidation(scopeGroup.Annotations) {
			continue
		}

		scopeGroup.SetAnnotations(skipDacAnnotationFalse)

		if err := checkCouchbaseScopeGroupResourceConstraints(scopeGroup); err != nil {
			scopeGroup.SetAnnotations(map[string]string{constants.AnnotationUnreconcilable: "true"})

			if updateErr := client.CouchbaseScopeGroups.Update(scopeGroup); updateErr != nil {
				errs = append(errs, updateErr)
			}

			errs = append(errs, err)
		}
	}

	return errs
}

func validateScopes(client *client.Client) []error {
	var errs []error

	for _, scope := range client.CouchbaseScopes.List() {
		if shouldSkipValidation(scope.Annotations) {
			continue
		}

		scope.SetAnnotations(skipDacAnnotationFalse)

		if err := checkCouchbaseScopeResourceConstraints(scope); err != nil {
			scope.SetAnnotations(map[string]string{constants.AnnotationUnreconcilable: "true"})

			if updateErr := client.CouchbaseScopes.Update(scope); updateErr != nil {
				errs = append(errs, updateErr)
			}

			errs = append(errs, err)
		}
	}

	return errs
}

func validateCollectionGroups(client *client.Client) []error {
	var errs []error

	for _, collectionGroup := range client.CouchbaseCollectionGroups.List() {
		if shouldSkipValidation(collectionGroup.Annotations) {
			continue
		}

		collectionGroup.SetAnnotations(skipDacAnnotationFalse)

		if err := checkCouchbaseCollectionGroupResourceConstraints(collectionGroup); err != nil {
			collectionGroup.SetAnnotations(map[string]string{constants.AnnotationUnreconcilable: "true"})

			if updateErr := client.CouchbaseCollectionGroups.Update(collectionGroup); updateErr != nil {
				errs = append(errs, updateErr)
			}

			errs = append(errs, err)
		}
	}

	return errs
}

func validateCollections(client *client.Client) []error {
	var errs []error

	for _, collection := range client.CouchbaseCollections.List() {
		if shouldSkipValidation(collection.Annotations) {
			continue
		}

		collection.SetAnnotations(skipDacAnnotationFalse)

		if err := checkCouchbaseCollectionResourceConstraints(collection); err != nil {
			collection.SetAnnotations(map[string]string{constants.AnnotationUnreconcilable: "true"})

			if updateErr := client.CouchbaseCollections.Update(collection); updateErr != nil {
				errs = append(errs, updateErr)
			}

			errs = append(errs, err)
		}
	}

	return errs
}

func validateBackupRestores(client *client.Client) []error {
	var errs []error

	for _, backupRestore := range client.CouchbaseBackupRestores.List() {
		if shouldSkipValidation(backupRestore.Annotations) {
			continue
		}

		backupRestore.SetAnnotations(skipDacAnnotationFalse)

		if err := checkCouchbaseBackupRestoreResourceConstraints(backupRestore); err != nil {
			backupRestore.SetAnnotations(map[string]string{constants.AnnotationUnreconcilable: "true"})

			if updateErr := client.CouchbaseBackupRestores.Update(backupRestore); updateErr != nil {
				errs = append(errs, updateErr)
			}

			errs = append(errs, err)
		}
	}

	return errs
}

func validateBuckets(client *client.Client) []error {
	var errs []error

	couchbaseBuckets := client.CouchbaseBuckets.List()
	memcachedBuckets := client.CouchbaseMemcachedBuckets.List()
	ephemeralBuckets := client.CouchbaseEphemeralBuckets.List()

	if err := validateCouchbaseBuckets(couchbaseBuckets, client); len(err) != 0 {
		errs = append(errs, err...)
	}

	if err := validateMemcachedBuckets(memcachedBuckets, client); len(err) != 0 {
		errs = append(errs, err...)
	}

	if err := validateEphemeralBuckets(ephemeralBuckets, client); len(err) != 0 {
		errs = append(errs, err...)
	}

	return errs
}

func validateCouchbaseGroupsConstraints(client *client.Client) []error {
	var errs []error

	for _, group := range client.CouchbaseGroups.List() {
		if shouldSkipValidation(group.Annotations) {
			continue
		}

		group.SetAnnotations(skipDacAnnotationFalse)

		if err := checkCouchbaseGroupResourceConstraints(group); err != nil {
			group.SetAnnotations(map[string]string{constants.AnnotationUnreconcilable: "true"})

			if updateErr := client.CouchbaseGroups.Update(group); updateErr != nil {
				errs = append(errs, updateErr)
			}

			errs = append(errs, err)
		}
	}

	return errs
}

func validateCouchbaseBackupsConstraints(client *client.Client) []error {
	var errs []error

	for _, backup := range client.CouchbaseBackups.List() {
		if shouldSkipValidation(backup.Annotations) {
			continue
		}

		backup.SetAnnotations(skipDacAnnotationFalse)

		if err := checkCouchbaseBackupResourceConstraints(backup); err != nil {
			backup.SetAnnotations(map[string]string{constants.AnnotationUnreconcilable: "true"})

			if updateErr := client.CouchbaseBackups.Update(backup); updateErr != nil {
				errs = append(errs, updateErr)
			}

			errs = append(errs, err)
		}
	}

	return errs
}

func validateCouchbaseUsers(client *client.Client) []error {
	var errs []error

	for _, user := range client.CouchbaseUsers.List() {
		if shouldSkipValidation(user.Annotations) {
			continue
		}

		user.SetAnnotations(skipDacAnnotationFalse)

		if err := checkCouchbaseUserResourceConstraints(user); err != nil {
			user.SetAnnotations(map[string]string{constants.AnnotationUnreconcilable: "true"})

			if updateErr := client.CouchbaseUsers.Update(user); updateErr != nil {
				errs = append(errs, updateErr)
			}

			errs = append(errs, err)
		}
	}

	return errs
}

func validateCouchbaseBuckets(buckets []*couchbasev2.CouchbaseBucket, client *client.Client) []error {
	var errs []error

	for _, bucket := range buckets {
		if shouldSkipValidation(bucket.Annotations) {
			continue
		}

		bucket.SetAnnotations(skipDacAnnotationFalse)

		if err := checkCouchbaseBucketsConstraints(bucket); err != nil {
			bucket.SetAnnotations(map[string]string{constants.AnnotationUnreconcilable: "true"})

			if updateErr := client.CouchbaseBuckets.Update(bucket); updateErr != nil {
				errs = append(errs, updateErr)
			}

			errs = append(errs, err)
		}
	}

	return errs
}

func validateMemcachedBuckets(buckets []*couchbasev2.CouchbaseMemcachedBucket, client *client.Client) []error {
	var errs []error

	for _, bucket := range buckets {
		if shouldSkipValidation(bucket.Annotations) {
			continue
		}

		bucket.SetAnnotations(skipDacAnnotationFalse)

		if err := checkMemcachedBucketsConstraints(bucket); err != nil {
			bucket.SetAnnotations(map[string]string{constants.AnnotationUnreconcilable: "true"})

			if updateErr := client.CouchbaseMemcachedBuckets.Update(bucket); updateErr != nil {
				errs = append(errs, updateErr)
			}

			errs = append(errs, err)
		}
	}

	return errs
}

func validateEphemeralBuckets(buckets []*couchbasev2.CouchbaseEphemeralBucket, client *client.Client) []error {
	var errs []error

	for _, bucket := range buckets {
		if shouldSkipValidation(bucket.Annotations) {
			continue
		}

		bucket.SetAnnotations(skipDacAnnotationFalse)

		if err := checkEphemeralBucketConstraints(bucket); err != nil {
			bucket.SetAnnotations(map[string]string{constants.AnnotationUnreconcilable: "true"})

			if updateErr := client.CouchbaseEphemeralBuckets.Update(bucket); updateErr != nil {
				errs = append(errs, updateErr)
			}

			errs = append(errs, err)
		}
	}

	return errs
}

func checkEphemeralBucketConstraints(bucket *couchbasev2.CouchbaseEphemeralBucket) error {
	return validationv2.CheckConstraintsEphemeralBucket(v, bucket)
}

func checkMemcachedBucketsConstraints(bucket *couchbasev2.CouchbaseMemcachedBucket) error {
	return validationv2.CheckConstraintsMemcachedBucket(v, bucket)
}

func checkCouchbaseBucketsConstraints(bucket *couchbasev2.CouchbaseBucket) error {
	return validationv2.CheckConstraintsBucket(v, bucket)
}

func CheckCouchbaseClusterResource(cluster *couchbasev2.CouchbaseCluster) ([]string, error) {
	skipValidation, found := cluster.Annotations[constants.AnnotationSkipDACValidation]
	if found {
		if strings.EqualFold(skipValidation, "true") {
			return nil, nil
		}
	}

	cluster.SetAnnotations(skipDacAnnotationFalse)

	return validationv2.CheckConstraints(v, cluster)
}

func CheckCouchbaseClusterResourceUpdate(update *couchbasev2.CouchbaseCluster, cluster *couchbasev2.CouchbaseCluster) error {
	skipValidation, found := cluster.Annotations[constants.AnnotationSkipDACValidation]
	if found {
		if strings.EqualFold(skipValidation, "true") {
			return nil
		}
	}

	cluster.SetAnnotations(skipDacAnnotationFalse)

	return validationv2.CheckChangeConstraintsCluster(v, cluster, update)
}

func CheckCouchbaseClusterResourceImmutableFields(update *couchbasev2.CouchbaseCluster, cluster *couchbasev2.CouchbaseCluster) error {
	skipValidation, found := cluster.Annotations[constants.AnnotationSkipDACValidation]
	if found {
		if strings.EqualFold(skipValidation, "true") {
			return nil
		}
	}

	update.SetAnnotations(skipDacAnnotationFalse)

	return validationv2.CheckImmutableFields(cluster, update)
}

func checkCouchbaseUserResourceConstraints(user *couchbasev2.CouchbaseUser) error {
	return validationv2.CheckConstraintsCouchbaseUser(v, user)
}

func checkCouchbaseGroupResourceConstraints(group *couchbasev2.CouchbaseGroup) error {
	return validationv2.CheckConstraintsCouchbaseGroup(v, group)
}

func checkCouchbaseBackupResourceConstraints(backup *couchbasev2.CouchbaseBackup) error {
	return validationv2.CheckConstraintsBackup(v, backup)
}

func checkCouchbaseBackupRestoreResourceConstraints(backupRestore *couchbasev2.CouchbaseBackupRestore) error {
	return validationv2.CheckConstraintsBackupRestore(v, backupRestore)
}

func checkCouchbaseCollectionResourceConstraints(collection *couchbasev2.CouchbaseCollection) error {
	return validationv2.CheckConstraintsCollection(v, collection)
}

func checkCouchbaseCollectionGroupResourceConstraints(collectionGroup *couchbasev2.CouchbaseCollectionGroup) error {
	return validationv2.CheckConstraintsCollectionGroup(v, collectionGroup)
}

func checkCouchbaseScopeResourceConstraints(scope *couchbasev2.CouchbaseScope) error {
	return validationv2.CheckConstraintsScope(v, scope)
}

func checkCouchbaseScopeGroupResourceConstraints(scopeGroup *couchbasev2.CouchbaseScopeGroup) error {
	return validationv2.CheckConstraintsScopeGroup(v, scopeGroup)
}

func shouldSkipValidation(annotations map[string]string) bool {
	skipValidation, found := annotations[constants.AnnotationSkipDACValidation]
	return found && strings.EqualFold(skipValidation, "true")
}
