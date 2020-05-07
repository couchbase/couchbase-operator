package errors

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/couchbase/couchbase-operator/pkg/util/constants"

	v1 "k8s.io/api/core/v1"
)

var (
	ErrClusterCreating       = errors.New("existing cluster failed during prior initialization, state unknown")
	ErrUnknownCreatePod      = errors.New("unknown error occurred creating pod")
	ErrResourceLabelMismatch = errors.New("resource does not match label selection")
)

type rebalanceNotObservedError struct{}

func (e rebalanceNotObservedError) Error() string {
	return "rebalance task not observed as running"
}

var RebalanceNotObservedError error = rebalanceNotObservedError{}

type ErrSecretMissingUsername struct {
	Reason string
}

type ErrSecretMissingPassword struct {
	Reason string
}

type ErrCreatingPod struct {
	Reason string
}

type ErrDeletingPod struct {
	Reason string
}

type ErrRunningPod struct {
	Reason string
}

type ErrInvalidBucketParamChange struct {
	Bucket string
	Param  string
	From   interface{}
	To     interface{}
}

type ErrPodUnschedulable struct {
	Reason string
}

type ErrUnsupportedVersion struct {
	Version string
}

type ErrCreatingPersistentVolumeClaim struct {
	Reason string
}

type ErrCreatingPersistentVolume struct {
	Reason string
}

// Errors for volume health checking
type ErrNoVolumeMounts struct{}

type ErrVolumeClaimPending struct {
	Path  string
	Phase v1.PersistentVolumeClaimPhase
}

type ErrVolumeClaimLost struct {
	Path  string
	Phase v1.PersistentVolumeClaimPhase
}

type ErrVolumeClaimUnknownPhase struct {
	Path  string
	Phase v1.PersistentVolumeClaimPhase
}

type ErrVolumeClaimMissing struct {
	Path string
}

type ErrVolumeUnexpectedPhase struct {
	Path  string
	Phase v1.PersistentVolumePhase
}

type ErrVolumeMissingGroup struct {
	VolumeName string
}

func NewErrVolumeMissingGroup(name string) error {
	return &ErrVolumeMissingGroup{
		VolumeName: name,
	}
}

func (e ErrVolumeMissingGroup) Error() string {
	return fmt.Sprintf("volume `%s` is not labeled with a server group", e.VolumeName)
}

func IsErrVolumeMissingGroup(err error) bool {
	_, ok := err.(*ErrVolumeMissingGroup)
	return ok
}

// ErrUnknownMember is used when mapping a pod to a member and the
// member is unknown
type ErrUnknownMember struct {
	member string
}

// Error returns an error string.
func (e ErrUnknownMember) Error() string {
	return fmt.Sprintf("member is unknown: %s", e.member)
}

// NewErrUnknownMember creates a new unknown member error.
func NewErrUnknownMember(member string) error {
	return &ErrUnknownMember{member: member}
}

// IsErrUnknownMember returns whether the error is an unknown member.
func IsErrUnknownMember(err error) bool {
	_, ok := err.(*ErrUnknownMember)
	return ok
}

// RebalanceIncompleteError is used when a rebalance operation did not
// complete successfully and the cluster is unbalanced
type rebalanceIncompleteError struct{}

func (e rebalanceIncompleteError) Error() string {
	return "rebalance did not complete, cluster is unbalanced"
}

func NewRebalanceIncompleteError() error {
	return &rebalanceIncompleteError{}
}

func IsRebalanceIncompleteError(err error) bool {
	_, ok := err.(*rebalanceIncompleteError)
	return ok
}

// ErrUnknownServerClass is used when mapping a class name to a server
// configuration and the configuration does not exist
type ErrUnknownServerClass struct {
	serverClass string
}

// Error returns an error string.
func (e ErrUnknownServerClass) Error() string {
	return fmt.Sprintf("server class is unknown: %s", e.serverClass)
}

// NewErrUnknownServerClass returns a new new missing.
func NewErrUnknownServerClass(serverClass string) error {
	return &ErrUnknownServerClass{serverClass: serverClass}
}

// IsErrUnknownServerClass returns whether the error is an unknown server class.
func IsErrUnknownServerClass(err error) bool {
	_, ok := err.(*ErrUnknownServerClass)
	return ok
}

func (e ErrSecretMissingUsername) Error() string {
	return fmt.Sprintf("secret is missing username key: %s", e.Reason)
}

func (e ErrSecretMissingPassword) Error() string {
	return fmt.Sprintf("secret is missing password key: %s", e.Reason)
}

func (e ErrCreatingPod) Error() string {
	return fmt.Sprintf("failed to create pod: %s", e.Reason)
}

func (e ErrDeletingPod) Error() string {
	return fmt.Sprintf("failed to delete pod: %s", e.Reason)
}

func (e ErrRunningPod) Error() string {
	return fmt.Sprintf("failed to run pod: %s", e.Reason)
}

func (e ErrInvalidBucketParamChange) Error() string {
	fromStr := "unset"
	toStr := "unset"

	if hasValue(e.From) {
		fromStr = reflect.Indirect(reflect.ValueOf(e.From)).String()
	}

	if hasValue(e.To) {
		toStr = reflect.Indirect(reflect.ValueOf(e.To)).String()
	}

	return fmt.Sprintf("cannot change (%s) bucket param='%s' from '%s' to '%s'",
		e.Bucket, e.Param, fromStr, toStr)
}

func (e ErrPodUnschedulable) Error() string {
	return fmt.Sprintf("unable to schedule pod: %s", e.Reason)
}

func (e ErrUnsupportedVersion) Error() string {
	return fmt.Sprintf("version not supported: %s, min version: %s", e.Version, constants.CouchbaseVersionMin)
}

func (e ErrCreatingPersistentVolumeClaim) Error() string {
	return fmt.Sprintf("failed to create persistent volume claim: %s", e.Reason)
}

func (e ErrCreatingPersistentVolume) Error() string {
	return fmt.Sprintf("failed to create persistent volume: %s", e.Reason)
}

func (e ErrNoVolumeMounts) Error() string {
	return fmt.Sprintf("No volume mounts defined")
}

func (e ErrVolumeClaimLost) Error() string {
	return fmt.Sprintf("PersistentVolumeClaim for path %s has lost it's underlying PersistentVolume, Phase=`%s`", e.Path, e.Phase)
}

func (e ErrVolumeClaimPending) Error() string {
	return fmt.Sprintf("PersistentVolumeClaim for path %s is not yet bound to an underlying PersistentVolume, Phase=`%s`", e.Path, e.Phase)
}

func (e ErrVolumeClaimUnknownPhase) Error() string {
	return fmt.Sprintf("PersistentVolumeClaim for path %s is in an unknown phase `%s`", e.Path, e.Phase)
}

func (e ErrVolumeClaimMissing) Error() string {
	return fmt.Sprintf("Missing PersistentVolumeClaim for path %s", e.Path)
}

func (e ErrVolumeUnexpectedPhase) Error() string {
	return fmt.Sprintf("PersistentVolume for path %s is in an unexpected Phase: `%s`, expected `Bound`", e.Path, e.Phase)
}

func hasValue(v interface{}) bool {
	return reflect.ValueOf(v) != reflect.Zero(reflect.TypeOf(v))
}
