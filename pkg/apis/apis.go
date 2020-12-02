package apis

import (
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"

	"k8s.io/apimachinery/pkg/runtime"
)

func AddToScheme(s *runtime.Scheme) error {
	schemeBuilders := runtime.SchemeBuilder{
		couchbasev2.AddToScheme,
	}

	return schemeBuilders.AddToScheme(s)
}
