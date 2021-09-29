//go:build redhat

package certification

import (
	"github.com/couchbase/couchbase-operator/pkg/version"
)

const (
	imageRepo  = "registry.connect.redhat.com"
	useFSGroup = false
)

var (
	imageDefault = imageRepo + "/couchbase/operator-certification:" + version.WithRevision()
)
