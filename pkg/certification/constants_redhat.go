//go:build redhat

package certification

import (
	"github.com/couchbase/couchbase-operator/pkg/version"

	corev1 "k8s.io/api/core/v1"
)

const (
	imageRepo              = "registry.connect.redhat.com"
	useFSGroup             = false
	imagePullPolicyDefault = string(corev1.PullIfNotPresent)
)

var imageDefault = imageRepo + "/couchbase/operator-certification:" + version.WithRevision()
