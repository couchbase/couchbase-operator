package e2e

import (
	"testing"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
)

// testRotateAdminPassword is a basic sanity test to ensure the operator rotates
// the password on demand and that the test suite is able to observe it happening.
func testRotateAdminPassword(t *testing.T, kubernetes *types.Cluster, tls *e2eutil.TLSContext, policy *couchbasev2.ClientCertificatePolicy) {
	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	cluster := e2eutil.MustNewXDCRCluster(t, kubernetes, clusterSize, nil, tls, policy)

	// Rotate the password.
	e2eutil.MustRotateClusterPassword(t, kubernetes)

	// Check the events match what we expect:
	// * Cluster created
	// * Password rotated
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Optional{
			Validator: eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
		},
		eventschema.Event{Reason: k8sutil.EventReasonAdminPasswordChanged},
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestRotateAdminPassword(t *testing.T) {
	kubernetes := framework.Global.GetCluster(0)

	testRotateAdminPassword(t, kubernetes, nil, nil)
}

func TestRotateAdminPasswordTLS(t *testing.T) {
	kubernetes := framework.Global.GetCluster(0)

	tls := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})

	testRotateAdminPassword(t, kubernetes, tls, nil)
}

func TestRotateAdminPasswordMutualTLS(t *testing.T) {
	kubernetes := framework.Global.GetCluster(0)

	tls := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})

	policy := couchbasev2.ClientCertificatePolicyEnable
	testRotateAdminPassword(t, kubernetes, tls, &policy)
}

func TestRotateAdminPasswordMandatoryMutualTLS(t *testing.T) {
	kubernetes := framework.Global.GetCluster(0)

	tls := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})

	policy := couchbasev2.ClientCertificatePolicyMandatory
	testRotateAdminPassword(t, kubernetes, tls, &policy)
}

// testRotateAdminPasswordAndRestart tests that we operator continue to work across
// a restart.  Ensures that the operator wasn't relying on any cached state and the
// password has indeed been rotatated.
// Note: we could also validate the cluster persistence secret has the right value
// for absolute certainty.
func testRotateAdminPasswordAndRestart(t *testing.T, kubernetes *types.Cluster, tls *e2eutil.TLSContext, policy *couchbasev2.ClientCertificatePolicy) {
	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	cluster := e2eutil.MustNewXDCRCluster(t, kubernetes, clusterSize, nil, tls, policy)

	// Rotate the password and restart the operator.  We perform a simple
	// scaling operation so we can observe the operator actually being able
	// to talk to server.
	e2eutil.MustRotateClusterPassword(t, kubernetes)
	e2eutil.MustDeleteOperatorDeployment(t, kubernetes, time.Minute)
	e2eutil.MustCreateOperatorDeployment(t, kubernetes)
	cluster = e2eutil.MustResizeCluster(t, 0, clusterSize+1, kubernetes, cluster, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Password rotated
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Optional{
			Validator: eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
		},
		eventschema.Event{Reason: k8sutil.EventReasonAdminPasswordChanged},
		e2eutil.ClusterScaleUpSequence(1),
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestRotateAdminPasswordAndRestart(t *testing.T) {
	kubernetes := framework.Global.GetCluster(0)

	testRotateAdminPasswordAndRestart(t, kubernetes, nil, nil)
}

func TestRotateAdminPasswordAndRestartTLS(t *testing.T) {
	kubernetes := framework.Global.GetCluster(0)

	tls := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})

	testRotateAdminPasswordAndRestart(t, kubernetes, tls, nil)
}

func TestRotateAdminPasswordAndRestartMutualTLS(t *testing.T) {
	kubernetes := framework.Global.GetCluster(0)

	tls := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})

	policy := couchbasev2.ClientCertificatePolicyEnable
	testRotateAdminPasswordAndRestart(t, kubernetes, tls, &policy)
}

func TestRotateAdminPasswordAndRestartMandatoryMutualTLS(t *testing.T) {
	kubernetes := framework.Global.GetCluster(0)

	tls := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})

	policy := couchbasev2.ClientCertificatePolicyMandatory
	testRotateAdminPasswordAndRestart(t, kubernetes, tls, &policy)
}

// testRotateAdminPasswordDuringRestart tests that the operator uses the persistence
// sercet as its source of truth on restart so thus using the current admin password
// rather than the one in the user specified secret that has been updated.
func testRotateAdminPasswordDuringRestart(t *testing.T, kubernetes *types.Cluster, tls *e2eutil.TLSContext, policy *couchbasev2.ClientCertificatePolicy) {
	// Static configuration.
	clusterSize := 3

	// Create the cluster.
	cluster := e2eutil.MustNewXDCRCluster(t, kubernetes, clusterSize, nil, tls, policy)

	e2eutil.MustDeleteOperatorDeployment(t, kubernetes, time.Minute)
	e2eutil.MustRotateClusterPassword(t, kubernetes)
	e2eutil.MustCreateOperatorDeployment(t, kubernetes)
	cluster = e2eutil.MustResizeCluster(t, 0, clusterSize+1, kubernetes, cluster, 5*time.Minute)

	// Check the events match what we expect:
	// * Cluster created
	// * Password rotated
	expectedEvents := []eventschema.Validatable{
		e2eutil.ClusterCreateSequence(clusterSize),
		eventschema.Optional{
			Validator: eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
		},
		eventschema.Event{Reason: k8sutil.EventReasonAdminPasswordChanged},
		e2eutil.ClusterScaleUpSequence(1),
	}
	ValidateEvents(t, kubernetes, cluster, expectedEvents)
}

func TestRotateAdminPasswordDuringRestart(t *testing.T) {
	kubernetes := framework.Global.GetCluster(0)

	testRotateAdminPasswordDuringRestart(t, kubernetes, nil, nil)
}

func TestRotateAdminPasswordDuringRestartTLS(t *testing.T) {
	kubernetes := framework.Global.GetCluster(0)

	tls := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})

	testRotateAdminPasswordDuringRestart(t, kubernetes, tls, nil)
}

func TestRotateAdminPasswordDuringRestartMutualTLS(t *testing.T) {
	kubernetes := framework.Global.GetCluster(0)

	tls := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})

	policy := couchbasev2.ClientCertificatePolicyEnable
	testRotateAdminPasswordDuringRestart(t, kubernetes, tls, &policy)
}

func TestRotateAdminPasswordDuringRestartMandatoryMutualTLS(t *testing.T) {
	kubernetes := framework.Global.GetCluster(0)

	tls := e2eutil.MustInitClusterTLS(t, kubernetes, &e2eutil.TLSOpts{})

	policy := couchbasev2.ClientCertificatePolicyMandatory
	testRotateAdminPasswordDuringRestart(t, kubernetes, tls, &policy)
}
