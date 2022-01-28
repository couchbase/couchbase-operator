package e2e

import (
	"fmt"
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/x509"

	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/e2espec"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	corev1 "k8s.io/api/core/v1"
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
		bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
		e2eutil.MustNewBucket(t, kubernetes, bucket)

		cluster := options.Generate(kubernetes)
		cluster.Spec.Networking.ExposedFeatures = couchbasev2.ExposedFeatureList{
			couchbasev2.FeatureClient,
		}
		cluster = e2eutil.MustNewClusterFromSpec(t, kubernetes, cluster)

		// Verify that all nodes advertise an IP based alternate address.
		e2eutil.MustCheckForIPAlternateAddresses(t, kubernetes, cluster, time.Minute)
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

	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
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

	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
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
	subjectAltNames := x509.MandatorySANs(cluster.Name, cluster.Namespace)
	subjectAltNames = append(subjectAltNames, fmt.Sprintf("*.%s", newDomain))
	e2eutil.MustRotateServerCertificate(t, ctx, subjectAltNames)
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

	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
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

	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
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

	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
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
	subjectAltNames := x509.MandatorySANs(cluster.Name, cluster.Namespace)
	subjectAltNames = append(subjectAltNames, fmt.Sprintf("*.%s", newDomain))
	e2eutil.MustRotateServerCertificate(t, ctx, subjectAltNames)
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

	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
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

	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
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

	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
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
	// Platform configuration.
	f := framework.Global

	kubernetes, cleanup := f.SetupTest(t)
	defer cleanup()

	// Static configuration.
	clusterSize := 3

	// The enforcement stuff only appreared in 7.0.2... in true "add a new feature
	// to a bug fix release" form.
	framework.Requires(t, kubernetes).AtLeastVersion("7.0.2")

	// Create any old cluster and ensure all pods have dual stack enabled.
	cluster := clusterOptions().WithEphemeralTopology(clusterSize).MustCreate(t, kubernetes)
	e2eutil.MustExposePorts(t, kubernetes, cluster, couchbasev2.AFInet, false, time.Minute)

	// Set the mode explicitly to IPv4, expect the IPv6 ports to disappear.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Add("/spec/networking/addressFamily", couchbasev2.AFInet), time.Minute)
	e2eutil.MustExposePorts(t, kubernetes, cluster, couchbasev2.AFInet, true, time.Minute)

	// Revert the mode back to unset and esnure dial stack is restored.
	cluster = e2eutil.MustPatchCluster(t, kubernetes, cluster, jsonpatch.NewPatchSet().Remove("/spec/networking/addressFamily"), time.Minute)
	e2eutil.MustExposePorts(t, kubernetes, cluster, couchbasev2.AFInet, false, time.Minute)

	// Ensure the expected events were raised.
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Repeat{Times: 2, Validator: eventschema.Event{Reason: k8sutil.EventNetworkSettingsModified}},
	}

	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}
