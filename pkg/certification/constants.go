//go:build !redhat
// +build !redhat

package certification

import (
	"github.com/couchbase/couchbase-operator/pkg/version"

	corev1 "k8s.io/api/core/v1"
)

const (
	useFSGroup             = true
	imagePullPolicyDefault = string(corev1.PullIfNotPresent)
)

var imageDefault = "couchbase/operator-certification:" + version.Version
