package actions

import (
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
)

type Action interface {
	Describe() string
	Do(ctx *context.Context, config interface{}) error
	Config() interface{}
	Checks(ctx *context.Context, config interface{}, state string) error
}
