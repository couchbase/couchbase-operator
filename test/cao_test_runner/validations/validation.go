package validations

import (
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
	oldvalidators "github.com/couchbase/couchbase-operator/test/cao_test_runner/validations/old_validators"
)

const (
	Pre  = "PRE"
	Post = "POST"
)

type Validator interface {
	GetState() string
	Run(ctx *context.Context, testAssets assets.TestAssetGetterSetter) error
}

func RegisterValidators() map[string]Validator {
	return map[string]Validator{
		"couchbaseReadiness":   &oldvalidators.CouchbaseReadiness{},
		"collectLogs":          &oldvalidators.CollectLogs{},
		"preFlight":            &oldvalidators.PreFlight{},
		"couchbaseVersion":     &oldvalidators.CBVersion{},
		"couchbaseClusterSize": &oldvalidators.CouchbaseClusterSize{},
		"kubeconfigContext":    &oldvalidators.KubeConfigValidator{},
	}
}
