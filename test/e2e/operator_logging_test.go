package e2e

import (
	"fmt"
	"testing"
	"time"

	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
)

// TestDynamicOperatorLoggingUsingClusterAnnotations tests if operator log level for the cluster
// is correctly set to the value specified with operatorLogLevel annotation.
func TestDynamicOperatorLoggingUsingClusterAnnotations(t *testing.T) {
	// Platform configuration.
	f := framework.Global

	kubernetes1, kubernetes2, cleanup := f.SetupTestRemote(t)
	defer cleanup()

	// Static configuration.
	cluster1Size := 1
	cluster2Size := 1

	// Create the clusters.
	cluster1 := clusterOptions().WithEphemeralTopology(cluster1Size).Generate(kubernetes1)
	cluster2 := clusterOptions().WithEphemeralTopology(cluster2Size).Generate(kubernetes2)

	if cluster1.GetAnnotations() == nil {
		cluster1.Annotations = make(map[string]string)
	}

	if cluster2.GetAnnotations() == nil {
		cluster2.Annotations = make(map[string]string)
	}

	cluster1.Annotations["cao.couchbase.com/operatorLogLevel"] = "2" // The test operator pod comes up with debug level
	cluster2.Annotations["cao.couchbase.com/operatorLogLevel"] = "info"

	cluster1 = e2eutil.CreateNewClusterFromSpec(t, kubernetes1, cluster1, -1)
	cluster2 = e2eutil.CreateNewClusterFromSpec(t, kubernetes2, cluster2, -1)

	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes1, cluster1, 3*time.Minute)
	e2eutil.MustWaitClusterStatusHealthy(t, kubernetes2, cluster2, 3*time.Minute)

	// Ensure that logs with -2 level are present in operator logs
	// Log structure: {"level":"Level(-2)", ... ,"cluster":"namespace/name"}
	expectedLevel1 := `"level":"Level(-2)"`
	expectedCluster1Payload := fmt.Sprintf(`"cluster":"%s/%s"`, cluster1.Namespace, cluster1.Name)
	e2eutil.MustFindOperatorLog(t, kubernetes1, cluster1, expectedLevel1, expectedCluster1Payload)

	// Cluster 2 assertions (Log Level info)
	// Log structure: {"level":"info", ... ,"cluster":"namespace/name"}
	expectedLevel2 := `"level":"info"`
	expectedCluster2Payload := fmt.Sprintf(`"cluster":"%s/%s"`, cluster2.Namespace, cluster2.Name)
	e2eutil.MustFindOperatorLog(t, kubernetes2, cluster2, expectedLevel2, expectedCluster2Payload)
}
