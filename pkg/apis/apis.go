package apis

import (
	couchbasev1 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"

	"k8s.io/apimachinery/pkg/runtime"
)

func AddToScheme(s *runtime.Scheme) error {
	schemeBuilders := runtime.SchemeBuilder{
		couchbasev1.AddToScheme,
		couchbasev2.AddToScheme,
	}

	return schemeBuilders.AddToScheme(s)
}
