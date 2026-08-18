/*
Copyright 2026-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package v2

import (
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	// ConditionTypeUnreconcilable is the condition type reporting that the
	// Operator is skipping a dependent resource.
	ConditionTypeUnreconcilable = "Unreconcilable"

	// unreconcilableMessageSeparator separates the judging cluster's name from
	// the human readable detail within a condition message. A CouchbaseCluster
	// name is a DNS label, so it can never contain this separator itself.
	unreconcilableMessageSeparator = ": "

	// unreconcilableMessageFormat is built from the separator above, so the
	// writer and the readers of a message can never drift apart.
	unreconcilableMessageFormat = "%s" + unreconcilableMessageSeparator + "%s"
)

// UnreconcilableAware is implemented by every API kind that carries an
// Unreconcilable status condition.
//
// It hands over the whole conditions slice rather than a single condition, so
// the accessors below can be written once and work for every kind.
type UnreconcilableAware interface {
	metav1.Object
	runtime.Object

	GetConditions() []metav1.Condition
	SetConditions(conditions []metav1.Condition)
}

// UnreconcilableMessage builds the message for an Unreconcilable condition
// written by clusterName.
//
// A dependent resource can be selected by more than one CouchbaseCluster, and
// each of them reaches its own verdict. metav1.Condition has nowhere to record
// who made the judgement, so the cluster name rides along as a message prefix
// and each cluster owns the entry bearing its own. That is also why these lists
// are +listType=atomic rather than keyed on type, since more than one entry may
// quite legitimately be of type Unreconcilable.
func UnreconcilableMessage(clusterName, detail string) string {
	return fmt.Sprintf(unreconcilableMessageFormat, clusterName, detail)
}

// UnreconcilableClusterName returns the name of the CouchbaseCluster that wrote
// a condition, or the empty string if its message carries no cluster prefix.
func UnreconcilableClusterName(condition *metav1.Condition) string {
	clusterName, _, found := strings.Cut(condition.Message, unreconcilableMessageSeparator)
	if !found {
		return ""
	}

	return clusterName
}

// UnreconcilableDetail returns a condition's message with the judging cluster's
// name stripped off.
func UnreconcilableDetail(condition *metav1.Condition) string {
	_, detail, found := strings.Cut(condition.Message, unreconcilableMessageSeparator)
	if !found {
		return condition.Message
	}

	return detail
}

// IsUnreconcilableConditionUpToDate reports whether an object already carries exactly the
// verdict the named cluster is about to write, so writing it again would change
// nothing.
func IsUnreconcilableConditionUpToDate(object UnreconcilableAware, clusterName string, status metav1.ConditionStatus,
	reason, detail string, observedGeneration int64,
) bool {
	condition, found := GetUnreconcilable(object, clusterName)
	if !found {
		return false
	}

	return doesConditionMatch(condition, status, reason, UnreconcilableMessage(clusterName, detail),
		observedGeneration)
}

// doesConditionMatch reports whether a condition already says precisely this.
func doesConditionMatch(condition *metav1.Condition, status metav1.ConditionStatus,
	reason, message string, observedGeneration int64,
) bool {
	return condition.Status == status && condition.Reason == reason &&
		condition.Message == message && condition.ObservedGeneration == observedGeneration
}

// GetUnreconcilable returns the Unreconcilable condition recorded by the named
// CouchbaseCluster. The returned pointer aliases the object's own slice, so do
// not hold on to it across a SetConditions.
func GetUnreconcilable(object UnreconcilableAware, clusterName string) (*metav1.Condition, bool) {
	conditions := object.GetConditions()

	for i := range conditions {
		if isUnreconcilableFor(&conditions[i], clusterName) {
			return &conditions[i], true
		}
	}

	return nil, false
}

// SetUnreconcilable upserts the Unreconcilable condition for the named
// CouchbaseCluster, leaving every other entry untouched, and reports whether
// anything besides LastTransitionTime actually changed.
//
// Callers lean on that return value to skip the API write altogether.
func SetUnreconcilable(object UnreconcilableAware, clusterName string, status metav1.ConditionStatus,
	reason, detail string, observedGeneration int64, now metav1.Time,
) bool {
	message := UnreconcilableMessage(clusterName, detail)
	conditions := object.GetConditions()

	for i := range conditions {
		condition := &conditions[i]

		if !isUnreconcilableFor(condition, clusterName) {
			continue
		}

		if doesConditionMatch(condition, status, reason, message, observedGeneration) {
			return false
		}

		// Only a change of status counts as a transition.
		if condition.Status != status {
			condition.LastTransitionTime = now
		}

		condition.Status = status
		condition.Reason = reason
		condition.Message = message
		condition.ObservedGeneration = observedGeneration

		object.SetConditions(conditions)

		return true
	}

	object.SetConditions(append(conditions, metav1.Condition{
		Type:               ConditionTypeUnreconcilable,
		Status:             status,
		ObservedGeneration: observedGeneration,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	}))

	return true
}

// isUnreconcilableFor reports whether a condition is the Unreconcilable entry
// belonging to the named cluster.
func isUnreconcilableFor(condition *metav1.Condition, clusterName string) bool {
	return condition.Type == ConditionTypeUnreconcilable &&
		UnreconcilableClusterName(condition) == clusterName
}

// UnreconcilableAware implementations.
//
// CouchbaseRoleBinding and CouchbaseEncryptionKey are deliberately absent.
// Neither is validated in the reconcile loop, so neither has anything to say.
var (
	_ UnreconcilableAware = &CouchbaseBucket{}
	_ UnreconcilableAware = &CouchbaseEphemeralBucket{}
	_ UnreconcilableAware = &CouchbaseMemcachedBucket{}
	_ UnreconcilableAware = &CouchbaseScope{}
	_ UnreconcilableAware = &CouchbaseScopeGroup{}
	_ UnreconcilableAware = &CouchbaseCollection{}
	_ UnreconcilableAware = &CouchbaseCollectionGroup{}
	_ UnreconcilableAware = &CouchbaseUser{}
	_ UnreconcilableAware = &CouchbaseGroup{}
	_ UnreconcilableAware = &CouchbaseReplication{}
	_ UnreconcilableAware = &CouchbaseMigrationReplication{}
	_ UnreconcilableAware = &CouchbaseBackup{}
	_ UnreconcilableAware = &CouchbaseBackupRestore{}
	_ UnreconcilableAware = &CouchbaseAutoscaler{}
)

func (o *CouchbaseBucket) GetConditions() []metav1.Condition { return o.Status.Conditions }

func (o *CouchbaseBucket) SetConditions(conditions []metav1.Condition) {
	o.Status.Conditions = conditions
}

func (o *CouchbaseEphemeralBucket) GetConditions() []metav1.Condition { return o.Status.Conditions }

func (o *CouchbaseEphemeralBucket) SetConditions(conditions []metav1.Condition) {
	o.Status.Conditions = conditions
}

func (o *CouchbaseMemcachedBucket) GetConditions() []metav1.Condition { return o.Status.Conditions }

func (o *CouchbaseMemcachedBucket) SetConditions(conditions []metav1.Condition) {
	o.Status.Conditions = conditions
}

func (o *CouchbaseScope) GetConditions() []metav1.Condition { return o.Status.Conditions }

func (o *CouchbaseScope) SetConditions(conditions []metav1.Condition) {
	o.Status.Conditions = conditions
}

func (o *CouchbaseScopeGroup) GetConditions() []metav1.Condition { return o.Status.Conditions }

func (o *CouchbaseScopeGroup) SetConditions(conditions []metav1.Condition) {
	o.Status.Conditions = conditions
}

func (o *CouchbaseCollection) GetConditions() []metav1.Condition { return o.Status.Conditions }

func (o *CouchbaseCollection) SetConditions(conditions []metav1.Condition) {
	o.Status.Conditions = conditions
}

func (o *CouchbaseCollectionGroup) GetConditions() []metav1.Condition { return o.Status.Conditions }

func (o *CouchbaseCollectionGroup) SetConditions(conditions []metav1.Condition) {
	o.Status.Conditions = conditions
}

func (o *CouchbaseUser) GetConditions() []metav1.Condition { return o.Status.Conditions }

func (o *CouchbaseUser) SetConditions(conditions []metav1.Condition) {
	o.Status.Conditions = conditions
}

func (o *CouchbaseGroup) GetConditions() []metav1.Condition { return o.Status.Conditions }

func (o *CouchbaseGroup) SetConditions(conditions []metav1.Condition) {
	o.Status.Conditions = conditions
}

func (o *CouchbaseReplication) GetConditions() []metav1.Condition { return o.Status.Conditions }

func (o *CouchbaseReplication) SetConditions(conditions []metav1.Condition) {
	o.Status.Conditions = conditions
}

func (o *CouchbaseMigrationReplication) GetConditions() []metav1.Condition {
	return o.Status.Conditions
}

func (o *CouchbaseMigrationReplication) SetConditions(conditions []metav1.Condition) {
	o.Status.Conditions = conditions
}

func (o *CouchbaseBackup) GetConditions() []metav1.Condition { return o.Status.Conditions }

func (o *CouchbaseBackup) SetConditions(conditions []metav1.Condition) {
	o.Status.Conditions = conditions
}

func (o *CouchbaseBackupRestore) GetConditions() []metav1.Condition { return o.Status.Conditions }

func (o *CouchbaseBackupRestore) SetConditions(conditions []metav1.Condition) {
	o.Status.Conditions = conditions
}

func (o *CouchbaseAutoscaler) GetConditions() []metav1.Condition { return o.Status.Conditions }

func (o *CouchbaseAutoscaler) SetConditions(conditions []metav1.Condition) {
	o.Status.Conditions = conditions
}
