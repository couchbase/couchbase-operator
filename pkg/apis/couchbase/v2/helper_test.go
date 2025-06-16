package v2

import (
	"testing"
	"time"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	LowVersionImage      = "couchbase/server:6.5.0"
	LowVersionHashImage  = "couchbase/server@sha256:b21765563ba510c0b1ca43bc9287567761d901b8d00fee704031e8f405bfa501"
	MedVersionImage      = "couchbase/server:7.0.1"
	MedVersionHashImage  = "couchbase/server@sha256:fa5d031059e005cd9d85983b1a120dab37fc60136cb699534b110f49d27388f7"
	HighVersionImage     = "couchbase/server:7.1.3"
	HighVersionHashImage = "couchbase/server@sha256:d0d1734a98fea7639793873d9a54c27d6be6e7838edad2a38e8d451d66be3497"
)

func TestIsNativeAuditCleanupEnabled(t *testing.T) {
	c := CouchbaseCluster{
		Spec: ClusterSpec{
			Logging: CouchbaseClusterLoggingSpec{
				Audit: &CouchbaseClusterAuditLoggingSpec{
					Enabled: true,
					Rotation: &CouchbaseClusterLogRotationSpec{
						PruneAge: &v1.Duration{Duration: 0 * time.Second},
					},
				},
			},
		},
	}

	if c.IsNativeAuditCleanupEnabled() {
		t.Error("expected IsNativeAuditCleanupEnabled to return false, but returned true")
	}

	c.Spec.Logging.Audit.Rotation.PruneAge = &v1.Duration{Duration: 10 * time.Second}

	if !c.IsNativeAuditCleanupEnabled() {
		t.Error("expected IsNativeAuditCleanupEnabled to return true, but returned false")
	}
}
