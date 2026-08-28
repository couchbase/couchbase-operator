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
	goerrors "errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/client"
	"github.com/couchbase/couchbase-operator/pkg/cluster"
	"github.com/couchbase/couchbase-operator/pkg/conversion"
	"github.com/couchbase/couchbase-operator/pkg/unreconcilable"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/validator"
	"github.com/couchbase/couchbase-operator/pkg/validator/types"
	"github.com/couchbase/couchbase-operator/pkg/validator/util"
	validationv2 "github.com/couchbase/couchbase-operator/pkg/validator/v2"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type bucketNamer struct {
	// c is a cluster reference so we have access to the cluster name to uniquely
	// name resources based on cluster (as we may have multiple in the same namespace)
	c *cluster.Cluster
}

// isSpecInvalid reports whether err is a verdict that the resource's spec is
// invalid, as opposed to a failure to determine whether it is valid.
func isSpecInvalid(err error) bool {
	return err != nil && !isIndeterminateError(err)
}

// isIndeterminateError reports whether err means "I could not tell" rather than
// "the spec is wrong". These validators run every reconcile cycle and do live
// reads, so mistaking a transient blip for a verdict would brand a perfectly
// healthy resource unreconcilable every ten seconds.
func isIndeterminateError(err error) bool {
	if err == nil {
		return false
	}

	if checkIsMemberError(err) {
		return true
	}

	if goerrors.Is(err, ctx.Canceled) || goerrors.Is(err, ctx.DeadlineExceeded) {
		return true
	}

	if k8serrors.IsForbidden(err) || k8serrors.IsUnauthorized(err) ||
		k8serrors.IsTimeout(err) || k8serrors.IsServerTimeout(err) ||
		k8serrors.IsTooManyRequests(err) || k8serrors.IsInternalError(err) ||
		k8serrors.IsServiceUnavailable(err) || k8serrors.IsUnexpectedServerError(err) {
		return true
	}

	// The validators aggregate into composite errors that never implement
	// Unwrap, so the typed checks above cannot always see through them.
	return strings.Contains(err.Error(), "forbidden")
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

// CheckManagedResourceImmutableConstraints validates that no immutable field has
// been changed on a dependent resource, marking every offender into the
// cluster's unreconcilable tracker.
func CheckManagedResourceImmutableConstraints(currentCluster *cluster.Cluster) []error {
	var errs []error

	if currentCluster.GetCouchbaseCluster().Spec.Paused {
		return nil
	}

	errs = append(errs, validateBucketsImmutableFields(currentCluster)...)
	errs = append(errs, validateReplicationsImmutableFields(currentCluster)...)
	errs = append(errs, validateBackupsImmutableFields(currentCluster)...)
	errs = append(errs, validateAutoscalersImmutableField(currentCluster)...)

	return errs
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
		if err := validationv2.CheckImmutableFieldsAutoscaler(current, update); isSpecInvalid(err) {
			currentCluster.Unreconcilable().Mark(
				unreconcilable.Ref{Kind: couchbasev2.AutoscalerCRDResourceKind, Name: update.Name},
				unreconcilable.ReasonImmutableFieldChanged, err.Error())

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
		if err := validationv2.CheckImmutableFieldsBackup(actual, update); isSpecInvalid(err) {
			currentCluster.Unreconcilable().Mark(
				unreconcilable.Ref{Kind: couchbasev2.BackupCRDResourceKind, Name: update.Name},
				unreconcilable.ReasonImmutableFieldChanged, err.Error())

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

func validateBucketsImmutableFields(currentCluster *cluster.Cluster) []error {
	var errs []error

	updateBuckets, err := currentCluster.GetBucketsToUpdate()
	if err != nil {
		if checkIsMemberError(err) {
			return nil
		}

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

		if err := validator.CheckImmutableFields(oldBucket, newBucket); isSpecInvalid(err) {
			errs = append(errs, err)
			currentCluster.Unreconcilable().MarkBucket(update.BucketName,
				unreconcilable.ReasonImmutableFieldChanged, err.Error())
		}
	}

	return errs
}

// CheckManagedResourceChangeConstraints validates dependent-resource updates
// against the current state, marking every offender into the cluster's
// unreconcilable tracker.
func CheckManagedResourceChangeConstraints(currentCluster *cluster.Cluster) []error {
	var errs []error

	if currentCluster.GetCouchbaseCluster().Spec.Paused {
		return nil
	}

	rv := &reconcileValidator{v: currentCluster.GetValidator(), k8s: currentCluster.GetK8sClient(), tracker: currentCluster.Unreconcilable()}

	errs = append(errs, rv.validateBucketsChangeConstraints(currentCluster)...)

	return errs
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
func (rv *reconcileValidator) validateBucketsChangeConstraints(currentCluster *cluster.Cluster) []error {
	var errs []error

	namer := &bucketNamer{
		c: currentCluster,
	}

	updateBuckets, err := currentCluster.GetBucketsToUpdate()
	if err != nil {
		if checkIsMemberError(err) {
			return nil
		}

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

		switch t1 := oldBucket.(type) {
		case *couchbasev2.CouchbaseBucket:
			if t2, ok := newBucket.(*couchbasev2.CouchbaseBucket); ok {
				// ConvertAbstractBucketToAPIBucket only sets Name, not Namespace.
				// Validators that list namespace-scoped resources (e.g. collections)
				// need the correct namespace to query against.
				ns := currentCluster.GetCouchbaseCluster().Namespace
				t1.Namespace = ns
				t2.Namespace = ns
				if _, err := validationv2.CheckChangeConstraintsBucket(rv.v, t1, t2, currentCluster.GetCouchbaseCluster()); isSpecInvalid(err) {
					errs = append(errs, err)
					currentCluster.Unreconcilable().MarkBucket(update.BucketName,
						unreconcilable.ReasonValidationFailed, err.Error())
				}
			}
		case *couchbasev2.CouchbaseEphemeralBucket:
			if t2, ok := newBucket.(*couchbasev2.CouchbaseEphemeralBucket); ok {
				if err := validationv2.CheckChangeConstraintsEphemeralBucket(rv.v, t1, t2, currentCluster.GetCouchbaseCluster()); isSpecInvalid(err) {
					errs = append(errs, err)
					currentCluster.Unreconcilable().MarkBucket(update.BucketName,
						unreconcilable.ReasonValidationFailed, err.Error())
				}
			}

		default:
			// It must be a couchbase bucket so continue if it's somehow something else.
			continue
		}
	}

	return errs
}

// reconcileValidator bundles the cluster's pre-initialised validator with its k8s
// client so that validation methods can be run as part of reconciliation. It is
// cheap to construct (two pointer fields) — the underlying validator is created
// once in cluster.New and reused across all reconcile cycles.
type reconcileValidator struct {
	v   *types.Validator
	k8s *client.Client

	// tracker is the owning cluster's unreconcilable tracker. It is nil on the
	// paths that validate the CouchbaseCluster itself rather than its dependent
	// resources, and the tracker's methods are happy to be called anyway.
	tracker *unreconcilable.Tracker
}

// markUnreconcilable records a constraint-validation failure against the
// resource that caused it, and suppresses that resource for the rest of the
// cycle.
func (rv *reconcileValidator) markUnreconcilable(kind, name string, err error) {
	rv.tracker.Mark(unreconcilable.Ref{Kind: kind, Name: name}, unreconcilable.ReasonValidationFailed, err.Error())
}

func CheckCouchbaseClusterResource(v *types.Validator, c *client.Client, couchbase *couchbasev2.CouchbaseCluster) ([]string, error) {
	rv := &reconcileValidator{v: v, k8s: c}
	return rv.CheckCouchbaseClusterResource(couchbase)
}

func CheckClusterChangeConstraints(v *types.Validator, c *client.Client, current, updated *couchbasev2.CouchbaseCluster) error {
	rv := &reconcileValidator{v: v, k8s: c}
	return rv.CheckClusterChangeConstraints(current, updated)
}

// CheckManagedResourceConstraints validates each dependent resource's spec and
// marks the offenders into the cluster's unreconcilable tracker.
//
// Buckets are deliberately reported but not marked. A bucket failing these
// checks has never been skipped before, and marking it here would newly stop
// reconciling buckets that work perfectly well today. Buckets get marked only
// from the change-constraint and immutable-field entry points.
//
// The returned errors are for logging only. See validateManagedResources.
func CheckManagedResourceConstraints(c *cluster.Cluster) []error {
	var errs []error

	if c.GetCouchbaseCluster().Spec.Paused {
		return nil
	}

	rv := &reconcileValidator{v: c.GetValidator(), k8s: c.GetK8sClient(), tracker: c.Unreconcilable()}

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

		if err := rv.checkCouchbaseScopeGroupResourceConstraints(validatable(scopeGroup)); isSpecInvalid(err) {
			rv.markUnreconcilable(couchbasev2.ScopeGroupCRDResourceKind, scopeGroup.Name, err)

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

		if err := rv.checkCouchbaseScopeResourceConstraints(validatable(scope)); isSpecInvalid(err) {
			rv.markUnreconcilable(couchbasev2.ScopeCRDResourceKind, scope.Name, err)

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

		if err := rv.checkCouchbaseCollectionGroupResourceConstraints(validatable(collectionGroup)); isSpecInvalid(err) {
			rv.markUnreconcilable(couchbasev2.CollectionGroupCRDResourceKind, collectionGroup.Name, err)

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

		if err := rv.checkCouchbaseCollectionResourceConstraints(validatable(collection)); isSpecInvalid(err) {
			rv.markUnreconcilable(couchbasev2.CollectionCRDResourceKind, collection.Name, err)

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

		if err := rv.checkCouchbaseBackupRestoreResourceConstraints(validatable(backupRestore)); isSpecInvalid(err) {
			rv.markUnreconcilable(couchbasev2.BackupRestoreCRDResourceKind, backupRestore.Name, err)

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

		if err := rv.checkCouchbaseGroupResourceConstraints(validatable(group)); isSpecInvalid(err) {
			rv.markUnreconcilable(couchbasev2.GroupCRDResourceKind, group.Name, err)

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

		if err := rv.checkCouchbaseBackupResourceConstraints(validatable(backup)); isSpecInvalid(err) {
			rv.markUnreconcilable(couchbasev2.BackupCRDResourceKind, backup.Name, err)

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

		if err := rv.checkCouchbaseUserResourceConstraints(validatable(user)); isSpecInvalid(err) {
			rv.markUnreconcilable(couchbasev2.UserCRDResourceKind, user.Name, err)

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

		if err := rv.checkCouchbaseBucketsConstraints(bucket, cluster); isSpecInvalid(err) {
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

		if err := rv.checkMemcachedBucketsConstraints(bucket, cluster); isSpecInvalid(err) {
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

		if err := rv.checkEphemeralBucketConstraints(bucket, cluster); isSpecInvalid(err) {
			errs = append(errs, err)
		}
	}

	return errs
}

func (rv *reconcileValidator) checkEphemeralBucketConstraints(bucket *couchbasev2.CouchbaseEphemeralBucket, cluster *couchbasev2.CouchbaseCluster) error {
	_, err := validationv2.CheckConstraintsEphemeralBucket(rv.v, bucket, cluster)
	return err
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

// validatable returns a copy of a cached resource with the admission-controller
// bypass annotation forced off.
//
// The constraint validators read the same annotation the admission controller
// does. Without this, someone who set it merely to get a resource admitted
// would quietly switch off the Operator's own judgement of that resource too.
// Copying first keeps the override well away from the shared informer cache.
func validatable[T interface{ DeepCopy() T }](resource T) T {
	copied := resource.DeepCopy()

	object, ok := any(copied).(metav1.Object)
	if !ok {
		return copied
	}

	annotations := object.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}

	annotations[constants.AnnotationDisableAdmissionController] = "false"
	object.SetAnnotations(annotations)

	return copied
}

func shouldSkipValidation(annotations map[string]string) bool {
	skipValidation, found := annotations[constants.AnnotationSkipDACValidation]
	return found && strings.EqualFold(skipValidation, "true")
}

func checkIsMemberError(err error) bool {
	return goerrors.Is(err, couchbaseutil.ErrMemberError)
}

// ValidateXDCRReplicationBuckets checks that every XDCR replication's source
// bucket exists, marking the ones that fail so that only they get skipped. Its
// errors are for logging only.
func ValidateXDCRReplicationBuckets(currentCluster *cluster.Cluster) []error {
	cbCluster := currentCluster.GetCouchbaseCluster()

	if !cbCluster.Spec.XDCR.Managed {
		return nil
	}

	rv := &reconcileValidator{v: currentCluster.GetValidator(), k8s: currentCluster.GetK8sClient(), tracker: currentCluster.Unreconcilable()}

	var errs []error

	failed, err := validationv2.CheckConstraintXDCRReplicationBucketsForReconcile(rv.v, cbCluster)
	if err != nil {
		errs = append(errs, err)
	}

	for _, replication := range failed {
		rv.tracker.Mark(
			unreconcilable.Ref{Kind: replication.Kind, Name: replication.Name},
			unreconcilable.ReasonDependencyMissing, replication.Message)
	}

	return errs
}

// ValidateBucketsInAbeyance holds the buckets that are valid in themselves but
// cannot be applied to the live cluster yet.
func ValidateBucketsInAbeyance(currentCluster *cluster.Cluster) []error {
	cbCluster := currentCluster.GetCouchbaseCluster()

	if cbCluster.Spec.Paused || !cbCluster.Spec.Buckets.Managed {
		return nil
	}

	rv := &reconcileValidator{v: currentCluster.GetValidator(), k8s: currentCluster.GetK8sClient(), tracker: currentCluster.Unreconcilable()}

	var errs []error

	errs = append(errs, rv.holdBucketsOverMemoryQuota(currentCluster)...)
	errs = append(errs, rv.holdBucketsBelowMinReplicas(cbCluster)...)
	errs = append(errs, rv.holdBucketsMissingEncryptionKeys(cbCluster)...)

	return errs
}

// holdBucketsOverMemoryQuota holds any bucket whose creation or resize would push
// the cluster's total bucket memory past spec.cluster.dataServiceMemoryQuota.
func (rv *reconcileValidator) holdBucketsOverMemoryQuota(currentCluster *cluster.Cluster) []error {
	quota := currentCluster.GetCouchbaseCluster().Spec.ClusterSettings.DataServiceMemQuota
	if quota == nil {
		return nil
	}

	allocations, err := currentCluster.GetBucketMemoryAllocations()
	if err != nil {
		if isIndeterminateError(err) {
			return nil
		}

		return []error{err}
	}

	return rv.markHeldBuckets(bucketsOverMemoryQuota(allocations, k8sutil.Megabytes(quota)),
		unreconcilable.ReasonQuotaExceeded)
}

// holdBucketsBelowMinReplicas holds any bucket asking for fewer replicas than
// spec.cluster.data.minReplicasCount allows.
func (rv *reconcileValidator) holdBucketsBelowMinReplicas(cbCluster *couchbasev2.CouchbaseCluster) []error {
	held, err := validationv2.CheckConstraintBucketReplicaCountsForReconcile(rv.v, cbCluster)
	if isIndeterminateError(err) {
		return nil
	}

	errs := rv.markHeldBuckets(held, unreconcilable.ReasonValidationFailed)

	if err != nil {
		errs = append(errs, err)
	}

	return errs
}

// holdBucketsMissingEncryptionKeys holds any bucket naming a CouchbaseEncryptionKey
// that does not exist yet.
func (rv *reconcileValidator) holdBucketsMissingEncryptionKeys(cbCluster *couchbasev2.CouchbaseCluster) []error {
	held, err := validationv2.CheckConstraintBucketEncryptionKeysForReconcile(rv.v, cbCluster)
	if isIndeterminateError(err) {
		return nil
	}

	errs := rv.markHeldBuckets(held, unreconcilable.ReasonDependencyMissing)

	if err != nil {
		errs = append(errs, err)
	}

	return errs
}

// markHeldBuckets suppresses each held bucket for the rest of this cycle under the
// given reason, and hands its message back for the log.
func (rv *reconcileValidator) markHeldBuckets(held []validationv2.UnreconcilableBucket, reason unreconcilable.Reason) []error {
	var errs []error

	for _, bucket := range held {
		rv.tracker.MarkBucket(bucket.BucketName, reason, bucket.Message)

		errs = append(errs, goerrors.New(bucket.Message))
	}

	return errs
}

// bucketsOverMemoryQuota decides which pending bucket creates and resizes have to
// wait for room within quotaMB.
func bucketsOverMemoryQuota(allocations []cluster.BucketMemoryAllocation, quotaMB int64) []validationv2.UnreconcilableBucket {
	var committed int64

	pending := make([]cluster.BucketMemoryAllocation, 0, len(allocations))

	for _, allocation := range allocations {
		if allocation.Exists {
			committed += allocation.AllocatedMB

			// Unchanged, or shrinking. Either way there is nothing to hold, and it
			// asks for no more than the server has already given it.
			if allocation.RequestedMB <= allocation.AllocatedMB {
				continue
			}
		}

		pending = append(pending, allocation)
	}

	if len(pending) == 0 {
		return nil
	}

	sort.Slice(pending, func(i, j int) bool { return pending[i].BucketName < pending[j].BucketName })

	headroom := quotaMB - committed

	var held []validationv2.UnreconcilableBucket

	for _, allocation := range pending {
		increase := allocation.RequestedMB - allocation.AllocatedMB

		if increase <= headroom {
			headroom -= increase

			continue
		}

		held = append(held, validationv2.UnreconcilableBucket{
			BucketName: allocation.BucketName,
			Message: fmt.Sprintf("bucket %s needs a further %dMi of the cluster's %dMi data service memory quota, of which only %dMi is unallocated; "+
				"the Operator is holding this bucket until the total fits — raise spec.cluster.dataServiceMemoryQuota, or reduce another bucket's spec.memoryQuota",
				allocation.BucketName, increase, quotaMB, max(headroom, 0)),
		})
	}

	return held
}
