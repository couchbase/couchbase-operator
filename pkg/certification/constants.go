// +build !redhat

package certification

import (
	"github.com/couchbase/couchbase-operator/pkg/version"
)

var (
	imageDefault = "couchbase/operator-certification:" + version.Version
)
