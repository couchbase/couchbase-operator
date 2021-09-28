//go:build !redhat
// +build !redhat

package certification

import (
	"github.com/couchbase/couchbase-operator/pkg/version"
)

const (
	useFSGroup = true
)

var (
	imageDefault = "couchbase/operator-certification:" + version.Version
)
