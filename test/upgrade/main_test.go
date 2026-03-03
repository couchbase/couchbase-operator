/*
Copyright 2020-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
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
	flag.StringVar(&target, "target", "2.0.0", "Target version to upgrade from")
	flag.Parse()

	logrus.SetFormatter(&logrus.TextFormatter{ForceColors: true})

	os.Exit(m.Run())
}

// TestUpgrade installs a target version into the default namespace of the provided
// cluster.  This expects a crd.yaml, dac.yaml, operator.yaml and cluster.yaml.
// Once setup and verified, then an upgrade, specific to this release ensures it works.
func TestUpgrade(t *testing.T) {
	logrus.Infof("Upgrading from version %s", target)

	c, err := util.NewClients()
	util.Assert(t, err)

	// Load and create all CRDs.  Refresh the REST mapper afterwards once they
	// have been created.
	logrus.Info("Installing CRDs ...")

	crdResources, err := util.LoadYAMLs(fmt.Sprintf("targets/%s/crd.yaml", target))
	util.Assert(t, err)
	util.Assert(t, util.CreateResources(c, crdResources))
	util.Assert(t, c.Refresh())

	// Load and create the DAC.  We somehow need to ensure it is working before
	// proceeding.  We could just retry the cluster creation until that passes
	// CRD validation...
	logrus.Info("Installing DAC ...")

	dacResources, err := util.LoadYAMLs(fmt.Sprintf("targets/%s/dac.yaml", target))
	util.Assert(t, err)
	util.Assert(t, util.CreateResources(c, dacResources))

	util.MustWaitFor(t, util.ResourceCondition(c, "apps", "v1", "Deployment", "couchbase-operator-admission", "Available", "True"), 1*time.Minute)

	// Load and create the operator.  This is enventually consistent, so should
	// come up eventually.
	logrus.Info("Installing Operator ...")

	operatorResources, err := util.LoadYAMLs(fmt.Sprintf("targets/%s/operator.yaml", target))
	util.Assert(t, err)
	util.Assert(t, util.CreateResources(c, operatorResources))

	util.MustWaitFor(t, util.ResourceCondition(c, "apps", "v1", "Deployment", "couchbase-operator", "Available", "True"), 1*time.Minute)

	// Load and create the Couchbase cluster.  This should be a good representative
	// cluster with all the bells and whistles e.g. TLS and PVCs.  This we do need to
	// wait for.
	logrus.Info("Installing Cluster ...")

	clusterResources, err := util.LoadYAMLs(fmt.Sprintf("targets/%s/cluster.yaml", target))
	util.Assert(t, err)
	util.Assert(t, util.CreateResources(c, clusterResources))

	util.MustWaitFor(t, util.ResourceCondition(c, "couchbase.com", "v2", "CouchbaseCluster", "cb-example", "Available", "True"), 5*time.Minute)
	time.Sleep(2 * time.Minute) // This is actually broken on 2.0 and fires too early...

	// Load and update any cluster resources.
	logrus.Info("Updating Cluster ...")

	clusterUpdateResources, err := util.LoadYAMLs("pre-uninstall/cluster.yaml")
	util.Assert(t, err)
	util.Assert(t, util.ReplaceResources(c, clusterUpdateResources))

	util.MustWaitFor(t, util.ResourceEvent(c, "couchbase.com", "v2", "CouchbaseCluster", "cb-example", "TLSUpdated"), 5*time.Minute)
	time.Sleep(2 * time.Minute) // There is no event to say it's completed successfully on 2.0...

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

	dacResources, err = util.CouchbaseOperatorConfig("generate", "admission", "--image", "couchbase/couchbase-operator-admission:v1")
	util.Assert(t, err)
	util.Assert(t, util.CreateResources(c, dacResources))

	util.MustWaitFor(t, util.ResourceCondition(c, "apps", "v1", "Deployment", "couchbase-operator-admission", "Available", "True"), 1*time.Minute)

	// Install the new operator.
	logrus.Info("Installing Operator ...")

	operatorResources, err = util.CouchbaseOperatorConfig("generate", "operator", "--image", "couchbase/couchbase-operator:v1")
	util.Assert(t, err)
	util.Assert(t, util.CreateResources(c, operatorResources))

	util.MustWaitFor(t, util.ResourceCondition(c, "apps", "v1", "Deployment", "couchbase-operator", "Available", "True"), 1*time.Minute)

	util.MustWaitFor(t, util.ResourceEvent(c, "couchbase.com", "v2", "CouchbaseCluster", "cb-example", "UpgradeFinished"), 10*time.Minute)
}

// TestUpgradePiecemeal upgrades the CRD and DAC only, while leaving existing operators
// and clusters in place.  This facilitates doing upgrades on demand, or supporting multiple
// versions of the operator at the same time.
func TestUpgradePiecemeal(t *testing.T) {
	logrus.Infof("Upgrading from version %s", target)

	c, err := util.NewClients()
	util.Assert(t, err)

	// Load and create all CRDs.  Refresh the REST mapper afterwards once they
	// have been created.
	logrus.Info("Installing CRDs ...")

	crdResources, err := util.LoadYAMLs(fmt.Sprintf("targets/%s/crd.yaml", target))
	util.Assert(t, err)
	util.Assert(t, util.CreateResources(c, crdResources))
	util.Assert(t, c.Refresh())

	// Load and create the DAC.  We somehow need to ensure it is working before
	// proceeding.  We could just retry the cluster creation until that passes
	// CRD validation...
	logrus.Info("Installing DAC ...")

	dacResources, err := util.LoadYAMLs(fmt.Sprintf("targets/%s/dac.yaml", target))
	util.Assert(t, err)
	util.Assert(t, util.CreateResources(c, dacResources))

	util.MustWaitFor(t, util.ResourceCondition(c, "apps", "v1", "Deployment", "couchbase-operator-admission", "Available", "True"), 1*time.Minute)

	// Load and create the operator.  This is enventually consistent, so should
	// come up eventually.
	logrus.Info("Installing Operator ...")

	operatorResources, err := util.LoadYAMLs(fmt.Sprintf("targets/%s/operator.yaml", target))
	util.Assert(t, err)
	util.Assert(t, util.CreateResources(c, operatorResources))

	util.MustWaitFor(t, util.ResourceCondition(c, "apps", "v1", "Deployment", "couchbase-operator", "Available", "True"), 1*time.Minute)

	// Load and create the Couchbase cluster.  This should be a good representative
	// cluster with all the bells and whistles e.g. TLS and PVCs.  This we do need to
	// wait for.
	logrus.Info("Installing Cluster ...")

	clusterResources, err := util.LoadYAMLs(fmt.Sprintf("targets/%s/cluster.yaml", target))
	util.Assert(t, err)
	util.Assert(t, util.CreateResources(c, clusterResources))

	util.MustWaitFor(t, util.ResourceCondition(c, "couchbase.com", "v2", "CouchbaseCluster", "cb-example", "Available", "True"), 5*time.Minute)
	time.Sleep(2 * time.Minute) // This is actually broken on 2.0 and fires too early...

	// Load and update any cluster resources.
	logrus.Info("Updating Cluster ...")

	clusterUpdateResources, err := util.LoadYAMLs("pre-uninstall/cluster.yaml")
	util.Assert(t, err)
	util.Assert(t, util.ReplaceResources(c, clusterUpdateResources))

	util.MustWaitFor(t, util.ResourceEvent(c, "couchbase.com", "v2", "CouchbaseCluster", "cb-example", "TLSUpdated"), 5*time.Minute)
	time.Sleep(2 * time.Minute) // There is no event to say it's completed successfully on 2.0...

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

	dacResources, err = util.CouchbaseOperatorConfig("generate", "admission", "--image", "couchbase/couchbase-operator-admission:v1")
	util.Assert(t, err)
	util.Assert(t, util.CreateResources(c, dacResources))

	util.MustWaitFor(t, util.ResourceCondition(c, "apps", "v1", "Deployment", "couchbase-operator-admission", "Available", "True"), 1*time.Minute)
}
