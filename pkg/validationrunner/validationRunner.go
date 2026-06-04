/*
Copyright 2024-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package validationrunner

import (
	ctx "context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"strings"

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

func isValidationError(err error) bool {
	// This is a hack to stop scenarios where resources are created simultaneously or before
	// operator and its roles.  This can lead to the K8S API rejecting the cache requests.
	// This is really only been seen in E2E, not a common customer issue.
	return err != nil && !strings.Contains(err.Error(), "forbidden")
}

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

func CheckManagedResourceImmutableConstraints(currentCluster *cluster.Cluster) ([]error, map[string]bool) {
	var errs []error

	if currentCluster.GetCouchbaseCluster().Spec.Paused {
		return nil, nil
	}

	bucketErrs, failedBuckets := validateBucketsImmutableFields(currentCluster)
	errs = append(errs, bucketErrs...)
	errs = append(errs, validateReplicationsImmutableFields(currentCluster)...)
	errs = append(errs, validateBackupsImmutableFields(currentCluster)...)
	errs = append(errs, validateAutoscalersImmutableField(currentCluster)...)

	return errs, failedBuckets
}

func validateAutoscalersImmutableField(currentCluster *cluster.Cluster) []error {
	var errs []error

	autoscalerUpdates, err := currentCluster.GatherAutoscalerUpdates()
	if err != nil {
		if checkIsMemberError(err) {
			return nil
		}

		return append(errs, err)
	}

	for current, update := range autoscalerUpdates {
		if err := validationv2.CheckImmutableFieldsAutoscaler(current, update); isValidationError(err) {
			couchbaseutil.AddAnnotation(&update.ObjectMeta, constants.AnnotationUnreconcilable, "true")

			if _, updateErr := currentCluster.GetK8sClient().CouchbaseClient.CouchbaseV2().CouchbaseAutoscalers(currentCluster.GetCouchbaseCluster().Namespace).Update(ctx.Background(), update, metav1.UpdateOptions{}); updateErr != nil {
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
		if checkIsMemberError(err) {
			return nil
		}

		return append(errs, err)
	}

	for actual, update := range backupUpdates {
		if err := validationv2.CheckImmutableFieldsBackup(actual, update); isValidationError(err) {
			couchbaseutil.AddAnnotation(&update.ObjectMeta, constants.AnnotationUnreconcilable, "true")

			if _, updateErr := currentCluster.GetK8sClient().CouchbaseClient.CouchbaseV2().CouchbaseBackups(currentCluster.GetCouchbaseCluster().Namespace).Update(ctx.Background(), update, metav1.UpdateOptions{}); updateErr != nil {
				errs = append(errs, updateErr)
			}

			errs = append(errs, err)
		}
	}

	return errs
}

func validateReplicationsImmutableFields(currentCluster *cluster.Cluster) []error {
	var errs []error

	// Skip validation if cluster is not ready or XDCR is not managed
	if !currentCluster.GetCouchbaseCluster().Spec.XDCR.Managed {
		return nil
	}

	// Build desired state from CRDs - this might fail if remote clusters aren't configured yet
	desiredStates, err := currentCluster.BuildDesiredReplicationStates()
	if err != nil {
		if checkIsMemberError(err) {
			return nil
		}
		// If remote clusters aren't ready, skip validation rather than failing
		return nil
	}

	// Get current replications from server
	currentReplications, err := currentCluster.ListReplications()
	if err != nil {
		if checkIsMemberError(err) {
			return nil
		}
		// If server isn't ready, skip validation
		return nil
	}

	// Fetch current state from server
	currentStates, err := currentCluster.FetchCurrentReplicationStates(currentReplications)
	if err != nil {
		if checkIsMemberError(err) {
			return nil
		}
		// If we can't fetch current state, skip validation
		return nil
	}

	// Check for immutable field changes (only user-configurable fields)
	for key, desiredState := range desiredStates {
		if currentState, exists := currentStates[key]; exists {
			// Only validate truly immutable user-configurable fields
			// Note: ToCluster, Type, ReplicationType are not user-configurable.
			if currentState.Create.FromBucket != string(desiredState.Spec.Bucket) {
				errs = append(errs, util.NewUpdateError("spec.bucket", "body"))
			}

			if currentState.Create.ToBucket != string(desiredState.Spec.RemoteBucket) {
				errs = append(errs, util.NewUpdateError("spec.remoteBucket", "body"))
			}
		}
	}

	return errs
}

func validateBucketsImmutableFields(currentCluster *cluster.Cluster) ([]error, map[string]bool) {
	var errs []error

	failedBuckets := make(map[string]bool)

	updateBuckets, err := currentCluster.GetBucketsToUpdate()
	if err != nil {
		if checkIsMemberError(err) {
			return nil, failedBuckets
		}

		return append(errs, err), failedBuckets
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

		if err := validator.CheckImmutableFields(oldBucket, newBucket); isValidationError(err) {
			errs = append(errs, err)
			failedBuckets[update.BucketName] = true
		}
	}

	return errs, failedBuckets
}

func CheckManagedResourceChangeConstraints(currentCluster *cluster.Cluster) ([]error, map[string]bool) {
	var errs []error

	if currentCluster.GetCouchbaseCluster().Spec.Paused {
		return nil, nil
	}

	rv := &reconcileValidator{v: currentCluster.GetValidator(), k8s: currentCluster.GetK8sClient()}

	bucketErrs, failedBuckets := rv.validateBucketsChangeConstraints(currentCluster)
	errs = append(errs, bucketErrs...)

	return errs, failedBuckets
}

func (rv *reconcileValidator) CheckClusterChangeConstraints(currentCluster, updatedCluster *couchbasev2.CouchbaseCluster) error {
	if err := rv.CheckCouchbaseClusterResourceImmutableFields(updatedCluster, currentCluster); err != nil {
		return err
	}

	if err := rv.CheckCouchbaseClusterResourceUpdate(updatedCluster, currentCluster); err != nil {
		return err
	}

	return nil
}

//nolint:gocognit
func (rv *reconcileValidator) validateBucketsChangeConstraints(currentCluster *cluster.Cluster) ([]error, map[string]bool) {
	var errs []error

	failedBuckets := make(map[string]bool)

	namer := &bucketNamer{
		c: currentCluster,
	}

	updateBuckets, err := currentCluster.GetBucketsToUpdate()
	if err != nil {
		if checkIsMemberError(err) {
			return nil, failedBuckets
		}

		return append(errs, err), failedBuckets
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

		switch t1 := oldBucket.(type) {
		case *couchbasev2.CouchbaseBucket:
			if t2, ok := newBucket.(*couchbasev2.CouchbaseBucket); ok {
				// ConvertAbstractBucketToAPIBucket only sets Name, not Namespace.
				// Validators that list namespace-scoped resources (e.g. collections)
				// need the correct namespace to query against.
				ns := currentCluster.GetCouchbaseCluster().Namespace
				t1.Namespace = ns
				t2.Namespace = ns
				if _, err := validationv2.CheckChangeConstraintsBucket(rv.v, t1, t2, currentCluster.GetCouchbaseCluster()); isValidationError(err) {
					errs = append(errs, err)
					failedBuckets[update.BucketName] = true
				}
			}
		case *couchbasev2.CouchbaseEphemeralBucket:
			if t2, ok := newBucket.(*couchbasev2.CouchbaseEphemeralBucket); ok {
				if err := validationv2.CheckChangeConstraintsEphemeralBucket(rv.v, t1, t2, currentCluster.GetCouchbaseCluster()); isValidationError(err) {
					errs = append(errs, err)
					failedBuckets[update.BucketName] = true
				}
			}

		default:
			// It must be a couchbase bucket so continue if it's somehow something else.
			continue
		}
	}

	return errs, failedBuckets
}

// reconcileValidator bundles the cluster's pre-initialised validator with its k8s
// client so that validation methods can be run as part of reconciliation. It is
// cheap to construct (two pointer fields) — the underlying validator is created
// once in cluster.New and reused across all reconcile cycles.
type reconcileValidator struct {
	v   *types.Validator
	k8s *client.Client
}

func CheckCouchbaseClusterResource(v *types.Validator, c *client.Client, couchbase *couchbasev2.CouchbaseCluster) ([]string, error) {
	rv := &reconcileValidator{v: v, k8s: c}
	return rv.CheckCouchbaseClusterResource(couchbase)
}

func CheckClusterChangeConstraints(v *types.Validator, c *client.Client, current, updated *couchbasev2.CouchbaseCluster) error {
	rv := &reconcileValidator{v: v, k8s: c}
	return rv.CheckClusterChangeConstraints(current, updated)
}

func CheckManagedResourceConstraints(c *cluster.Cluster) []error {
	var errs []error

	if c.GetCouchbaseCluster().Spec.Paused {
		return nil
	}

	rv := &reconcileValidator{v: c.GetValidator(), k8s: c.GetK8sClient()}

	errs = append(errs, rv.validateBuckets(c)...)
	errs = append(errs, rv.validateCouchbaseUsers()...)
	errs = append(errs, rv.validateCouchbaseGroupsConstraints()...)
	errs = append(errs, rv.validateCouchbaseBackupsConstraints()...)
	errs = append(errs, rv.validateBackupRestores()...)
	errs = append(errs, rv.validateCollections()...)
	errs = append(errs, rv.validateCollectionGroups()...)
	errs = append(errs, rv.validateScopes()...)
	errs = append(errs, rv.validateScopeGroups()...)

	return errs
}

func (rv *reconcileValidator) validateScopeGroups() []error {
	var errs []error

	for _, scopeGroup := range rv.k8s.CouchbaseScopeGroups.List() {
		if shouldSkipValidation(scopeGroup.Annotations) {
			continue
		}

		couchbaseutil.AddAnnotation(&scopeGroup.ObjectMeta, constants.AnnotationDisableAdmissionController, "false")

		if err := rv.checkCouchbaseScopeGroupResourceConstraints(scopeGroup); isValidationError(err) {
			couchbaseutil.AddAnnotation(&scopeGroup.ObjectMeta, constants.AnnotationUnreconcilable, "true")

			if _, updateErr := rv.k8s.CouchbaseClient.CouchbaseV2().CouchbaseScopeGroups(scopeGroup.Namespace).Update(ctx.Background(), scopeGroup, metav1.UpdateOptions{}); updateErr != nil {
				errs = append(errs, updateErr)
			}

			errs = append(errs, err)
		}
	}

	return errs
}

func (rv *reconcileValidator) validateScopes() []error {
	var errs []error

	for _, scope := range rv.k8s.CouchbaseScopes.List() {
		if shouldSkipValidation(scope.Annotations) {
			continue
		}

		couchbaseutil.AddAnnotation(&scope.ObjectMeta, constants.AnnotationDisableAdmissionController, "false")

		if err := rv.checkCouchbaseScopeResourceConstraints(scope); isValidationError(err) {
			couchbaseutil.AddAnnotation(&scope.ObjectMeta, constants.AnnotationUnreconcilable, "true")

			if _, updateErr := rv.k8s.CouchbaseClient.CouchbaseV2().CouchbaseScopes(scope.Namespace).Update(ctx.Background(), scope, metav1.UpdateOptions{}); updateErr != nil {
				errs = append(errs, updateErr)
			}

			errs = append(errs, err)
		}
	}

	return errs
}

func (rv *reconcileValidator) validateCollectionGroups() []error {
	var errs []error

	for _, collectionGroup := range rv.k8s.CouchbaseCollectionGroups.List() {
		if shouldSkipValidation(collectionGroup.Annotations) {
			continue
		}

		couchbaseutil.AddAnnotation(&collectionGroup.ObjectMeta, constants.AnnotationDisableAdmissionController, "false")

		if err := rv.checkCouchbaseCollectionGroupResourceConstraints(collectionGroup); isValidationError(err) {
			couchbaseutil.AddAnnotation(&collectionGroup.ObjectMeta, constants.AnnotationUnreconcilable, "true")

			if _, updateErr := rv.k8s.CouchbaseClient.CouchbaseV2().CouchbaseCollectionGroups(collectionGroup.Namespace).Update(ctx.Background(), collectionGroup, metav1.UpdateOptions{}); updateErr != nil {
				errs = append(errs, updateErr)
			}

			errs = append(errs, err)
		}
	}

	return errs
}

func (rv *reconcileValidator) validateCollections() []error {
	var errs []error

	for _, collection := range rv.k8s.CouchbaseCollections.List() {
		if shouldSkipValidation(collection.Annotations) {
			continue
		}

		couchbaseutil.AddAnnotation(&collection.ObjectMeta, constants.AnnotationDisableAdmissionController, "false")

		if err := rv.checkCouchbaseCollectionResourceConstraints(collection); isValidationError(err) {
			couchbaseutil.AddAnnotation(&collection.ObjectMeta, constants.AnnotationUnreconcilable, "true")

			if _, updateErr := rv.k8s.CouchbaseClient.CouchbaseV2().CouchbaseCollections(collection.Namespace).Update(ctx.Background(), collection, metav1.UpdateOptions{}); updateErr != nil {
				errs = append(errs, updateErr)
			}

			errs = append(errs, err)
		}
	}

	return errs
}

func (rv *reconcileValidator) validateBackupRestores() []error {
	var errs []error

	for _, backupRestore := range rv.k8s.CouchbaseBackupRestores.List() {
		if shouldSkipValidation(backupRestore.Annotations) {
			continue
		}

		couchbaseutil.AddAnnotation(&backupRestore.ObjectMeta, constants.AnnotationDisableAdmissionController, "false")

		if err := rv.checkCouchbaseBackupRestoreResourceConstraints(backupRestore); isValidationError(err) {
			couchbaseutil.AddAnnotation(&backupRestore.ObjectMeta, constants.AnnotationUnreconcilable, "true")

			if _, updateErr := rv.k8s.CouchbaseClient.CouchbaseV2().CouchbaseBackupRestores(backupRestore.Namespace).Update(ctx.Background(), backupRestore, metav1.UpdateOptions{}); updateErr != nil {
				errs = append(errs, updateErr)
			}

			errs = append(errs, err)
		}
	}

	return errs
}

func (rv *reconcileValidator) validateBuckets(c *cluster.Cluster) []error {
	var errs []error

	cbc := c.GetCouchbaseCluster()

	if err := rv.validateCouchbaseBuckets(rv.k8s.CouchbaseBuckets.List(), cbc); len(err) != 0 {
		errs = append(errs, err...)
	}

	if err := rv.validateMemcachedBuckets(rv.k8s.CouchbaseMemcachedBuckets.List(), cbc); len(err) != 0 {
		errs = append(errs, err...)
	}

	if err := rv.validateEphemeralBuckets(rv.k8s.CouchbaseEphemeralBuckets.List(), cbc); len(err) != 0 {
		errs = append(errs, err...)
	}

	return errs
}

func (rv *reconcileValidator) validateCouchbaseGroupsConstraints() []error {
	var errs []error

	for _, group := range rv.k8s.CouchbaseGroups.List() {
		if shouldSkipValidation(group.Annotations) {
			continue
		}

		couchbaseutil.AddAnnotation(&group.ObjectMeta, constants.AnnotationDisableAdmissionController, "false")

		if err := rv.checkCouchbaseGroupResourceConstraints(group); isValidationError(err) {
			couchbaseutil.AddAnnotation(&group.ObjectMeta, constants.AnnotationUnreconcilable, "true")

			if _, updateErr := rv.k8s.CouchbaseClient.CouchbaseV2().CouchbaseGroups(group.Namespace).Update(ctx.Background(), group, metav1.UpdateOptions{}); updateErr != nil {
				errs = append(errs, updateErr)
			}

			errs = append(errs, err)
		}
	}

	return errs
}

func (rv *reconcileValidator) validateCouchbaseBackupsConstraints() []error {
	var errs []error

	for _, backup := range rv.k8s.CouchbaseBackups.List() {
		if shouldSkipValidation(backup.Annotations) {
			continue
		}

		couchbaseutil.AddAnnotation(&backup.ObjectMeta, constants.AnnotationDisableAdmissionController, "false")

		if err := rv.checkCouchbaseBackupResourceConstraints(backup); isValidationError(err) {
			couchbaseutil.AddAnnotation(&backup.ObjectMeta, constants.AnnotationUnreconcilable, "true")

			if _, updateErr := rv.k8s.CouchbaseClient.CouchbaseV2().CouchbaseBackups(backup.Namespace).Update(ctx.Background(), backup, metav1.UpdateOptions{}); updateErr != nil {
				errs = append(errs, updateErr)
			}

			errs = append(errs, err)
		}
	}

	return errs
}

func (rv *reconcileValidator) validateCouchbaseUsers() []error {
	var errs []error

	for _, user := range rv.k8s.CouchbaseUsers.List() {
		if shouldSkipValidation(user.Annotations) {
			continue
		}

		couchbaseutil.AddAnnotation(&user.ObjectMeta, constants.AnnotationDisableAdmissionController, "false")

		if err := rv.checkCouchbaseUserResourceConstraints(user); isValidationError(err) {
			couchbaseutil.AddAnnotation(&user.ObjectMeta, constants.AnnotationUnreconcilable, "true")

			if _, updateErr := rv.k8s.CouchbaseClient.CouchbaseV2().CouchbaseUsers(user.Namespace).Update(ctx.Background(), user, metav1.UpdateOptions{}); updateErr != nil {
				errs = append(errs, updateErr)
			}

			errs = append(errs, err)
		}
	}

	return errs
}

func (rv *reconcileValidator) validateCouchbaseBuckets(buckets []*couchbasev2.CouchbaseBucket, cluster *couchbasev2.CouchbaseCluster) []error {
	var errs []error

	for _, bucket := range buckets {
		if shouldSkipValidation(bucket.Annotations) {
			continue
		}

		if err := rv.checkCouchbaseBucketsConstraints(bucket, cluster); isValidationError(err) {
			errs = append(errs, err)
		}
	}

	return errs
}

func (rv *reconcileValidator) validateMemcachedBuckets(buckets []*couchbasev2.CouchbaseMemcachedBucket, cluster *couchbasev2.CouchbaseCluster) []error {
	var errs []error

	for _, bucket := range buckets {
		if shouldSkipValidation(bucket.Annotations) {
			continue
		}

		if err := rv.checkMemcachedBucketsConstraints(bucket, cluster); isValidationError(err) {
			errs = append(errs, err)
		}
	}

	return errs
}

func (rv *reconcileValidator) validateEphemeralBuckets(buckets []*couchbasev2.CouchbaseEphemeralBucket, cluster *couchbasev2.CouchbaseCluster) []error {
	var errs []error

	for _, bucket := range buckets {
		if shouldSkipValidation(bucket.Annotations) {
			continue
		}

		if err := rv.checkEphemeralBucketConstraints(bucket, cluster); isValidationError(err) {
			errs = append(errs, err)
		}
	}

	return errs
}

func (rv *reconcileValidator) checkEphemeralBucketConstraints(bucket *couchbasev2.CouchbaseEphemeralBucket, cluster *couchbasev2.CouchbaseCluster) error {
	return validationv2.CheckConstraintsEphemeralBucket(rv.v, bucket, cluster)
}

func (rv *reconcileValidator) checkMemcachedBucketsConstraints(bucket *couchbasev2.CouchbaseMemcachedBucket, cluster *couchbasev2.CouchbaseCluster) error {
	_, err := validationv2.CheckConstraintsMemcachedBucket(rv.v, bucket, cluster)
	return err
}

func (rv *reconcileValidator) checkCouchbaseBucketsConstraints(bucket *couchbasev2.CouchbaseBucket, cluster *couchbasev2.CouchbaseCluster) error {
	_, err := validationv2.CheckConstraintsBucket(rv.v, bucket, cluster)
	return err
}

func (rv *reconcileValidator) CheckCouchbaseClusterResource(cluster *couchbasev2.CouchbaseCluster) ([]string, error) {
	skipValidation, found := cluster.Annotations[constants.AnnotationSkipDACValidation]
	if found {
		if strings.EqualFold(skipValidation, "true") {
			return nil, nil
		}
	}

	couchbaseutil.AddAnnotation(&cluster.ObjectMeta, constants.AnnotationDisableAdmissionController, "false")

	return validationv2.CheckConstraints(rv.v, cluster)
}

func (rv *reconcileValidator) CheckCouchbaseClusterResourceUpdate(update *couchbasev2.CouchbaseCluster, cluster *couchbasev2.CouchbaseCluster) error {
	skipValidation, found := update.Annotations[constants.AnnotationSkipDACValidation]
	if found {
		if strings.EqualFold(skipValidation, "true") {
			return nil
		}
	}

	if reflect.DeepEqual(update, cluster) {
		return nil
	}

	couchbaseutil.AddAnnotation(&update.ObjectMeta, constants.AnnotationDisableAdmissionController, "false")

	_, err := validationv2.CheckChangeConstraintsCluster(rv.v, cluster, update)

	return err
}

func (rv *reconcileValidator) CheckCouchbaseClusterResourceImmutableFields(update *couchbasev2.CouchbaseCluster, cluster *couchbasev2.CouchbaseCluster) error {
	skipValidation, found := update.Annotations[constants.AnnotationSkipDACValidation]
	if found {
		if strings.EqualFold(skipValidation, "true") {
			return nil
		}
	}

	if reflect.DeepEqual(update, cluster) {
		return nil
	}

	couchbaseutil.AddAnnotation(&update.ObjectMeta, constants.AnnotationDisableAdmissionController, "false")

	return validationv2.CheckImmutableFields(cluster, update)
}

func (rv *reconcileValidator) checkCouchbaseUserResourceConstraints(user *couchbasev2.CouchbaseUser) error {
	_, err := validationv2.CheckConstraintsCouchbaseUser(rv.v, user)
	return err
}

func (rv *reconcileValidator) checkCouchbaseGroupResourceConstraints(group *couchbasev2.CouchbaseGroup) error {
	return validationv2.CheckConstraintsCouchbaseGroup(rv.v, group)
}

func (rv *reconcileValidator) checkCouchbaseBackupResourceConstraints(backup *couchbasev2.CouchbaseBackup) error {
	return validationv2.CheckConstraintsBackup(rv.v, backup)
}

func (rv *reconcileValidator) checkCouchbaseBackupRestoreResourceConstraints(backupRestore *couchbasev2.CouchbaseBackupRestore) error {
	return validationv2.CheckConstraintsBackupRestore(rv.v, backupRestore)
}

func (rv *reconcileValidator) checkCouchbaseCollectionResourceConstraints(collection *couchbasev2.CouchbaseCollection) error {
	_, err := validationv2.CheckConstraintsCollection(rv.v, collection)
	return err
}

func (rv *reconcileValidator) checkCouchbaseCollectionGroupResourceConstraints(collectionGroup *couchbasev2.CouchbaseCollectionGroup) error {
	_, err := validationv2.CheckConstraintsCollectionGroup(rv.v, collectionGroup)
	return err
}

func (rv *reconcileValidator) checkCouchbaseScopeResourceConstraints(scope *couchbasev2.CouchbaseScope) error {
	return validationv2.CheckConstraintsScope(rv.v, scope)
}

func (rv *reconcileValidator) checkCouchbaseScopeGroupResourceConstraints(scopeGroup *couchbasev2.CouchbaseScopeGroup) error {
	return validationv2.CheckConstraintsScopeGroup(rv.v, scopeGroup)
}

func shouldSkipValidation(annotations map[string]string) bool {
	skipValidation, found := annotations[constants.AnnotationSkipDACValidation]
	return found && strings.EqualFold(skipValidation, "true")
}

func checkIsMemberError(err error) bool {
	return errors.Is(err, couchbaseutil.ErrMemberError)
}

// validateXDCRReplicationBuckets checks bucket existence for all XDCR replications during
// the reconcile cycle. Instead of blocking the entire cluster, it returns a set of failed
// replication names so only those replications are skipped during reconciliation.
func ValidateXDCRReplicationBuckets(currentCluster *cluster.Cluster) ([]error, map[string]bool) {
	cbCluster := currentCluster.GetCouchbaseCluster()

	if !cbCluster.Spec.XDCR.Managed {
		return nil, nil
	}

	rv := &reconcileValidator{v: currentCluster.GetValidator(), k8s: currentCluster.GetK8sClient()}

	var errs []error

	failedReplications := make(map[string]bool)

	failedNames, err := validationv2.CheckConstraintXDCRReplicationBucketsForReconcile(rv.v, cbCluster)
	if err != nil {
		errs = append(errs, err)
	}

	for _, name := range failedNames {
		failedReplications[name] = true
	}

	return errs, failedReplications
}
