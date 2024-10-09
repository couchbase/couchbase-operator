package actions

import (
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
)

type Action interface {
	Describe() string
	Do(ctx *context.Context, testAssets assets.TestAssetGetter) error
	Config() interface{}
	CheckConfig() error
	RunValidators(ctx *context.Context, state string, testAssets assets.TestAssetGetterSetter) error
}
