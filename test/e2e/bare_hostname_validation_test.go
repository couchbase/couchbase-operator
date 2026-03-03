/*
Copyright 2025-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2e

import (
	"fmt"
	"strings"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	util_x509 "github.com/couchbase/couchbase-operator/pkg/util/x509"

	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
)

// mustGetAdmissionFailureOnCreateCluster expects cluster creation to fail with a specific error message.
func mustGetAdmissionFailureOnCreateCluster(t *testing.T, k8s *types.Cluster, cluster *couchbasev2.CouchbaseCluster, rejectReason string) {
	if _, err := e2eutil.CreateCluster(k8s, cluster); err == nil || !strings.Contains(err.Error(), rejectReason) {
		e2eutil.Die(t, fmt.Errorf("expected admission error: %s, got: %w", rejectReason, err))
	}
}

// testBareHostnameValidationCase runs a single test case for bare hostname validation.
func testBareHostnameValidationCase(t *testing.T, kubernetes *types.Cluster, clusterSize int, includeBareHostnamesInCert, validateBareHostnamesInSpec, expectAdmissionFailure bool, expectedAdmissionFailureReason string) {
	// Use a consistent cluster name for both certificate generation and validation
	clusterName := "test-couchbase-" + e2eutil.RandomSuffix()

	// Create TLS context based on test case using the consistent cluster name
	tlsOpts := &e2eutil.TLSOpts{
		ClusterName: clusterName,
		AltNames:    util_x509.MandatorySANs(clusterName, kubernetes.Namespace, includeBareHostnamesInCert),
	}
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, tlsOpts)

	// Generate cluster spec with TLS and set the cluster name
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithTLS(ctx).Generate(kubernetes)
	cluster.Name = clusterName

	// Set ValidateBareHostnames after Generate() to avoid it being overwritten by applyTLS()
	if cluster.Spec.Networking.TLS == nil {
		cluster.Spec.Networking.TLS = &couchbasev2.TLSPolicy{}
	}

	cluster.Spec.Networking.TLS.ValidateBareHostnames = validateBareHostnamesInSpec

	if expectAdmissionFailure {
		// This scenario expects cluster creation to fail at admission
		mustGetAdmissionFailureOnCreateCluster(t, kubernetes, cluster, expectedAdmissionFailureReason)
	} else {
		// This scenario expects cluster to be created successfully
		cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

		// Verify cluster becomes healthy and TLS checks pass
		e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 2*time.Minute)
		e2eutil.MustCheckClusterTLS(t, kubernetes, cluster, ctx, 5*time.Minute)
	}
}

func TestBareHostnameValidation(t *testing.T) {
	f := framework.Global
	kubernetes, cleanup := f.SetupTestExclusive(t)

	defer cleanup()

	clusterSize := constants.Size3

	t.Run("Default_WithBareHostname_ShouldSucceed", func(t *testing.T) {
		cleanup := f.SetupSubTest(t)
		defer cleanup()

		testBareHostnameValidationCase(t, kubernetes, clusterSize, true, true, false, "")
	})

	t.Run("Default_WithoutBareHostname_ShouldFail", func(t *testing.T) {
		cleanup := f.SetupSubTest(t)
		defer cleanup()

		testBareHostnameValidationCase(t, kubernetes, clusterSize, false, true, true, "certificate cannot be verified for zone")
	})

	t.Run("Disabled_WithBareHostname_ShouldSucceed", func(t *testing.T) {
		cleanup := f.SetupSubTest(t)
		defer cleanup()

		testBareHostnameValidationCase(t, kubernetes, clusterSize, true, false, false, "")
	})

	t.Run("Disabled_WithoutBareHostname_ShouldSucceed", func(t *testing.T) {
		cleanup := f.SetupSubTest(t)
		defer cleanup()

		testBareHostnameValidationCase(t, kubernetes, clusterSize, false, false, false, "")
	})
}
