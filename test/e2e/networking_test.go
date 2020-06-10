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

// TestExposedFeatureIP tests alternate addresses are populated with IP addresses with
// a basic cluster.
func TestExposedFeatureIP(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size1

	// Create the cluster.
	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)

	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.Spec.Networking.ExposedFeatures = couchbasev2.ExposedFeatureList{
		couchbasev2.FeatureClient,
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	// Verify that all nodes advertise an IP based alternate address.
	e2eutil.MustCheckForIPAlternateAddresses(t, targetKube, testCouchbase, time.Minute)
	e2eutil.MustCheckForNodeServiceType(t, targetKube, testCouchbase, corev1.ServiceTypeNodePort, time.Minute)
}

// TestExposedFeatureDNS tests alternate addresses are populated with DNS addresses with
// a DNS enabled cluster.
func TestExposedFeatureDNS(t *testing.T) {
	t.Skip("requires DDNS - addressability checks will fail without this")

	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

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

	ctx := e2eutil.MustInitClusterTLS(t, targetKube, tlsOptions)

	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)

	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.Name = clusterName
	testCouchbase.Spec.Networking.ExposedFeatures = couchbasev2.ExposedFeatureList{
		couchbasev2.FeatureClient,
	}
	testCouchbase.Spec.Networking.ExposedFeatureServiceType = corev1.ServiceTypeLoadBalancer
	testCouchbase.Spec.Networking.DNS = &couchbasev2.DNS{
		Domain: domain,
	}
	testCouchbase.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	// Verify that all nodes advertise a DNS based alternate address.
	e2eutil.MustCheckForDNSAlternateAddresses(t, targetKube, testCouchbase, domain, time.Minute)
	e2eutil.MustCheckForDNSServiceAnnotations(t, targetKube, testCouchbase, domain, time.Minute)
	e2eutil.MustCheckForNodeServiceType(t, targetKube, testCouchbase, corev1.ServiceTypeLoadBalancer, time.Minute)
}

// TestExposedFeatureDNSModify tests modifications to the DNS configuration are mirrored by
// node services.
func TestExposedFeatureDNSModify(t *testing.T) {
	t.Skip("requires DDNS - addressability checks will fail without this")

	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

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

	ctx := e2eutil.MustInitClusterTLS(t, targetKube, tlsOptions)

	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)

	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.Name = clusterName
	testCouchbase.Spec.Networking.ExposedFeatures = couchbasev2.ExposedFeatureList{
		couchbasev2.FeatureClient,
	}
	testCouchbase.Spec.Networking.ExposedFeatureServiceType = corev1.ServiceTypeLoadBalancer
	testCouchbase.Spec.Networking.DNS = &couchbasev2.DNS{
		Domain: domain,
	}
	testCouchbase.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	// Verify that all nodes advertise a DNS based alternate address, and it changes when updated.
	e2eutil.MustCheckForDNSAlternateAddresses(t, targetKube, testCouchbase, domain, time.Minute)
	e2eutil.MustCheckForDNSServiceAnnotations(t, targetKube, testCouchbase, domain, time.Minute)
	e2eutil.MustCheckForNodeServiceType(t, targetKube, testCouchbase, corev1.ServiceTypeLoadBalancer, time.Minute)
	subjectAltNames := x509.MandatorySANs(testCouchbase.Name, testCouchbase.Namespace)
	subjectAltNames = append(subjectAltNames, fmt.Sprintf("*.%s", newDomain))
	e2eutil.MustRotateServerCertificate(t, ctx, subjectAltNames)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/Networking/DNS/Domain", newDomain), time.Minute)
	e2eutil.MustCheckForDNSAlternateAddresses(t, targetKube, testCouchbase, newDomain, 5*time.Minute)
	e2eutil.MustCheckForDNSServiceAnnotations(t, targetKube, testCouchbase, newDomain, time.Minute)
	e2eutil.MustCheckForNodeServiceType(t, targetKube, testCouchbase, corev1.ServiceTypeLoadBalancer, time.Minute)
}

// TestExposedFeatureServiceTypeModify tests modifications to the node service type are mirrored
// by the node services.
func TestExposedFeatureServiceTypeModify(t *testing.T) {
	t.Skip("requires DDNS - addressability checks will fail without this")

	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

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

	ctx := e2eutil.MustInitClusterTLS(t, targetKube, tlsOptions)

	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)

	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.Name = clusterName
	testCouchbase.Spec.Networking.ExposedFeatures = couchbasev2.ExposedFeatureList{
		couchbasev2.FeatureClient,
	}
	testCouchbase.Spec.Networking.ExposedFeatureServiceType = corev1.ServiceTypeLoadBalancer
	testCouchbase.Spec.Networking.DNS = &couchbasev2.DNS{
		Domain: domain,
	}
	testCouchbase.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	// Verify that changing the node port type is reflected in the services.
	e2eutil.MustCheckForNodeServiceType(t, targetKube, testCouchbase, corev1.ServiceTypeLoadBalancer, time.Minute)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/Networking/ExposedFeatureServiceType", corev1.ServiceTypeNodePort), time.Minute)
	e2eutil.MustCheckForNodeServiceType(t, targetKube, testCouchbase, corev1.ServiceTypeNodePort, time.Minute)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/Networking/ExposedFeatureServiceType", corev1.ServiceTypeLoadBalancer), time.Minute)
	e2eutil.MustCheckForNodeServiceType(t, targetKube, testCouchbase, corev1.ServiceTypeLoadBalancer, time.Minute)
}

// TestConsoleServiceDNS tests the admin console service DNS annotation is set when
// DNS is configured.
func TestConsoleServiceDNS(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

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

	ctx := e2eutil.MustInitClusterTLS(t, targetKube, tlsOptions)

	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)

	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.Name = clusterName
	testCouchbase.Spec.Networking.ExposeAdminConsole = true
	testCouchbase.Spec.Networking.AdminConsoleServices = couchbasev2.ServiceList{
		couchbasev2.DataService,
	}
	testCouchbase.Spec.Networking.AdminConsoleServiceType = corev1.ServiceTypeLoadBalancer
	testCouchbase.Spec.Networking.DNS = &couchbasev2.DNS{
		Domain: domain,
	}
	testCouchbase.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	// Verify console service advertises a DNS based address.
	e2eutil.MustCheckForDNSAdminAnnotation(t, targetKube, testCouchbase, domain, time.Minute)
	e2eutil.MustCheckForConsoleServiceType(t, targetKube, testCouchbase, corev1.ServiceTypeLoadBalancer, time.Minute)
}

// TestConsoleServiceDNSModify tests modifications to the DNS configuration are mirrored by
// console service.
func TestConsoleServiceDNSModify(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

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

	ctx := e2eutil.MustInitClusterTLS(t, targetKube, tlsOptions)

	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)

	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.Name = clusterName
	testCouchbase.Spec.Networking.ExposeAdminConsole = true
	testCouchbase.Spec.Networking.AdminConsoleServices = couchbasev2.ServiceList{
		couchbasev2.DataService,
	}
	testCouchbase.Spec.Networking.AdminConsoleServiceType = corev1.ServiceTypeLoadBalancer
	testCouchbase.Spec.Networking.DNS = &couchbasev2.DNS{
		Domain: domain,
	}
	testCouchbase.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	// Verify that all nodes advertise a DNS based alternate address, and it changes when updated.
	e2eutil.MustCheckForDNSAdminAnnotation(t, targetKube, testCouchbase, domain, time.Minute)
	e2eutil.MustCheckForConsoleServiceType(t, targetKube, testCouchbase, corev1.ServiceTypeLoadBalancer, time.Minute)
	subjectAltNames := x509.MandatorySANs(testCouchbase.Name, testCouchbase.Namespace)
	subjectAltNames = append(subjectAltNames, fmt.Sprintf("*.%s", newDomain))
	e2eutil.MustRotateServerCertificate(t, ctx, subjectAltNames)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/Networking/DNS/Domain", newDomain), time.Minute)
	e2eutil.MustCheckForDNSAdminAnnotation(t, targetKube, testCouchbase, newDomain, 5*time.Minute)
	e2eutil.MustCheckForConsoleServiceType(t, targetKube, testCouchbase, corev1.ServiceTypeLoadBalancer, time.Minute)
}

// TestConsoleServiceTypeModify tests the console service type is updated when the configuration
// is updated.
func TestConsoleServiceTypeModify(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

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

	ctx := e2eutil.MustInitClusterTLS(t, targetKube, tlsOptions)

	bucket := e2eutil.MustGetBucket(t, f.BucketType, f.CompressionMode)
	e2eutil.MustNewBucket(t, targetKube, bucket)

	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.Name = clusterName
	testCouchbase.Spec.Networking.ExposeAdminConsole = true
	testCouchbase.Spec.Networking.AdminConsoleServices = couchbasev2.ServiceList{
		couchbasev2.DataService,
	}
	testCouchbase.Spec.Networking.AdminConsoleServiceType = corev1.ServiceTypeLoadBalancer
	testCouchbase.Spec.Networking.DNS = &couchbasev2.DNS{
		Domain: domain,
	}
	testCouchbase.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	// Verify that changing the node port type is reflected in the services.
	e2eutil.MustCheckForConsoleServiceType(t, targetKube, testCouchbase, corev1.ServiceTypeLoadBalancer, time.Minute)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/Networking/AdminConsoleServiceType", corev1.ServiceTypeNodePort), time.Minute)
	e2eutil.MustCheckForConsoleServiceType(t, targetKube, testCouchbase, corev1.ServiceTypeNodePort, time.Minute)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Replace("/Spec/Networking/AdminConsoleServiceType", corev1.ServiceTypeLoadBalancer), time.Minute)
	e2eutil.MustCheckForConsoleServiceType(t, targetKube, testCouchbase, corev1.ServiceTypeLoadBalancer, time.Minute)
}

// TestExposedFeatureTrafficPolicyCluster ensures an external traffic policy of
// Cluster doesn't cause the cluster to fail.
func TestExposedFeatureTrafficPolicyCluster(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

	// Static configuration.
	clusterSize := constants.Size3

	// Create the cluster.
	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.Spec.Networking.ExposedFeatures = couchbasev2.ExposedFeatureList{
		couchbasev2.FeatureAdmin,
	}
	policy := corev1.ServiceExternalTrafficPolicyTypeCluster
	testCouchbase.Spec.Networking.ExposedFeatureTrafficPolicy = &policy
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	// Check the events match what we expect:
	// * Cluster created
	expectedEvents := []eventschema.Validatable{
		eventschema.Repeat{Times: clusterSize, Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded}},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	}
	ValidateEvents(t, targetKube, testCouchbase, expectedEvents)
}

// TestLoadBalancerSourceRanges tests that we can create a cluster with IP
// source ranges set, remove them and add them back again, and observe that it
// is happening.
func TestLoadBalancerSourceRanges(t *testing.T) {
	// Platform configuration.
	f := framework.Global
	targetKube := f.GetCluster(0)

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

	ctx := e2eutil.MustInitClusterTLS(t, targetKube, tlsOptions)

	e2eutil.MustNewBucket(t, targetKube, e2espec.DefaultBucket)
	testCouchbase := e2espec.NewBasicCluster(clusterSize)
	testCouchbase.Name = clusterName
	testCouchbase.Spec.Networking.ExposeAdminConsole = true
	testCouchbase.Spec.Networking.AdminConsoleServiceTemplate = &couchbasev2.ServiceTemplateSpec{
		Spec: &corev1.ServiceSpec{
			Type:                     corev1.ServiceTypeLoadBalancer,
			LoadBalancerSourceRanges: sourceRanges,
		},
	}
	testCouchbase.Spec.Networking.DNS = &couchbasev2.DNS{
		Domain: domain,
	}
	testCouchbase.Spec.Networking.TLS = &couchbasev2.TLSPolicy{
		Static: &couchbasev2.StaticTLS{
			ServerSecret:   ctx.ClusterSecretName,
			OperatorSecret: ctx.OperatorSecretName,
		},
	}
	testCouchbase = e2eutil.MustNewClusterFromSpec(t, targetKube, testCouchbase)

	// Ensure the source ranges are correctly installed, then remove and verify, then
	// add back again and verify.
	e2eutil.MustCheckConsoleServiceLoadBalancerSourceRanges(t, targetKube, testCouchbase, sourceRanges, time.Minute)
	testCouchbase = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Remove("/Spec/Networking/AdminConsoleServiceTemplate/Spec/LoadBalancerSourceRanges"), time.Minute)
	e2eutil.MustCheckConsoleServiceLoadBalancerSourceRanges(t, targetKube, testCouchbase, nil, time.Minute)
	_ = e2eutil.MustPatchCluster(t, targetKube, testCouchbase, jsonpatch.NewPatchSet().Add("/Spec/Networking/AdminConsoleServiceTemplate/Spec/LoadBalancerSourceRanges", sourceRanges), time.Minute)
	e2eutil.MustCheckConsoleServiceLoadBalancerSourceRanges(t, targetKube, testCouchbase, sourceRanges, time.Minute)
}
