/*
Copyright 2019-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2eutil

import (
	"context"
	"testing"
	"time"

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
	// Check port 389 and 636
}
