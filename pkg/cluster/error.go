package cluster

import (
	"errors"
	"fmt"
	"reflect"
)

var (
	errCreatedCluster  = errors.New("cluster failed to be created")
	errUnkownCreatePod = errors.New("unkown error occurred creating pod")
)

type ErrSecretMissingUsername struct {
	reason string
}

type ErrSecretMissingPassword struct {
	reason string
}

type ErrCreatingPod struct {
	reason string
}

type ErrRunningPod struct {
	reason string
}

type ErrInvalidBucketParamChange struct {
	bucket string
	param  string
	from   interface{}
	to     interface{}
}

func (e ErrSecretMissingUsername) Error() string {
	return fmt.Sprintf("secret is missing username key: %s", e.reason)
}

func (e ErrSecretMissingPassword) Error() string {
	return fmt.Sprintf("secret is missing password key: %s", e.reason)
}

func (e ErrCreatingPod) Error() string {
	return fmt.Sprintf("failed to create pod: %s", e.reason)
}

func (e ErrRunningPod) Error() string {
	return fmt.Sprintf("failed to run pod: %s", e.reason)
}

func (e ErrInvalidBucketParamChange) Error() string {
	fromStr := "unset"
	toStr := "unset"
	if hasValue(e.from) {
		fromStr = reflect.Indirect(reflect.ValueOf(e.from)).String()
	}
	if hasValue(e.to) {
		toStr = reflect.Indirect(reflect.ValueOf(e.to)).String()
	}

	return fmt.Sprintf("cannot change (%s) bucket param='%s' from '%s' to '%s'",
		e.bucket, e.param, fromStr, toStr)
}

func hasValue(v interface{}) bool {
	return reflect.ValueOf(v) != reflect.Zero(reflect.TypeOf(v))
}
