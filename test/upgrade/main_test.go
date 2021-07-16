/*
Setup for CI
============
make
make container
kind delete cluster
kind create cluster
kind load docker-image couchbase/couchbase-operator:v1
kind load docker-image couchbase/couchbase-operator-admission:v1
docker pull couchbase/operator:2.1.0
kind load docker-image couchbase/operator:2.1.0
docker pull couchbase/admission-controller:2.1.0
kind load docker-image couchbase/admission-controller:2.1.0
docker pull couchbase/server:6.6.0
kind load docker-image couchbase/server:6.6.0
docker pull couchbase/server:6.6.2
kind load docker-image couchbase/server:6.6.2
go test ./test/upgrade -v -race
*/

package upgrade

import (
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/couchbase/couchbase-operator/test/upgrade/util"

	"github.com/sirupsen/logrus"
)

var (
	// Target is the name of the target directory to use for sourceing YAML.
	target string
)

// TestMain does any static configuration before the tests kick off.
func TestMain(m *testing.M) {
	flag.StringVar(&target, "target", "2.1.0", "Target version to upgrade from")
	flag.Parse()

	logrus.SetFormatter(&logrus.TextFormatter{ForceColors: true})

	os.Exit(m.Run())
}

// TestUpgradeFull installs a target version into the default namespace of the provided
// cluster.  This expects a crd.yaml, dac.yaml, operator.yaml and cluster.yaml.
// Once setup and verified, then an upgrade, specific to this release ensures it works.
func TestUpgradeFull(t *testing.T) {
	logrus.Infof("Upgrading from version %s", target)

	c, err := util.NewClients()
	util.Assert(t, err)

	util.Setup(t, c)
	defer util.Teardown(t, c)

	// Load and create all CRDs.  Refresh the REST mapper afterwards once they
	// have been created.
	logrus.Info("Installing CRDs ...")

	crdResources, err := util.LoadYAMLs(fmt.Sprintf("targets/%s/crd.yaml", target))
	util.Assert(t, err)
	util.Assert(t, util.CreateResources(c, crdResources))
	// Time delay!!  Need to wait until the CRDs are registered.
	time.Sleep(30 * time.Second)
	util.Assert(t, c.Refresh())

	// Load and create the DAC.  We somehow need to ensure it is working before
	// proceeding.  We could just retry the cluster creation until that passes
	// CRD validation...
	logrus.Info("Installing DAC ...")

	dacResources, err := util.LoadYAMLs(fmt.Sprintf("targets/%s/dac.yaml", target))
	util.Assert(t, err)
	util.Assert(t, util.CreateResources(c, dacResources))

	util.MustWaitFor(t, util.ResourceCondition(c, "apps", "v1", "Deployment", "default", "couchbase-operator-admission", "Available", "True"), 1*time.Minute)

	// Load and create the operator.  This is enventually consistent, so should
	// come up eventually.
	logrus.Info("Installing Operator ...")

	operatorResources, err := util.LoadYAMLs(fmt.Sprintf("targets/%s/operator.yaml", target))
	util.Assert(t, err)
	util.Assert(t, util.CreateResources(c, operatorResources))

	util.MustWaitFor(t, util.ResourceCondition(c, "apps", "v1", "Deployment", util.Namespace, "couchbase-operator", "Available", "True"), 1*time.Minute)

	// Load and create the Couchbase cluster.  This should be a good representative
	// cluster with all the bells and whistles e.g. TLS and PVCs.  This we do need to
	// wait for.
	logrus.Info("Installing Cluster ...")

	clusterResources, err := util.LoadYAMLs(fmt.Sprintf("targets/%s/cluster.yaml", target))
	util.Assert(t, err)
	util.Assert(t, util.CreateResources(c, clusterResources))

	util.MustWaitFor(t, util.ResourceCondition(c, "couchbase.com", "v2", "CouchbaseCluster", util.Namespace, "cb-example", "Available", "True"), 5*time.Minute)
	//	time.Sleep(2 * time.Minute) // This is actually broken on 2.0 and fires too early...

	// Load and update any cluster resources.
	// Problem: if we are coming from a specifc version, we may need to perform some
	// maintenance work first.  How do we specify this?  Do we just do it serially?
	// How do we know when these things have taken effect?
	//	logrus.Info("Updating Cluster ...")
	//
	//	clusterUpdateResources, err := util.LoadYAMLs("pre-uninstall/cluster.yaml")
	//	util.Assert(t, err)
	//	util.Assert(t, util.ReplaceResources(c, clusterUpdateResources))
	//
	//	util.MustWaitFor(t, util.ResourceEvent(c, "couchbase.com", "v2", "CouchbaseCluster", "cb-example", "TLSUpdated"), 5*time.Minute)
	//	time.Sleep(2 * time.Minute) // There is no event to say it's completed successfully on 2.0...

	// Uninstall the operator.
	logrus.Info("Uninstalling Operator ...")

	util.Assert(t, util.DeleteResources(c, operatorResources))

	// Uninstall the DAC.
	logrus.Info("Uninstalling DAC ...")

	util.Assert(t, util.DeleteResources(c, dacResources))

	// Replace the CRDs.
	logrus.Info("Replacing CRDs ...")

	crdResources, err = util.LoadYAMLs("../../example/crd.yaml")
	util.Assert(t, err)
	util.Assert(t, util.ReplaceOrCreateResources(c, crdResources))
	util.Assert(t, c.Refresh())

	// Install the new DAC.
	logrus.Info("Installing DAC ...")

	util.Assert(t, util.CouchbaseOperatorConfig("create", "admission", "--image", "couchbase/couchbase-operator-admission:v1"))
	util.MustWaitFor(t, util.ResourceCondition(c, "apps", "v1", "Deployment", "default", "couchbase-operator-admission", "Available", "True"), 1*time.Minute)

	// Install the new operator.
	logrus.Info("Installing Operator ...")

	util.Assert(t, util.CouchbaseOperatorConfig("create", "operator", "--image", "couchbase/couchbase-operator:v1", "--namespace", util.Namespace))
	util.MustWaitFor(t, util.ResourceCondition(c, "apps", "v1", "Deployment", util.Namespace, "couchbase-operator", "Available", "True"), 1*time.Minute)

	// Question: How do we indicate that an upgrade is going to happen?
	// Note: upgrades are bad from a usability perspective, try to avoid them.
	// util.MustWaitFor(t, util.ResourceEvent(c, "couchbase.com", "v2", "CouchbaseCluster", "cb-example", "UpgradeFinished"), 10*time.Minute)

	// Expect not to see an error condition.
	util.MustCheckFor(t, util.NoResourceCondition(c, "couchbase.com", "v2", "CouchbaseCluster", util.Namespace, "cb-example", "Error"), time.Minute)

	logrus.Info("Test complete.")
}

// TestUpgradePiecemeal upgrades the CRD and DAC only, while leaving existing operators
// and clusters in place.  This facilitates doing upgrades on demand, or supporting multiple
// versions of the operator at the same time.
func TestUpgradePiecemeal(t *testing.T) {
	logrus.Infof("Upgrading from version %s", target)

	c, err := util.NewClients()
	util.Assert(t, err)

	util.Setup(t, c)
	defer util.Teardown(t, c)

	// Load and create all CRDs.  Refresh the REST mapper afterwards once they
	// have been created.
	logrus.Info("Installing CRDs ...")

	crdResources, err := util.LoadYAMLs(fmt.Sprintf("targets/%s/crd.yaml", target))
	util.Assert(t, err)
	util.Assert(t, util.CreateResources(c, crdResources))
	// Time delay!!  Need to wait until the CRDs are registered.
	time.Sleep(30 * time.Second)
	util.Assert(t, c.Refresh())

	// Load and create the DAC.  We somehow need to ensure it is working before
	// proceeding.  We could just retry the cluster creation until that passes
	// CRD validation...
	logrus.Info("Installing DAC ...")

	dacResources, err := util.LoadYAMLs(fmt.Sprintf("targets/%s/dac.yaml", target))
	util.Assert(t, err)
	util.Assert(t, util.CreateResources(c, dacResources))

	util.MustWaitFor(t, util.ResourceCondition(c, "apps", "v1", "Deployment", "default", "couchbase-operator-admission", "Available", "True"), 1*time.Minute)

	// Load and create the operator.  This is enventually consistent, so should
	// come up eventually.
	logrus.Info("Installing Operator ...")

	operatorResources, err := util.LoadYAMLs(fmt.Sprintf("targets/%s/operator.yaml", target))
	util.Assert(t, err)
	util.Assert(t, util.CreateResources(c, operatorResources))

	util.MustWaitFor(t, util.ResourceCondition(c, "apps", "v1", "Deployment", util.Namespace, "couchbase-operator", "Available", "True"), 1*time.Minute)

	// Load and create the Couchbase cluster.  This should be a good representative
	// cluster with all the bells and whistles e.g. TLS and PVCs.  This we do need to
	// wait for.
	logrus.Info("Installing Cluster ...")

	clusterResources, err := util.LoadYAMLs(fmt.Sprintf("targets/%s/cluster.yaml", target))
	util.Assert(t, err)
	util.Assert(t, util.CreateResources(c, clusterResources))

	util.MustWaitFor(t, util.ResourceCondition(c, "couchbase.com", "v2", "CouchbaseCluster", util.Namespace, "cb-example", "Available", "True"), 5*time.Minute)
	//	time.Sleep(2 * time.Minute) // This is actually broken on 2.0 and fires too early...

	// Load and update any cluster resources.
	// Problem: if we are coming from a specifc version, we may need to perform some
	// maintenance work first.  How do we specify this?  Do we just do it serially?
	// How do we know when these things have taken effect?
	//	logrus.Info("Updating Cluster ...")

	//	clusterUpdateResources, err := util.LoadYAMLs("pre-uninstall/cluster.yaml")
	//	util.Assert(t, err)
	//	util.Assert(t, util.ReplaceResources(c, clusterUpdateResources))

	//	util.MustWaitFor(t, util.ResourceEvent(c, "couchbase.com", "v2", "CouchbaseCluster", "cb-example", "TLSUpdated"), 5*time.Minute)
	//	time.Sleep(2 * time.Minute) // There is no event to say it's completed successfully on 2.0...

	// Uninstall the DAC.
	logrus.Info("Uninstalling DAC ...")

	util.Assert(t, util.DeleteResources(c, dacResources))

	// Replace the CRDs.
	logrus.Info("Replacing CRDs ...")

	crdResources, err = util.LoadYAMLs("../../example/crd.yaml")
	util.Assert(t, err)
	util.Assert(t, util.ReplaceOrCreateResources(c, crdResources))
	util.Assert(t, c.Refresh())

	// Install the new DAC.
	logrus.Info("Installing DAC ...")

	util.Assert(t, util.CouchbaseOperatorConfig("create", "admission", "--image", "couchbase/couchbase-operator-admission:v1"))
	util.MustWaitFor(t, util.ResourceCondition(c, "apps", "v1", "Deployment", "default", "couchbase-operator-admission", "Available", "True"), 1*time.Minute)

	// Expect not to see an error condition.  Note this will only be relevant on
	// an upgrade from 2.2 as it has the requisite condition.
	util.MustCheckFor(t, util.NoResourceCondition(c, "couchbase.com", "v2", "CouchbaseCluster", util.Namespace, "cb-example", "Error"), time.Minute)

	logrus.Info("Test complete.")
}
