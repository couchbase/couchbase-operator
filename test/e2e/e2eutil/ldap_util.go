package e2eutil

import (
	"context"
	"testing"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/util/netutil"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NewLDAPServer creates a new LDAP server.
func NewLDAPServer(k8s *types.Cluster, pod *v1.Pod) (*v1.Pod, error) {
	return k8s.KubeClient.CoreV1().Pods(k8s.Namespace).Create(context.Background(), pod, metav1.CreateOptions{})
}

// MustNewLDAPServer create LDAP server or dies trying.
func MustNewLDAPServer(t *testing.T, k8s *types.Cluster, pod *v1.Pod) *v1.Pod {
	pod, err := NewLDAPServer(k8s, pod)
	if err != nil {
		Die(t, err)
	}

	return pod
}

// NewLDAPService creates headless service for accessing LDAP server.
func NewLDAPService(k8s *types.Cluster, service *v1.Service) (*v1.Service, error) {
	return k8s.KubeClient.CoreV1().Services(k8s.Namespace).Create(context.Background(), service, metav1.CreateOptions{})
}

// MustNewLDAPService creates LDAP service or dies trying.
func MustNewLDAPService(t *testing.T, k8s *types.Cluster, service *v1.Service) *v1.Service {
	service, err := NewLDAPService(k8s, service)
	if err != nil {
		Die(t, err)
	}

	return service
}

// MustCheckLDAPServer ensures the LDAP server is up and running before letting
// Couchbase loose with it.
func MustCheckLDAPServer(t *testing.T, k8s *types.Cluster, pod string, tls *TLSContext, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	callback := func() error {
		port, cleanup, err := forwardPort(k8s, k8s.Namespace, pod, "389")
		if err != nil {
			return err
		}

		defer cleanup()

		innerCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		cancel()

		if err := netutil.WaitForHostPort(innerCtx, "localhost:"+port); err != nil {
			return err
		}

		return nil
	}

	if err := retryutil.Retry(ctx, time.Second, callback); err != nil {
		Die(t, err)
	}

	callback = func() error {
		port, cleanup, err := forwardPort(k8s, k8s.Namespace, pod, "636")
		if err != nil {
			return err
		}

		defer cleanup()

		innerCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		cancel()

		if err := netutil.WaitForHostPortTLS(innerCtx, "localhost:"+port, tls.CA.Certificate); err != nil {
			return err
		}

		return nil
	}

	if err := retryutil.Retry(ctx, time.Second, callback); err != nil {
		Die(t, err)
	}
}
