package e2e

import (
	"testing"
	"time"

	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
)

// sdkConfig is a configuration for an SDK test suite.
type sdkConfig struct {
	// language is the SDK language e.g. Go, Python.
	language string

	// cccpImage is the old style "V2" library that can only connect to KV nodes.
	cccpImage string

	// gcccpImage is the new style "V3" library that can connect to any node.
	gcccpImage string
}

// testSDK is the common SDK tester, it checks V2 (CCCP) and V3 (GCCCP) against plaintext
// and TLS enabled clusters.
func testSDK(t *testing.T, local, remote *types.Cluster, config sdkConfig) {
	// Static configuration.
	clusterSize := 1

	// Create a TLS enabled cluster with a bucket.
	tls := e2eutil.MustInitClusterTLS(t, remote, &e2eutil.TLSOpts{})

	bucket := e2eutil.MustGetBucket(t, framework.Global.BucketType, framework.Global.CompressionMode)
	e2eutil.MustNewBucket(t, remote, bucket)

	cluster := e2eutil.MustNewXDCRCluster(t, remote, clusterSize, nil, tls, nil)

	// When ready run the tests!
	t.Run("V2", func(t *testing.T) {
		defer e2eutil.CleanupSDKResources(t, local)

		job := e2eutil.MustCreateSDKJob(t, local, remote, cluster, bucket, config.cccpImage, nil, true)
		e2eutil.MustWaitForSDKJobCompletion(t, local, job, time.Minute)
	})

	t.Run("V2/TLS", func(t *testing.T) {
		defer e2eutil.CleanupSDKResources(t, local)

		job := e2eutil.MustCreateSDKJob(t, local, remote, cluster, bucket, config.cccpImage, tls, true)
		e2eutil.MustWaitForSDKJobCompletion(t, local, job, time.Minute)
	})

	t.Run("V3", func(t *testing.T) {
		defer e2eutil.CleanupSDKResources(t, local)

		job := e2eutil.MustCreateSDKJob(t, local, remote, cluster, bucket, config.gcccpImage, nil, false)
		e2eutil.MustWaitForSDKJobCompletion(t, local, job, time.Minute)
	})

	t.Run("V3/TLS", func(t *testing.T) {
		defer e2eutil.CleanupSDKResources(t, local)

		job := e2eutil.MustCreateSDKJob(t, local, remote, cluster, bucket, config.gcccpImage, tls, false)
		e2eutil.MustWaitForSDKJobCompletion(t, local, job, time.Minute)
	})
}

// TestSDK tests basic connectivity of any SDKs that are defined.
func TestSDK(t *testing.T) {
	configs := []sdkConfig{
		{
			language:   "Go",
			cccpImage:  "spjmurray/gosdk:1.0.2",
			gcccpImage: "spjmurray/gosdk:2.0.4",
		},
	}

	for i := range configs {
		config := configs[i]

		test := func(t *testing.T) {
			test1 := func(t *testing.T) {
				k8s1 := framework.Global.GetCluster(0)

				testSDK(t, k8s1, k8s1, config)
			}

			t.Run("Local", test1)
		}

		t.Run(config.language, test)
	}
}
