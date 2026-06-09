/*
Copyright 2019-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2e

import (
	"fmt"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/x509"

	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/util"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	domain    = "acme.com"
	newDomain = "ajax.com"
)

// Data service ports.
const (
	dataServicePort    = 11210
	dataServicePortTLS = 11207
)

// TestExposedFeatureIP tests alternate addresses are populated with IP addresses with
// a basic cluster.
func TestExposedFeatureIP(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size1

	// Repeat for the various topologies we want to test
	testCases := []*e2eutil.ClusterOptions{
		clusterOptions().WithEphemeralTopology(clusterSize),
		clusterOptions().WithMixedEphemeralTopology(clusterSize),
		clusterOptions().WithSplitEphemeralTopology(clusterSize),
	}

	for _, options := range testCases {
		// Create the cluster.
		bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
		e2eutil.MustNewBucket(t, kubernetes, bucket)

		cluster := options.Generate(kubernetes)
		cluster.Spec.Networking.ExposedFeatures = couchbasev2.ExposedFeatureList{
			couchbasev2.FeatureClient,
		}
		cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

		// Verify that all nodes advertise an IP based alternate address.
		e2eutil.MustCheckForIPAlternateAddresses(t, kubernetes, cluster, 2*time.Minute)
		e2eutil.MustCheckForNodeServiceType(t, kubernetes, cluster, corev1.ServiceTypeNodePort, time.Minute)
		e2eutil.MustCheckServicePorts(t, kubernetes, cluster, time.Minute)
		e2eutil.MustDeleteBucket(t, kubernetes, bucket)
	}
}

// TestExposedFeatureDNS tests alternate addresses are populated with DNS addresses with
// a DNS enabled cluster.
func TestExposedFeatureDNS(t *testing.T) {
	t.Skip("requires DDNS - addressability checks will fail without this")

	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterName := "test-couchbase-" + e2eutil.RandomSuffix()
	clusterSize := constants.Size1

	// Create the cluster.
	tlsOptions := &e2eutil.TLSOpts{
		ClusterName: clusterName,
		ExtraAltNames: []string{
			fmt.Sprintf("*.%s", domain),
		},
	}

	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, tlsOptions)

	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Name = clusterName
	cluster.Spec.Networking.ExposedFeatures = couchbasev2.ExposedFeatureList{
		couchbasev2.FeatureClient,
	}
	cluster.Spec.Networking.ExposedFeatureServiceType = corev1.ServiceTypeLoadBalancer
	cluster.Spec.Networking.DNS = &couchbasev2.DNS{
		Domain: domain,
	}
	cluster.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
	}
	cluster.Spec.Networking.ExposedFeatures = []couchbasev2.ExposedFeature{
		couchbasev2.FeatureClient,
	}
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Verify that all nodes advertise a DNS based alternate address.
	e2eutil.MustCheckForDNSAlternateAddresses(t, cluster, domain, time.Minute)
	e2eutil.MustCheckForDNSServiceAnnotations(t, kubernetes, cluster, domain, time.Minute)
	e2eutil.MustCheckForNodeServiceType(t, kubernetes, cluster, corev1.ServiceTypeLoadBalancer, time.Minute)

	// Verify console service exposes data ports.
	expectedPorts := &[]int32{dataServicePortTLS}
	rejectedPorts := &[]int32{dataServicePort}
	e2eutil.MustCheckConsolePorts(t, kubernetes, cluster, expectedPorts, rejectedPorts)
}

func TestExposedFeatureExternalConnection(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size3

	// Repeat for the various topologies we want to test
	testCases := []*e2eutil.ClusterOptions{
		clusterOptions().WithEphemeralTopology(clusterSize),
		clusterOptions().WithMixedEphemeralTopology(clusterSize),
		clusterOptions().WithSplitEphemeralTopology(clusterSize),
	}

	for _, options := range testCases {
		// Create the cluster.
		bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
		e2eutil.MustNewBucket(t, kubernetes, bucket)

		cluster := options.Generate(kubernetes)
		cluster.Spec.Networking.ExposedFeatures = couchbasev2.ExposedFeatureList{
			couchbasev2.FeatureClient,
			couchbasev2.FeatureAdmin,
			couchbasev2.FeatureExternalConnection,
		}

		cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

		// Verify that all nodes advertise an IP based alternate address.
		e2eutil.MustCheckForIPAlternateAddresses(t, kubernetes, cluster, 2*time.Minute)
		e2eutil.MustCheckForNodeServiceType(t, kubernetes, cluster, corev1.ServiceTypeNodePort, time.Minute)
		e2eutil.MustCheckServicePorts(t, kubernetes, cluster, time.Minute)
		e2eutil.MustDeleteBucket(t, kubernetes, bucket)
	}
}

// TestExposedFeatureDNSModify tests modifications to the DNS configuration are mirrored by
// node services.
func TestExposedFeatureDNSModify(t *testing.T) {
	t.Skip("requires DDNS - addressability checks will fail without this")

	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size1

	// Create the cluster.
	clusterName := "test-couchbase-" + e2eutil.RandomSuffix()
	tlsOptions := &e2eutil.TLSOpts{
		ClusterName: clusterName,
		ExtraAltNames: []string{
			fmt.Sprintf("*.%s", domain),
		},
	}

	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, tlsOptions)

	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Name = clusterName
	cluster.Spec.Networking.ExposedFeatures = couchbasev2.ExposedFeatureList{
		couchbasev2.FeatureClient,
	}
	cluster.Spec.Networking.ExposedFeatureServiceType = corev1.ServiceTypeLoadBalancer
	cluster.Spec.Networking.DNS = &couchbasev2.DNS{
		Domain: domain,
	}
	cluster.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
	}
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Verify that all nodes advertise a DNS based alternate address, and it changes when updated.
	e2eutil.MustCheckForDNSAlternateAddresses(t, cluster, domain, time.Minute)
	e2eutil.MustCheckForDNSServiceAnnotations(t, kubernetes, cluster, domain, time.Minute)
	e2eutil.MustCheckForNodeServiceType(t, kubernetes, cluster, corev1.ServiceTypeLoadBalancer, time.Minute)
	subjectAltNames := x509.MandatorySANs(cluster.Name, cluster.Namespace, true, true)
	subjectAltNames = append(subjectAltNames, fmt.Sprintf("*.%s", newDomain))
	opts := e2eutil.TLSOpts{
		AltNames: subjectAltNames,
	}
	e2eutil.MustRotateServerCertificate(t, ctx, &opts)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/networking/dns/domain", newDomain), time.Minute)
	e2eutil.MustCheckForDNSAlternateAddresses(t, cluster, newDomain, 5*time.Minute)
	e2eutil.MustCheckForDNSServiceAnnotations(t, kubernetes, cluster, newDomain, time.Minute)
	e2eutil.MustCheckForNodeServiceType(t, kubernetes, cluster, corev1.ServiceTypeLoadBalancer, time.Minute)
}

// TestExposedFeatureServiceTypeModify tests modifications to the node service type are mirrored
// by the node services.
func TestExposedFeatureServiceTypeModify(t *testing.T) {
	t.Skip("requires DDNS - addressability checks will fail without this")

	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size1

	// Create the cluster.
	clusterName := "test-couchbase-" + e2eutil.RandomSuffix()
	tlsOptions := &e2eutil.TLSOpts{
		ClusterName: clusterName,
		ExtraAltNames: []string{
			fmt.Sprintf("*.%s", domain),
		},
	}

	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, tlsOptions)

	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Name = clusterName
	cluster.Spec.Networking.ExposedFeatures = couchbasev2.ExposedFeatureList{
		couchbasev2.FeatureClient,
	}
	cluster.Spec.Networking.ExposedFeatureServiceType = corev1.ServiceTypeLoadBalancer
	cluster.Spec.Networking.DNS = &couchbasev2.DNS{
		Domain: domain,
	}
	cluster.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
	}
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Verify that changing the node port type is reflected in the services.
	e2eutil.MustCheckForNodeServiceType(t, kubernetes, cluster, corev1.ServiceTypeLoadBalancer, time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/networking/exposedFeatureServiceType", corev1.ServiceTypeNodePort), time.Minute)
	e2eutil.MustCheckForNodeServiceType(t, kubernetes, cluster, corev1.ServiceTypeNodePort, time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/networking/exposedFeatureServiceType", corev1.ServiceTypeLoadBalancer), time.Minute)
	e2eutil.MustCheckForNodeServiceType(t, kubernetes, cluster, corev1.ServiceTypeLoadBalancer, time.Minute)
}

// TestConsoleServiceDNS tests the admin console service DNS annotation is set when
// DNS is configured.
func TestConsoleServiceDNS(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterName := "test-couchbase-" + e2eutil.RandomSuffix()
	clusterSize := constants.Size1

	// Create the cluster.
	tlsOptions := &e2eutil.TLSOpts{
		ClusterName: clusterName,
		ExtraAltNames: []string{
			fmt.Sprintf("*.%s", domain),
		},
	}

	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, tlsOptions)

	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Name = clusterName
	cluster.Spec.Networking.ExposeAdminConsole = true
	cluster.Spec.Networking.AdminConsoleServices = couchbasev2.ServiceList{
		couchbasev2.DataService,
	}
	cluster.Spec.Networking.AdminConsoleServiceType = corev1.ServiceTypeLoadBalancer
	cluster.Spec.Networking.DNS = &couchbasev2.DNS{
		Domain: domain,
	}
	cluster.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
	}
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Verify console service advertises a DNS based address.
	e2eutil.MustCheckForDNSAdminAnnotation(t, kubernetes, cluster, domain, time.Minute)
	e2eutil.MustCheckForConsoleServiceType(t, kubernetes, cluster, corev1.ServiceTypeLoadBalancer, time.Minute)

	// Verify data ports are not exposed for bootstrapping since exposed features is not set
	rejectedPorts := &[]int32{dataServicePort, dataServicePortTLS}
	e2eutil.MustCheckConsolePorts(t, kubernetes, cluster, nil, rejectedPorts)
}

// TestConsoleServiceDNSModify tests modifications to the DNS configuration are mirrored by
// console service.
func TestConsoleServiceDNSModify(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size1

	// Create the cluster.
	clusterName := "test-couchbase-" + e2eutil.RandomSuffix()
	tlsOptions := &e2eutil.TLSOpts{
		ClusterName: clusterName,
		ExtraAltNames: []string{
			fmt.Sprintf("*.%s", domain),
		},
	}

	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, tlsOptions)

	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Name = clusterName
	cluster.Spec.Networking.ExposeAdminConsole = true
	cluster.Spec.Networking.AdminConsoleServices = couchbasev2.ServiceList{
		couchbasev2.DataService,
	}
	cluster.Spec.Networking.AdminConsoleServiceType = corev1.ServiceTypeLoadBalancer
	cluster.Spec.Networking.DNS = &couchbasev2.DNS{
		Domain: domain,
	}
	cluster.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
	}
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Verify that all nodes advertise a DNS based alternate address, and it changes when updated.
	e2eutil.MustCheckForDNSAdminAnnotation(t, kubernetes, cluster, domain, time.Minute)
	e2eutil.MustCheckForConsoleServiceType(t, kubernetes, cluster, corev1.ServiceTypeLoadBalancer, time.Minute)
	subjectAltNames := x509.MandatorySANs(cluster.Name, cluster.Namespace, true, true)
	subjectAltNames = append(subjectAltNames, fmt.Sprintf("*.%s", newDomain))
	opts := e2eutil.TLSOpts{
		AltNames: subjectAltNames,
	}
	e2eutil.MustRotateServerCertificate(t, ctx, &opts)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/networking/dns/domain", newDomain), time.Minute)
	e2eutil.MustCheckForDNSAdminAnnotation(t, kubernetes, cluster, newDomain, 5*time.Minute)
	e2eutil.MustCheckForConsoleServiceType(t, kubernetes, cluster, corev1.ServiceTypeLoadBalancer, time.Minute)
}

// TestConsoleServiceTypeModify tests the console service type is updated when the configuration
// is updated.
func TestConsoleServiceTypeModify(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size1
	domain := "acme.com"

	// Create the cluster.
	clusterName := "test-couchbase-" + e2eutil.RandomSuffix()
	tlsOptions := &e2eutil.TLSOpts{
		ClusterName: clusterName,
		ExtraAltNames: []string{
			fmt.Sprintf("*.%s", domain),
		},
	}

	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, tlsOptions)

	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Name = clusterName
	cluster.Spec.Networking.ExposeAdminConsole = true
	cluster.Spec.Networking.AdminConsoleServices = couchbasev2.ServiceList{
		couchbasev2.DataService,
	}
	cluster.Spec.Networking.AdminConsoleServiceType = corev1.ServiceTypeLoadBalancer
	cluster.Spec.Networking.DNS = &couchbasev2.DNS{
		Domain: domain,
	}
	cluster.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
	}
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Verify that changing the node port type is reflected in the services.
	e2eutil.MustCheckForConsoleServiceType(t, kubernetes, cluster, corev1.ServiceTypeLoadBalancer, time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/networking/adminConsoleServiceType", corev1.ServiceTypeNodePort), time.Minute)
	e2eutil.MustCheckForConsoleServiceType(t, kubernetes, cluster, corev1.ServiceTypeNodePort, time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/networking/adminConsoleServiceType", corev1.ServiceTypeLoadBalancer), time.Minute)
	e2eutil.MustCheckForConsoleServiceType(t, kubernetes, cluster, corev1.ServiceTypeLoadBalancer, time.Minute)
}

// TestExposedFeatureTrafficPolicyCluster ensures an external traffic policy of
// Cluster doesn't cause the cluster to fail.
func TestExposedFeatureTrafficPolicyCluster(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.Networking.ExposedFeatures = couchbasev2.ExposedFeatureList{
		couchbasev2.FeatureAdmin,
	}
	policy := corev1.ServiceExternalTrafficPolicyTypeCluster
	cluster.Spec.Networking.ExposedFeatureTrafficPolicy = &policy
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	e2eutil.MustWaitForClusterEvent(t, kubernetes, cluster, e2eutil.RebalanceCompletedEvent(cluster), 10*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		eventschema.Repeat{Times: clusterSize, Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestLoadBalancerSourceRanges tests that we can create a cluster with IP
// source ranges set, remove them and add them back again, and observe that it
// is happening.
func TestLoadBalancerSourceRanges(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := constants.Size1
	sourceRanges := []string{
		"192.168.0.1/32",
	}

	// Create the cluster.
	clusterName := "test-couchbase-" + e2eutil.RandomSuffix()
	tlsOptions := &e2eutil.TLSOpts{
		ClusterName: clusterName,
		ExtraAltNames: []string{
			fmt.Sprintf("*.%s", domain),
		},
	}

	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, tlsOptions)

	e2eutil.MustNewBucket(t, kubernetes, e2espec.DefaultBucket())
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Name = clusterName
	cluster.Spec.Networking.ExposeAdminConsole = true
	cluster.Spec.Networking.AdminConsoleServiceTemplate = &couchbasev2.ServiceTemplateSpec{
		Spec: &corev1.ServiceSpec{
			Type:                     corev1.ServiceTypeLoadBalancer,
			LoadBalancerSourceRanges: sourceRanges,
		},
	}
	cluster.Spec.Networking.DNS = &couchbasev2.DNS{
		Domain: domain,
	}
	cluster.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
	}
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Ensure the source ranges are correctly installed, then remove and verify, then
	// add back again and verify.
	e2eutil.MustCheckConsoleServiceLoadBalancerSourceRanges(t, kubernetes, cluster, sourceRanges, time.Minute)
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Remove("/spec/networking/adminConsoleServiceTemplate/spec/loadBalancerSourceRanges"), time.Minute)
	e2eutil.MustCheckConsoleServiceLoadBalancerSourceRanges(t, kubernetes, cluster, nil, time.Minute)
	_ = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/networking/adminConsoleServiceTemplate/spec/loadBalancerSourceRanges", sourceRanges), time.Minute)
	e2eutil.MustCheckConsoleServiceLoadBalancerSourceRanges(t, kubernetes, cluster, sourceRanges, time.Minute)
}

// TestConsoleServiceBootstrapingClient verifies that data service ports are exposed
// on Console service when expose features includes the 'client' parameter.
func TestConsoleServiceBootstrapingClient(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterName := "test-couchbase-" + e2eutil.RandomSuffix()
	clusterSize := constants.Size1

	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	// create cluster
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Name = clusterName
	cluster.Spec.Networking.ExposeAdminConsole = true
	cluster.Spec.Networking.ExposedFeatures = []couchbasev2.ExposedFeature{
		couchbasev2.FeatureClient,
	}
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Verify console service exposes data ports.
	expectedPorts := &[]int32{dataServicePort, dataServicePortTLS}
	e2eutil.MustCheckConsolePorts(t, kubernetes, cluster, expectedPorts, nil)
}

// TestConsoleServiceBootstrapingXDCR verifies that data service ports is not exposed
// on Console service when expose features only specifies 'xdcr' parameter.
func TestConsoleServiceBootstrapingXDCR(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterName := "test-couchbase-" + e2eutil.RandomSuffix()
	clusterSize := constants.Size1

	bucket := e2eutil.MustGetBucket(f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, kubernetes, bucket)

	// create cluster
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Name = clusterName
	cluster.Spec.Networking.ExposeAdminConsole = true
	cluster.Spec.Networking.ExposedFeatures = []couchbasev2.ExposedFeature{
		couchbasev2.FeatureXDCR,
	}
	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Verify console service exposes data ports.
	rejectedPorts := &[]int32{dataServicePort, dataServicePortTLS}
	e2eutil.MustCheckConsolePorts(t, kubernetes, cluster, nil, rejectedPorts)
}

// TestNetworkAddressFamily ensures address family enforcement can be turned on and
// off.
func TestNetworkAddressFamily(t *testing.T) {
	testNetworkAddressFamilyAndNodeToNode(t, nil)
}

// TestNetworkAddressFamilyNodeToNodeControlPlaneOnly ensures address family enforcement can be turned on and
// off when using the ControlPlaneOnly encryption mode.
func TestNetworkAddressFamilyAndNodeToNodeControlPlaneOnly(t *testing.T) {
	n2n := couchbasev2.NodeToNodeControlPlaneOnly
	testNetworkAddressFamilyAndNodeToNode(t, &n2n)
}

// TestNetworkAddressFamilyNodeToNodeAll ensures address family enforcement can be turned on and
// off when using the All encryption mode.
func TestNetworkAddressFamilyAndNodeToNodeAll(t *testing.T) {
	n2n := couchbasev2.NodeToNodeAll
	testNetworkAddressFamilyAndNodeToNode(t, &n2n)
}

// TestNetworkAddressFamilyNodeToNodeStrict ensures address family enforcement can be turned on and
// off when using the Strict encryption mode.
func TestNetworkAddressFamilyAndNodeToNodeStrict(t *testing.T) {
	n2n := couchbasev2.NodeToNodeStrict
	testNetworkAddressFamilyAndNodeToNode(t, &n2n)
}

func testNetworkAddressFamilyAndNodeToNode(t *testing.T, nodeToNode *couchbasev2.NodeToNodeEncryptionType) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3

	// The enforcement stuff only appeared in 7.0.2... in true "add a new feature
	// to a bug fix release" form.
	framework.Requires(t, kubernetes).AtLeastVersion("7.0.2")

	aFamilyBaseline := couchbasev2.IPv4Priority
	aFamilyChange := couchbasev2.IPv4Only
	aFamilyDeprecated := couchbasev2.IPv4
	aFamilyBaselineListener := couchbaseutil.AddressFamilyIPV4
	if kubernetes.IPv6 {
		aFamilyBaseline = couchbasev2.IPv6Only
		aFamilyChange = couchbasev2.IPv6Priority
		aFamilyDeprecated = couchbasev2.IPv6
		aFamilyBaselineListener = couchbaseutil.AddressFamilyIPV6
	}

	// Create the cluster.
	expectedEncryption := couchbaseutil.Off
	var cluster *couchbasev2.CouchbaseCluster
	var ctx *e2eutil.TLSContext
	if nodeToNode != nil {
		cluster, ctx = MustInitClusterWithTLSAndNodeToNode(t, kubernetes, clusterSize, nodeToNode)
		expectedEncryption = couchbaseutil.On
	} else {
		cluster = clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	}

	// Check the baseline is correct.
	e2eutil.MustExposePortsTLS(t, kubernetes, cluster, aFamilyBaseline, ctx, time.Minute)
	e2eutil.MustHaveListenerSettingsTLS(t, kubernetes, cluster, ctx, []*couchbaseutil.ListenerConfiguration{
		{
			AddressFamily:  aFamilyBaselineListener,
			NodeEncryption: expectedEncryption,
		},
	}, time.Minute)
	e2eutil.MustWaitForClusterPodsReady(t, kubernetes, cluster, 2*time.Minute)

	// Check we can change the address family.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/networking/addressFamily", aFamilyChange), time.Minute)
	e2eutil.MustExposePortsTLS(t, kubernetes, cluster, aFamilyChange, ctx, time.Minute)
	e2eutil.MustHaveListenerSettingsTLS(t, kubernetes, cluster, ctx, []*couchbaseutil.ListenerConfiguration{
		{
			AddressFamily:  aFamilyBaselineListener,
			NodeEncryption: expectedEncryption,
		},
	}, time.Minute)
	e2eutil.MustWaitForClusterPodsReady(t, kubernetes, cluster, 2*time.Minute)

	// Check the deprecated option still works.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/networking/addressFamily", aFamilyDeprecated), time.Minute)
	e2eutil.MustExposePortsTLS(t, kubernetes, cluster, aFamilyDeprecated, ctx, time.Minute)
	e2eutil.MustHaveListenerSettingsTLS(t, kubernetes, cluster, ctx, []*couchbaseutil.ListenerConfiguration{
		{
			AddressFamily:  aFamilyBaselineListener,
			NodeEncryption: expectedEncryption,
		},
	}, time.Minute)
	e2eutil.MustWaitForClusterPodsReady(t, kubernetes, cluster, 2*time.Minute)

	// Ensure the expected events were raised.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}

	// IPv6 clusters start with IPv6Only. Changing from IPv6Priority -> deprecated IPv6 requires a change, whereas IPv4 clusters change from IPv4Only -> deprecatedIPv4 (no change).
	networkSettingsEvents := 1

	if kubernetes.IPv6 {
		expectedEvents = append(expectedEvents, eventschema.Event{Reason: k8sutil.EventNetworkSettingsModified})
		networkSettingsEvents++
	}

	if nodeToNode != nil {
		// Enabled as part of cluster creation.
		expectedEvents = append(expectedEvents, e2eutil.EnableN2NEncryptionSequence(nodeToNode))

		// Changing N2N encryption requires n2n encryption to be turned off. Following the change we expect it to be turned on again.
		for i := 0; i < networkSettingsEvents; i++ {
			expectedEvents = append(expectedEvents, e2eutil.DisableN2NEncryptionSequence(nodeToNode))
			expectedEvents = append(expectedEvents, eventschema.Event{Reason: k8sutil.EventNetworkSettingsModified})
			expectedEvents = append(expectedEvents, e2eutil.EnableN2NEncryptionSequence(nodeToNode))
		}
	} else {
		expectedEvents = append(expectedEvents, eventschema.Repeat{Times: networkSettingsEvents, Validator: eventschema.Event{Reason: k8sutil.EventNetworkSettingsModified}})
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestNetworkAddressFamilyAndNodeToNodeAll ensures address families and listener settings are correctly updated when TLS is enabled on a cluster
// using All encryption mode.
func TestNetworkAddressFamilyAndNodeToNodeEnabledAll(t *testing.T) {
	n2n := couchbasev2.NodeToNodeAll
	testNetworkAddressFamilyAndNodeToNodeEnabled(t, &n2n)
}

// TestNetworkAddressFamilyAndNodeToNodeStrict ensures address families and listener settings are correctly updated when TLS is enabled on a cluster
// using Strict encryption mode.
func TestNetworkAddressFamilyAndNodeToNodeEnabledStrict(t *testing.T) {
	n2n := couchbasev2.NodeToNodeStrict
	testNetworkAddressFamilyAndNodeToNodeEnabled(t, &n2n)
}

// TestNetworkAddressFamilyAndNodeToNodeEnabledControlPlaneOnly ensures address families and listener settings are correctly updated when TLS is enabled on a cluster
// using ControlPlaneOnly encryption mode.
func TestNetworkAddressFamilyAndNodeToNodeEnabledControlPlaneOnly(t *testing.T) {
	n2n := couchbasev2.NodeToNodeControlPlaneOnly
	testNetworkAddressFamilyAndNodeToNodeEnabled(t, &n2n)
}

func testNetworkAddressFamilyAndNodeToNodeEnabled(t *testing.T, nodeToNode *couchbasev2.NodeToNodeEncryptionType) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3

	// The enforcement stuff only appeared in 7.0.2... in true "add a new feature
	// to a bug fix release" form.
	framework.Requires(t, kubernetes).AtLeastVersion("7.0.2")

	aFamily := couchbasev2.IPv4Priority
	aFamilyListener := couchbaseutil.AddressFamilyIPV4
	if kubernetes.IPv6 {
		aFamily = couchbasev2.IPv6Only
		aFamilyListener = couchbaseutil.AddressFamilyIPV6
	}

	// Create the cluster.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)

	// As of 3/6/26, IPV6 + strict does not work with the index service. Can remove this once fixed.
	if kubernetes.IPv6 && nodeToNode != nil && *nodeToNode == couchbasev2.NodeToNodeStrict {
		cluster.Spec.Servers[0].Services = []couchbasev2.Service{couchbasev2.DataService, couchbasev2.QueryService}
	}

	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Verify the initial network configuration is correct.
	e2eutil.MustExposePorts(t, kubernetes, cluster, aFamily, time.Minute)
	e2eutil.MustHaveListenerSettings(t, kubernetes, cluster, []*couchbaseutil.ListenerConfiguration{
		{
			AddressFamily:  aFamilyListener,
			NodeEncryption: couchbaseutil.Off,
		},
	}, time.Minute)

	// Enable TLS and N2N encryption and wait for the cluster to be ready again.
	ctx := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{ClusterName: cluster.Name})

	tls := &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
		NodeToNodeEncryption: nodeToNode,
	}

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/networking/tls", tls), time.Minute)
	e2eutil.MustWaitForClusterCondition(t, kubernetes, couchbasev2.ClusterConditionUpgrading, v1.ConditionTrue, cluster, 5*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 20*time.Minute)
	e2eutil.MustCheckN2NEnabled(t, kubernetes, cluster, *nodeToNode, ctx, time.Minute)

	// Check the network configs for each member are updated with N2N configuration.
	e2eutil.MustExposePortsTLS(t, kubernetes, cluster, aFamily, ctx, time.Minute)
	e2eutil.MustHaveListenerSettingsTLS(t, kubernetes, cluster, ctx, []*couchbaseutil.ListenerConfiguration{
		{
			AddressFamily:  aFamilyListener,
			NodeEncryption: couchbaseutil.On,
		},
	}, time.Minute)
	e2eutil.MustWaitForClusterPodsReady(t, kubernetes, cluster, 2*time.Minute)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}

	if kubernetes.IPv6 {
		expectedEvents = append(expectedEvents, eventschema.Event{Reason: k8sutil.EventNetworkSettingsModified})
	}

	expectedEvents = append(expectedEvents,
		eventschema.Event{Reason: k8sutil.EventReasonClientTLSUpdated},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: clusterSize, Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
		e2eutil.EnableN2NEncryptionSequence(nodeToNode),
	)

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestNetworkAddressFamilyAndNodeToNodeDisabledControlPlaneOnly ensures address families and listener settings are correctly updated when N2N encryption is disabled on a cluster
// using ControlPlaneOnly encryption mode. This test runs with both IPv4 and IPv6 depending on the cao certify --ipv6 flag.
func TestNetworkAddressFamilyAndNodeToNodeDisabledControlPlaneOnly(t *testing.T) {
	n2n := couchbasev2.NodeToNodeControlPlaneOnly
	testNetworkAddressFamilyAndNodeToNodeDisabled(t, &n2n)
}

// TestNetworkAddressFamilyAndNodeToNodeDisabledStrict ensures address families and listener settings are correctly updated when N2N encryption is disabled on a cluster
// using Strict encryption mode. This test runs with both IPv4 and IPv6 depending on the cao certify --ipv6 flag.
func TestNetworkAddressFamilyAndNodeToNodeDisabledStrict(t *testing.T) {
	n2n := couchbasev2.NodeToNodeStrict
	testNetworkAddressFamilyAndNodeToNodeDisabled(t, &n2n)
}

// TestNetworkAddressFamilyAndNodeToNodeDisabledAll ensures address families and listener settings are correctly updated when N2N encryption is disabled on a cluster
// using All encryption mode. This test runs with both IPv4 and IPv6 depending on the cao certify --ipv6 flag.
func TestNetworkAddressFamilyAndNodeToNodeDisabledAll(t *testing.T) {
	n2n := couchbasev2.NodeToNodeAll
	testNetworkAddressFamilyAndNodeToNodeDisabled(t, &n2n)
}

func testNetworkAddressFamilyAndNodeToNodeDisabled(t *testing.T, nodeToNode *couchbasev2.NodeToNodeEncryptionType) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3

	// The enforcement stuff only appeared in 7.0.2... in true "add a new feature
	// to a bug fix release" form.
	framework.Requires(t, kubernetes).AtLeastVersion("7.0.2")

	aFamily := couchbasev2.IPv4Priority
	aFamilyListener := couchbaseutil.AddressFamilyIPV4
	if kubernetes.IPv6 {
		aFamily = couchbasev2.IPv6Only
		aFamilyListener = couchbaseutil.AddressFamilyIPV6
	}

	// Create the cluster.
	cluster, ctx := MustInitClusterWithTLSAndNodeToNode(t, kubernetes, clusterSize, nodeToNode)

	// Check the ports and listener.
	e2eutil.MustExposePortsTLS(t, kubernetes, cluster, aFamily, ctx, time.Minute)
	e2eutil.MustHaveListenerSettingsTLS(t, kubernetes, cluster, ctx, []*couchbaseutil.ListenerConfiguration{
		{
			AddressFamily:  aFamilyListener,
			NodeEncryption: couchbaseutil.On,
		},
	}, time.Minute)

	// Disable N2N encryption and check the listener updates but the port doesn't change.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Remove("/spec/networking/tls/nodeToNodeEncryption"), time.Minute)
	e2eutil.MustCheckN2NDisabled(t, kubernetes, cluster, *nodeToNode, time.Minute)

	e2eutil.MustExposePorts(t, kubernetes, cluster, aFamily, time.Minute)
	e2eutil.MustHaveListenerSettings(t, kubernetes, cluster, []*couchbaseutil.ListenerConfiguration{
		{
			AddressFamily:  aFamilyListener,
			NodeEncryption: couchbaseutil.Off,
		},
	}, time.Minute)
	e2eutil.MustWaitForClusterPodsReady(t, kubernetes, cluster, 2*time.Minute)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}

	if kubernetes.IPv6 {
		expectedEvents = append(expectedEvents, eventschema.Event{Reason: k8sutil.EventNetworkSettingsModified})
	}

	expectedEvents = append(expectedEvents, e2eutil.EnableN2NEncryptionSequence(nodeToNode))
	expectedEvents = append(expectedEvents, e2eutil.DisableN2NEncryptionSequence(nodeToNode))

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestNetworkAddressFamilyChange ensures changing the address family, scaling the cluster and changing back correctly updates the network configuration and listeners on couchbase server.
// This test runs with both IPv4 and IPv6 depending on the cao certify --ipv6 flag.
func TestNetworkAddressFamilyChange(t *testing.T) {
	testNetworkAddressFamilyAndNodeToNodeChange(t, nil)
}

// TestNetworkAddressFamilyChange ensures changing the address family, scaling the cluster and changing back correctly updates the network configuration and listeners on Couchbase Server
// while N2N encryption is enabled with mode ControlPlaneOnly. This test runs with both IPv4 and IPv6 depending on the cao certify --ipv6 flag.
func TestNetworkAddressFamilyChangeNodeToNodeControlPlaneOnly(t *testing.T) {
	nodeToNode := couchbasev2.NodeToNodeControlPlaneOnly
	testNetworkAddressFamilyAndNodeToNodeChange(t, &nodeToNode)
}

// TestNetworkAddressFamilyChange ensures changing the address family, scaling the cluster and changing back correctly updates the network configuration and listeners on Couchbase Server
// while N2N encryption is enabled with mode Strict. This test runs with both IPv4 and IPv6 depending on the cao certify --ipv6 flag.
func TestNetworkAddressFamilyChangeNodeToNodeStrict(t *testing.T) {
	nodeToNode := couchbasev2.NodeToNodeStrict
	testNetworkAddressFamilyAndNodeToNodeChange(t, &nodeToNode)
}

// TestNetworkAddressFamilyChange ensures changing the address family, scaling the cluster and changing back correctly updates the network configuration and listeners on Couchbase Server
// while N2N encryption is enabled with mode All. This test runs with both IPv4 and IPv6 depending on the cao certify --ipv6 flag.
func TestNetworkAddressFamilyChangeNodeToNodeAll(t *testing.T) {
	nodeToNode := couchbasev2.NodeToNodeAll
	testNetworkAddressFamilyAndNodeToNodeChange(t, &nodeToNode)
}

func testNetworkAddressFamilyAndNodeToNodeChange(t *testing.T, nodeToNode *couchbasev2.NodeToNodeEncryptionType) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.2")

	encryption := nodeToNode != nil

	// Create the cluster.
	expectedEncryption := couchbaseutil.Off
	var cluster *couchbasev2.CouchbaseCluster
	var ctx *e2eutil.TLSContext
	if encryption {
		cluster, ctx = MustGenerateClusterWithTLSAndNodeToNode(t, kubernetes, clusterSize, nodeToNode)
		expectedEncryption = couchbaseutil.On
	} else {
		cluster = clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	}

	aFamilyBaseline := couchbasev2.IPv4Only
	aFamilyChange := couchbasev2.IPv4Priority
	aFamilyBaselineListener := couchbaseutil.AddressFamilyIPV4
	if kubernetes.IPv6 {
		aFamilyBaseline = couchbasev2.IPv6Only
		aFamilyChange = couchbasev2.IPv6Priority
		aFamilyBaselineListener = couchbaseutil.AddressFamilyIPV6
	} else {
		cluster.Spec.Networking.AddressFamily = &aFamilyBaseline
	}

	// TODO: We should be able to remove this once the index service + ipv6 + strict encryption works.
	// FOr now we'll leave this here so our tests can pass.
	cluster.Spec.Servers[0].Services = []couchbasev2.Service{couchbasev2.DataService}

	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Check that the cluster is initialised correctly.
	e2eutil.MustExposePortsTLS(t, kubernetes, cluster, aFamilyBaseline, ctx, time.Minute)
	e2eutil.MustHaveListenerSettingsTLS(t, kubernetes, cluster, ctx, []*couchbaseutil.ListenerConfiguration{
		{
			AddressFamily:  aFamilyBaselineListener,
			NodeEncryption: expectedEncryption,
		},
	}, time.Minute)
	e2eutil.MustWaitForClusterPodsReady(t, kubernetes, cluster, 2*time.Minute)

	// Update the cluster to a dual networking setup.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/networking/addressFamily", aFamilyChange), time.Minute)

	// Check that the address family updates to dual networking and the listener settings are updated to match.
	e2eutil.MustExposePortsTLS(t, kubernetes, cluster, aFamilyChange, ctx, time.Minute)
	e2eutil.MustHaveListenerSettingsTLS(t, kubernetes, cluster, ctx, []*couchbaseutil.ListenerConfiguration{
		{
			AddressFamily:  aFamilyBaselineListener,
			NodeEncryption: expectedEncryption,
		},
	}, time.Minute)
	e2eutil.MustWaitForClusterPodsReady(t, kubernetes, cluster, 2*time.Minute)

	// time.Sleep(30 * time.Hour)

	// Scale the cluster and check the new pod has the correct settings.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/servers/0/size", clusterSize+1), time.Minute)
	e2eutil.MustWaitForClusterConditionsRemoved(t, kubernetes, cluster, 5*time.Minute, couchbasev2.ClusterConditionScaling)

	e2eutil.MustExposePortsTLS(t, kubernetes, cluster, aFamilyChange, ctx, time.Minute)
	e2eutil.MustHaveListenerSettingsTLS(t, kubernetes, cluster, ctx, []*couchbaseutil.ListenerConfiguration{
		{
			AddressFamily:  aFamilyBaselineListener,
			NodeEncryption: expectedEncryption,
		},
	}, time.Minute)
	e2eutil.MustWaitForClusterPodsReady(t, kubernetes, cluster, 2*time.Minute)

	// time.Sleep(30 * time.Hour)

	// Change the address family back to the original and check the settings are reverted correctly, and the cluster remains healthy with the new node.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/networking/addressFamily", aFamilyBaseline), time.Minute)
	e2eutil.MustExposePortsTLS(t, kubernetes, cluster, aFamilyBaseline, ctx, time.Minute)
	e2eutil.MustHaveListenerSettingsTLS(t, kubernetes, cluster, ctx, []*couchbaseutil.ListenerConfiguration{
		{
			AddressFamily:  aFamilyBaselineListener,
			NodeEncryption: expectedEncryption,
		},
	}, time.Minute)
	e2eutil.MustWaitForClusterPodsReady(t, kubernetes, cluster, 2*time.Minute)

	// Ensure the expected events were raised.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		// Clusters are always initiated with dual stack listeners. The following reconcile loop will update these to single stack.
		eventschema.Event{Reason: k8sutil.EventNetworkSettingsModified},
	}

	if encryption {
		// Enable N2N encryption after cluster startup.
		expectedEvents = append(expectedEvents, e2eutil.EnableN2NEncryptionSequence(nodeToNode))

		// Update to dual stack.
		expectedEvents = append(expectedEvents, e2eutil.DisableN2NEncryptionSequence(nodeToNode))
		expectedEvents = append(expectedEvents, eventschema.Event{Reason: k8sutil.EventNetworkSettingsModified})
		expectedEvents = append(expectedEvents, e2eutil.EnableN2NEncryptionSequence(nodeToNode))

		// Scale the cluster.
		expectedEvents = append(expectedEvents, e2eutil.ClusterScaleUpSequence(1))

		// Revert to original single stack.
		expectedEvents = append(expectedEvents, e2eutil.DisableN2NEncryptionSequence(nodeToNode))
		expectedEvents = append(expectedEvents, eventschema.Event{Reason: k8sutil.EventNetworkSettingsModified})
		expectedEvents = append(expectedEvents, e2eutil.EnableN2NEncryptionSequence(nodeToNode))
	} else {
		// Update to dual stack.
		expectedEvents = append(expectedEvents, eventschema.Event{Reason: k8sutil.EventNetworkSettingsModified})
		// Scale the cluster.
		expectedEvents = append(expectedEvents, e2eutil.ClusterScaleUpSequence(1))
		// Revert to original signle stack.
		expectedEvents = append(expectedEvents, eventschema.Event{Reason: k8sutil.EventNetworkSettingsModified})
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestNetworkAddressFamilyChangeToOppositePriority ensures changing the address family from IPv4Only -> IPv6Priority or IPv6Only -> IPv4Priority is allowed and works correctly.
// This test runs with both IPv4 and IPv6 depending on the cao certify --ipv6 flag.
func TestNetworkAddressFamilyChangeToOppositePriority(t *testing.T) {
	testNetworkAddressFamilyChangeToOppositePriorityAndNodeToNode(t, nil)
}

// TestNetworkAddressFamilyChangeToOppositePriority ensures changing the address family from IPv4Only -> IPv6Priority or IPv6Only -> IPv4Priority is allowed and works correctly while N2N encryption
// is enabled with mode ControlPlaneOnly. This test runs with both IPv4 and IPv6 depending on the cao certify --ipv6 flag.
func TestNetworkAddressFamilyChangeToOppositePriorityNodeToNodeControlPlaneOnly(t *testing.T) {
	nodeToNode := couchbasev2.NodeToNodeControlPlaneOnly
	testNetworkAddressFamilyChangeToOppositePriorityAndNodeToNode(t, &nodeToNode)
}

// TestNetworkAddressFamilyChangeToOppositePriority ensures changing the address family from IPv4Only -> IPv6Priority or IPv6Only -> IPv4Priority is allowed and works correctly while N2N encryption
// is enabled with mode Strict. This test runs with both IPv4 and IPv6 depending on the cao certify --ipv6 flag.
func TestNetworkAddressFamilyChangeToOppositePriorityNodeToNodeStrict(t *testing.T) {
	nodeToNode := couchbasev2.NodeToNodeStrict
	testNetworkAddressFamilyChangeToOppositePriorityAndNodeToNode(t, &nodeToNode)
}

// TestNetworkAddressFamilyChangeToOppositePriority ensures changing the address family from IPv4Only -> IPv6Priority or IPv6Only -> IPv4Priority is allowed and works correctly while N2N encryption
// is enabled with mode All. This test runs with both IPv4 and IPv6 depending on the cao certify --ipv6 flag.
func TestNetworkAddressFamilyChangeToOppositePriorityNodeToNodeAll(t *testing.T) {
	nodeToNode := couchbasev2.NodeToNodeAll
	testNetworkAddressFamilyChangeToOppositePriorityAndNodeToNode(t, &nodeToNode)
}

func testNetworkAddressFamilyChangeToOppositePriorityAndNodeToNode(t *testing.T, nodeToNode *couchbasev2.NodeToNodeEncryptionType) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3

	framework.Requires(t, kubernetes).AtLeastVersion("7.0.2")

	encryption := nodeToNode != nil

	// Create the cluster.
	expectedEncryption := couchbaseutil.Off
	var cluster *couchbasev2.CouchbaseCluster
	var ctx *e2eutil.TLSContext
	if encryption {
		cluster, ctx = MustGenerateClusterWithTLSAndNodeToNode(t, kubernetes, clusterSize, nodeToNode)
		expectedEncryption = couchbaseutil.On
	} else {
		cluster = clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	}

	aFamilyBaseline := couchbasev2.IPv4Only
	aFamilyChange := couchbasev2.IPv6Priority
	aFamilyBaselineListener := couchbaseutil.AddressFamilyIPV4
	aFamilyChangeListener := couchbaseutil.AddressFamilyIPV6
	if kubernetes.IPv6 {
		aFamilyBaseline = couchbasev2.IPv6Only
		aFamilyChange = couchbasev2.IPv4Priority
		aFamilyBaselineListener = couchbaseutil.AddressFamilyIPV6
		aFamilyChangeListener = couchbaseutil.AddressFamilyIPV4
	} else {
		cluster.Spec.Networking.AddressFamily = &aFamilyBaseline
	}

	// TODO: We should be able to remove this once the index service + ipv6 + strict encryption works.
	// FOr now we'll leave this here so our tests can pass.
	cluster.Spec.Servers[0].Services = []couchbasev2.Service{couchbasev2.DataService}

	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Check that the cluster is initialised correctly.
	e2eutil.MustExposePortsTLS(t, kubernetes, cluster, aFamilyBaseline, ctx, time.Minute)
	e2eutil.MustHaveListenerSettingsTLS(t, kubernetes, cluster, ctx, []*couchbaseutil.ListenerConfiguration{
		{
			AddressFamily:  aFamilyBaselineListener,
			NodeEncryption: expectedEncryption,
		},
	}, time.Minute)

	// Update the cluster to the opposite priority setup.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/networking/addressFamily", aFamilyChange), time.Minute)

	// Check that the address family updates to the opposite priority and the listener settings are updated to match.
	e2eutil.MustExposePortsTLS(t, kubernetes, cluster, aFamilyChange, ctx, 5*time.Minute)
	e2eutil.MustHaveListenerSettingsTLS(t, kubernetes, cluster, ctx, []*couchbaseutil.ListenerConfiguration{
		{
			AddressFamily:  aFamilyChangeListener,
			NodeEncryption: expectedEncryption,
		},
	}, time.Minute)

	// Scale the cluster to check we can still add pods ok.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/servers/0/size", clusterSize+1), 5*time.Minute)
	e2eutil.MustWaitForClusterConditionsRemoved(t, kubernetes, cluster, 5*time.Minute, couchbasev2.ClusterConditionScaling)

	// Check all the members again (includes new member).
	e2eutil.MustExposePortsTLS(t, kubernetes, cluster, aFamilyChange, ctx, 5*time.Minute)
	e2eutil.MustHaveListenerSettingsTLS(t, kubernetes, cluster, ctx, []*couchbaseutil.ListenerConfiguration{
		{
			AddressFamily:  aFamilyChangeListener,
			NodeEncryption: expectedEncryption,
		},
	}, time.Minute)

	// Return the cluster to the baseline address family.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/networking/addressFamily", aFamilyBaseline), time.Minute)

	// Check that the address family updates to the opposite priority and the listener settings are updated to match.
	e2eutil.MustExposePortsTLS(t, kubernetes, cluster, aFamilyBaseline, ctx, 5*time.Minute)
	e2eutil.MustHaveListenerSettingsTLS(t, kubernetes, cluster, ctx, []*couchbaseutil.ListenerConfiguration{
		{
			AddressFamily:  aFamilyBaselineListener,
			NodeEncryption: expectedEncryption,
		},
	}, time.Minute)

	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		// Clusters are always initiated with dual stack listeners. The following reconcile loop will update these to single stack.
		eventschema.Event{Reason: k8sutil.EventNetworkSettingsModified},
	}

	if encryption {
		// Enable N2N encryption after cluster startup.
		expectedEvents = append(expectedEvents, e2eutil.EnableN2NEncryptionSequence(nodeToNode))

		// Update to opposite address family dual stack.
		expectedEvents = append(expectedEvents, e2eutil.DisableN2NEncryptionSequence(nodeToNode))
		expectedEvents = append(expectedEvents, eventschema.Event{Reason: k8sutil.EventNetworkSettingsModified})
		expectedEvents = append(expectedEvents, e2eutil.EnableN2NEncryptionSequence(nodeToNode))

		// Scale the cluster.
		expectedEvents = append(expectedEvents, e2eutil.ClusterScaleUpSequence(1))

		// Revert to original single stack.
		expectedEvents = append(expectedEvents, e2eutil.DisableN2NEncryptionSequence(nodeToNode))
		expectedEvents = append(expectedEvents, eventschema.Event{Reason: k8sutil.EventNetworkSettingsModified})
		expectedEvents = append(expectedEvents, e2eutil.EnableN2NEncryptionSequence(nodeToNode))
	} else {
		// Update to opposite address family dual stack.
		expectedEvents = append(expectedEvents, eventschema.Event{Reason: k8sutil.EventNetworkSettingsModified})

		// Scale the cluster.
		expectedEvents = append(expectedEvents, e2eutil.ClusterScaleUpSequence(1))

		// Revert to original single stack.
		expectedEvents = append(expectedEvents, eventschema.Event{Reason: k8sutil.EventNetworkSettingsModified})
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestCreateInitNodeHostNameCluster(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	clusterSize := 1
	framework.Requires(t, kubernetes).InitNodeHostName(clusterSize)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)

	cluster.Annotations = map[string]string{
		"cao.couchbase.com/networking.improvedHostNetwork":      "true",
		"cao.couchbase.com/networking.initPodsWithNodeHostname": "true",
	}

	e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)
}

func TestCreateInitNodeHostNameClusterServerGroups(t *testing.T) {
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	clusterSize := 2
	serverGroups := 2
	framework.Requires(t, kubernetes).InitNodeHostName(clusterSize).ServerGroups(serverGroups)

	availableServerGroups := getAvailabilityZones(t, kubernetes)

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).Generate(kubernetes)
	cluster.Spec.ServerGroups = availableServerGroups[:serverGroups]

	cluster.Spec.Networking.ImprovedHostNetwork = true
	cluster.Spec.Networking.InitPodsWithNodeHostname = true

	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	expected := getExpectedRzaResultMap(clusterSize, availableServerGroups)
	expected.mustValidateRzaMap(t, kubernetes, cluster)
}

// TestAlternateAddressExternalDNSCheck tests that a healthy cluster
// that has its DNS settings updated will not eject activated nodes
// if the DNS check timeout is reached, but will continue to
// attempt to reach the DNS server.
func TestUpdatingAlternateAddressExternalDNSCheckDoesNotEjectActivatedNodes(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)

	defer cleanup()

	testDomain := "dnstest.local"

	// Provision a coreDNS service that mocks an exernalDNS by using the dns and the pod node ip.
	// Requests that aren't handled by this dns service will be forwarded to the cluster default dns service.
	// This will initially have no entries. We can add the pods after they have been created to simulate
	// dns propagation.
	dns := e2eutil.MustProvisionCoreDNSForExternalDNSCheck(t, kubernetes, testDomain)
	e2eutil.MustUpdateOperatorDeploymentDNSConfig(t, kubernetes, dns)

	// Static configuration.
	clusterName := "test-couchbase-" + e2eutil.RandomSuffix()
	clusterSize := constants.Size2

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithDNS(dns).Generate(kubernetes)
	cluster.Name = clusterName

	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)

	timeout := 30 * time.Second

	networking := couchbasev2.CouchbaseClusterNetworkingSpec{
		DNS: &couchbasev2.DNS{
			Domain: testDomain,
		},
		WaitForAddressReachableDelay: &metav1.Duration{Duration: 10 * time.Second},
		WaitForAddressReachable:      &metav1.Duration{Duration: timeout},
		ExposedFeatures:              []couchbasev2.ExposedFeature{couchbasev2.FeatureClient},
	}

	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/networking", networking), time.Minute)

	// Sleep so we can guarantee the DNS check timeout elapses.
	time.Sleep(timeout)

	pods := e2eutil.MustWaitForClusterPods(t, kubernetes, cluster, clusterSize, time.Minute)

	// Check both pods are still pending external DNS.
	e2eutil.MustWaitForPodWithCondition(t, kubernetes, pods[0].Name, k8sutil.PodPendingExternalDNSCondition, corev1.ConditionTrue, "Waiting on DNS Propagation", 1*time.Minute)
	e2eutil.MustWaitForPodWithCondition(t, kubernetes, pods[1].Name, k8sutil.PodPendingExternalDNSCondition, corev1.ConditionTrue, "Waiting on DNS Propagation", 1*time.Minute)

	// Check that the pods are marked as ready, even though they are pending external DNS.
	e2eutil.MustWaitForPodWithCondition(t, kubernetes, pods[0].Name, k8sutil.PodReadinessCondition, corev1.ConditionTrue, "", time.Minute)
	e2eutil.MustWaitForPodWithCondition(t, kubernetes, pods[1].Name, k8sutil.PodReadinessCondition, corev1.ConditionTrue, "", time.Minute)

	// Add all the pods to the dns forwarding list.
	e2eutil.MustAddPodsForDNSCheck(t, kubernetes, dns.GetName(), testDomain, pods)

	// We expect the cluster to enter a healthy state once all the pods' external addresses can be reached.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)

	// Make sure the pods no longer have the pending external DNS condition. It might take a bit for the coreDNS to update, hence the 3 minute timeout.
	e2eutil.MustWaitForPodWithoutCondition(t, kubernetes, pods[0].Name, k8sutil.PodPendingExternalDNSCondition, 3*time.Minute)
	e2eutil.MustWaitForPodWithoutCondition(t, kubernetes, pods[1].Name, k8sutil.PodPendingExternalDNSCondition, 3*time.Minute)

	// Check that the alternate addresses have been added to the cluster.
	e2eutil.MustCheckForDNSAlternateAddresses(t, cluster, testDomain, 2*time.Minute)
	e2eutil.MustCheckForDNSServiceAnnotations(t, kubernetes, cluster, testDomain, time.Minute)

	// We only expect the cluster to be created. Members should not be changed despite the
	// external DNS initially being unreachable and the DNS check timeout elapsing.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestAlternateAddressExternalDNSCheckEjectsNewPods tests that new pods that have not been
// activated in the cluster are ejected if the DNS check timeout is reached.
func TestAlternateAddressExternalDNSCheckEjectsNewPods(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)

	defer cleanup()

	testDomain := "dnstest.local"

	// Provision a coreDNS service that mocks an exernalDNS by using the dns and the pod node ip.
	// Requests that aren't handled by this dns service will be forwarded to the cluster default dns service.
	// This will initially have no entries. We can add the pods after they have been created to simulate
	// dns propagation.
	dns := e2eutil.MustProvisionCoreDNSForExternalDNSCheck(t, kubernetes, testDomain)
	e2eutil.MustUpdateOperatorDeploymentDNSConfig(t, kubernetes, dns)

	// Static configuration.
	clusterName := "test-couchbase-" + e2eutil.RandomSuffix()
	clusterSize := constants.Size2

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithDNS(dns).Generate(kubernetes)
	cluster.Name = clusterName
	cluster.Spec.Networking.ExposedFeatures = couchbasev2.ExposedFeatureList{
		couchbasev2.FeatureClient,
	}

	cluster.Spec.Networking = couchbasev2.CouchbaseClusterNetworkingSpec{
		DNS: &couchbasev2.DNS{
			Domain: testDomain,
		},
		WaitForAddressReachableDelay: &metav1.Duration{Duration: time.Minute},
		WaitForAddressReachable:      &metav1.Duration{Duration: 90 * time.Second},
		ExposedFeatures:              []couchbasev2.ExposedFeature{couchbasev2.FeatureClient},
	}

	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

	// Wait until all the cluster pods have been created
	pods := e2eutil.MustWaitForClusterPods(t, kubernetes, cluster, clusterSize, time.Minute)

	// Initially only add a single pod to the mocked dns.
	e2eutil.MustAddPodsForDNSCheck(t, kubernetes, dns.GetName(), testDomain, pods[:1])

	// Only the pod with the dns entry will be marked as ready. This may take a moment as coredns will need to reload.
	e2eutil.MustWaitForPodWithCondition(t, kubernetes, pods[0].Name, k8sutil.PodReadinessCondition, corev1.ConditionTrue, "", 2*time.Minute)

	// The other pod will be waiting for readiness.
	e2eutil.MustWaitForPodWithoutCondition(t, kubernetes, pods[1].Name, k8sutil.PodReadinessCondition, time.Minute)

	// Wait until the second pod is waiting for propagation, then wait for propagation to hit the timeout and the pod to be ejected as it was never activated.
	e2eutil.MustWaitForPodWithCondition(t, kubernetes, pods[1].Name, k8sutil.PodPendingExternalDNSCondition, corev1.ConditionTrue, "Waiting on DNS Propagation", 1*time.Minute)
	e2eutil.MustWaitForRebalanceEjectingNode(t, kubernetes, cluster, pods[1].Name, 10*time.Minute)

	// Fetch the new pod created by the operator and add it to the dns forwarding list.
	newMemberName := couchbaseutil.CreateMemberName(cluster.Name, 2)
	pod3 := e2eutil.MustWaitForPodWithCondition(t, kubernetes, newMemberName, k8sutil.PodPendingExternalDNSCondition, corev1.ConditionTrue, "Delaying DNS Check", 1*time.Minute)
	e2eutil.MustAddPodsForDNSCheck(t, kubernetes, dns.GetName(), testDomain, []corev1.Pod{*pod3})

	// We expect the cluster to enter a healthy state once all the pods' external addresses can be reached.
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)

	// Check that the alternate addresses have been added to the cluster.
	e2eutil.MustCheckForDNSAlternateAddresses(t, cluster, testDomain, 2*time.Minute)
	e2eutil.MustCheckForDNSServiceAnnotations(t, kubernetes, cluster, testDomain, time.Minute)

	// Make sure neither of the pods have the pending external DNS condition.
	e2eutil.MustWaitForPodWithoutCondition(t, kubernetes, pods[0].Name, k8sutil.PodPendingExternalDNSCondition, time.Minute)
	e2eutil.MustWaitForPodWithoutCondition(t, kubernetes, newMemberName, k8sutil.PodPendingExternalDNSCondition, time.Minute)

	// Check that both of the pods are marked as ready.
	e2eutil.MustWaitForPodWithCondition(t, kubernetes, pods[0].Name, k8sutil.PodReadinessCondition, corev1.ConditionTrue, "", time.Minute)
	e2eutil.MustWaitForPodWithCondition(t, kubernetes, newMemberName, k8sutil.PodReadinessCondition, corev1.ConditionTrue, "", time.Minute)

	// We expect the first two members to be added, the latter of which should be ejected as the DNS check
	// will fail given we didn't add it to the dns forwarding list. Once the third member is added, we expect
	// the cluster rebalance to complete as the DNS check will now pass.
	expectedEvents := []eventschema.Validatable{
		eventschema.Repeat{Times: 2, Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved, FuzzyMessage: pods[1].Name},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded, FuzzyMessage: newMemberName},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestAllowExternallyUnreachablePodsActivatesUnreachablePods tests that new couchabse nodes for which
// external DNS is not reachable will be activated in the cluster if the allowExternallyUnreachablePods flag is set to true.
func TestAllowExternallyUnreachablePodsActivatesUnreachablePods(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)

	defer cleanup()

	testDomain := "dnstest.com"

	// Provision a coreDNS service that mocks an exernalDNS by using the dns and the pod node ip.
	// Requests that aren't handled by this dns service will be forwarded to the cluster default dns service.
	// This will initially have no entries. We can add the pods after they have been created to simulate
	// dns propagation.
	dns := e2eutil.MustProvisionCoreDNSForExternalDNSCheck(t, kubernetes, testDomain)
	e2eutil.MustUpdateOperatorDeploymentDNSConfig(t, kubernetes, dns)

	// Static configuration.
	clusterName := "test-couchbase-" + e2eutil.RandomSuffix()
	clusterSize := constants.Size2

	cluster := clusterOptions().WithEphemeralTopology(clusterSize).WithDNS(dns).Generate(kubernetes)
	cluster.Name = clusterName
	cluster.Spec.Networking.ExposedFeatures = couchbasev2.ExposedFeatureList{
		couchbasev2.FeatureClient,
	}

	cluster.Spec.Networking = couchbasev2.CouchbaseClusterNetworkingSpec{
		DNS: &couchbasev2.DNS{
			Domain: testDomain,
		},
		WaitForAddressReachableDelay:   &metav1.Duration{Duration: 2 * time.Minute},
		WaitForAddressReachable:        &metav1.Duration{Duration: 3 * time.Minute},
		AllowExternallyUnreachablePods: util.BoolPtr(true),
		ExposedFeatures:                []couchbasev2.ExposedFeature{couchbasev2.FeatureClient},
	}

	cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)
	pod1Name := couchbaseutil.CreateMemberName(cluster.Name, 1)
	pod2Name := couchbaseutil.CreateMemberName(cluster.Name, 0)

	// Check that both pods first have the delaying DNS check condition.
	e2eutil.MustWaitForPodWithCondition(t, kubernetes, pod1Name, k8sutil.PodPendingExternalDNSCondition, corev1.ConditionTrue, "Delaying DNS Check", time.Minute)
	e2eutil.MustWaitForPodWithCondition(t, kubernetes, pod2Name, k8sutil.PodPendingExternalDNSCondition, corev1.ConditionTrue, "Delaying DNS Check", time.Minute)

	// Check that neither pod is marked as ready
	e2eutil.MustWaitForPodWithoutCondition(t, kubernetes, pod1Name, k8sutil.PodReadinessCondition, time.Minute)
	e2eutil.MustWaitForPodWithoutCondition(t, kubernetes, pod2Name, k8sutil.PodReadinessCondition, time.Minute)

	// Once the pods are waiting on dns propagation, we should wait at least 70 seconds (WaitForAddressReachable - WaitForAddressReachableDelay + 10 second overlap to consider reconciliation time)
	// to ensure the DNS check timeout elapses.
	time.Sleep(70 * time.Second)

	// The pods should now be marked as ready as the delay has elapsed.
	e2eutil.MustWaitForPodWithCondition(t, kubernetes, pod1Name, k8sutil.PodReadinessCondition, corev1.ConditionTrue, "", time.Minute)
	e2eutil.MustWaitForPodWithCondition(t, kubernetes, pod2Name, k8sutil.PodReadinessCondition, corev1.ConditionTrue, "", time.Minute)

	// Fetch the pods and add them to the dns forwarding list so we can pass the DNS check.
	pods := e2eutil.MustWaitForClusterPods(t, kubernetes, cluster, clusterSize, time.Minute)
	e2eutil.MustAddPodsForDNSCheck(t, kubernetes, dns.GetName(), testDomain, pods)

	// Check that the DNS alternate addresses have been added to the cluster.
	e2eutil.MustCheckForDNSAlternateAddresses(t, cluster, testDomain, 2*time.Minute)
	e2eutil.MustCheckForDNSServiceAnnotations(t, kubernetes, cluster, testDomain, time.Minute)

	// Make sure the pods no longer have the pending external DNS condition.
	e2eutil.MustWaitForPodWithoutCondition(t, kubernetes, pod1Name, k8sutil.PodPendingExternalDNSCondition, time.Minute)
	e2eutil.MustWaitForPodWithoutCondition(t, kubernetes, pod2Name, k8sutil.PodPendingExternalDNSCondition, time.Minute)

	// We only expect the cluster create sequence. Members should not be changed despite the
	// external DNS initially being unreachable and the DNS check timeout elapsing.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

// TestPodUpgradesWaitForDNSAvailableBeforeEjection tests that ejected pods are not destroyed if we aren't able to rebalance the cluster, such
// as during an upgrade where we are waiting for the DNS check delay to elapse for new pods.
func TestPodUpgradesWaitForDNSAvailableBeforeEjection(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)

	defer cleanup()

	framework.Requires(t, kubernetes).Upgradable()

	testDomain := "dnstest.com"

	dns := e2eutil.MustProvisionCoreDNSForExternalDNSCheck(t, kubernetes, testDomain)
	e2eutil.MustUpdateOperatorDeploymentDNSConfig(t, kubernetes, dns)

	clusterName := "test-couchbase-" + e2eutil.RandomSuffix()
	clusterSize := constants.Size2
	cluster := clusterOptionsUpgrade().WithEphemeralTopology(clusterSize).WithDNS(dns).Generate(kubernetes)
	cluster.Name = clusterName
	upgradeVersion := e2eutil.MustGetCouchbaseVersion(t, f.CouchbaseServerImage, f.CouchbaseServerImageVersion)

	networking := couchbasev2.CouchbaseClusterNetworkingSpec{
		DNS: &couchbasev2.DNS{
			Domain: testDomain,
		},
		WaitForAddressReachableDelay: &metav1.Duration{Duration: 10 * time.Second},
		WaitForAddressReachable:      &metav1.Duration{Duration: 2 * time.Minute},
		ExposedFeatures:              []couchbasev2.ExposedFeature{couchbasev2.FeatureClient},
	}

	cluster.Spec.Networking = networking
	cluster = e2eutil.MustNewClusterFromSpecAsync(t, kubernetes, cluster)

	// Once the pods have been created, we can add them to the dns forwarding list. This might take some time to propagate.
	pods := e2eutil.MustWaitForClusterPods(t, kubernetes, cluster, clusterSize, 5*time.Minute)
	e2eutil.MustAddPodsForDNSCheck(t, kubernetes, dns.GetName(), testDomain, pods)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)

	// Check the pods are marked as ready after the dns check delay has elapsed.
	e2eutil.MustWaitForPodWithCondition(t, kubernetes, pods[0].Name, k8sutil.PodReadinessCondition, corev1.ConditionTrue, "", time.Minute)
	e2eutil.MustWaitForPodWithCondition(t, kubernetes, pods[1].Name, k8sutil.PodReadinessCondition, corev1.ConditionTrue, "", time.Minute)

	// Check the DNS alternate addresses have been added to the cluster.
	e2eutil.MustCheckForDNSAlternateAddresses(t, cluster, testDomain, 2*time.Minute)
	e2eutil.MustCheckForDNSServiceAnnotations(t, kubernetes, cluster, testDomain, time.Minute)

	// Start an upgrade
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Replace("/spec/image", f.CouchbaseServerImage), time.Minute)

	// We'll need to wait for the new pods to be created before adding them to the dns forwarding list.
	for range clusterSize {
		pods := e2eutil.MustWaitForClusterPods(t, kubernetes, cluster, clusterSize+1, 2*time.Minute)
		e2eutil.MustAddPodsForDNSCheck(t, kubernetes, dns.GetName(), testDomain, pods)
		e2eutil.MustWaitForClusterPods(t, kubernetes, cluster, clusterSize, 2*time.Minute)
	}

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes, cluster, 10*time.Minute)
	pods = e2eutil.MustWaitForClusterPods(t, kubernetes, cluster, clusterSize, time.Minute)

	// Check that the pods are marked as ready.
	e2eutil.MustWaitForPodWithCondition(t, kubernetes, pods[0].Name, k8sutil.PodReadinessCondition, corev1.ConditionTrue, "", time.Minute)
	e2eutil.MustWaitForPodWithCondition(t, kubernetes, pods[1].Name, k8sutil.PodReadinessCondition, corev1.ConditionTrue, "", time.Minute)

	// Check that the cluster is on the upgrade version.
	e2eutil.MustCheckStatusVersion(t, kubernetes, cluster, upgradeVersion, time.Minute)
	e2eutil.MustCheckStatusVersionFor(t, kubernetes, cluster, upgradeVersion, time.Minute)

	// Check the events match what we expect and there are no adverse ones caused by the non-blocking DNS propagation.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeStarted},
		eventschema.Repeat{Times: clusterSize, Validator: upgradeSequence},
		eventschema.Event{Reason: k8sutil.EventReasonUpgradeFinished},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}
