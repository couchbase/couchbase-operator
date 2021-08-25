// +build redhat

package certification

import (
	"github.com/couchbase/couchbase-operator/pkg/version"
)

var (
	imageRepo    = "registry.connect.redhat.com"
	imageDefault = imageRepo + "/couchbase/platform-certification:" + version.WithRevision()
)
