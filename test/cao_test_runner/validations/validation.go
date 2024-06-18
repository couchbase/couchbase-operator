package validations

import (
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
)

const (
	Pre  = "PRE"
	Post = "POST"
)

type Validator interface {
	GetState() string
	Run(ctx *context.Context) error
}

func RegisterValidators() map[string]Validator {
	return map[string]Validator{
		"couchbaseReadiness": &CouchbaseReadiness{},
		"collectLogs":        &CollectLogs{},
		"preFlight":          &PreFlight{},
		"couchbaseVersion":   &CBVersion{},
	}
}
