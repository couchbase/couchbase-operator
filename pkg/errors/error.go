package errors

import (
	E "errors"
	"fmt"
	"reflect"

	"github.com/couchbase/couchbase-operator/pkg/util/constants"
)

var (
	ErrClusterCreating = E.New("existing cluster failed during prior initialization, state unknown")
	ErrUnkownCreatePod = E.New("unkown error occurred creating pod")
)

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

func hasValue(v interface{}) bool {
	return reflect.ValueOf(v) != reflect.Zero(reflect.TypeOf(v))
}
