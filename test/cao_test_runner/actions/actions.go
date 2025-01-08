package actions

import (
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
)

type Action interface {
	Describe() string
	Do(ctx *context.Context) error
	Config() interface{}
	CheckConfig() error
	RunValidators(ctx *context.Context, state string) error
}
