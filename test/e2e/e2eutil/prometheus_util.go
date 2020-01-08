package e2eutil

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"testing"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/netutil"
	"github.com/couchbase/couchbase-operator/pkg/util/portforward"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// check that prometheus sidecar container is exporting the correct metrics
// on all pods in the operator
func CheckPrometheus(k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster) error {

	listOptions := &metav1.ListOptions{
		LabelSelector: constants.CouchbaseServerClusterKey + "=" + couchbase.Name,
	}
	pods, err := k8s.KubeClient.CoreV1().Pods(couchbase.Namespace).List(*listOptions)
	if err != nil {
		return err
	}

	// check all pods
	for _, pod := range pods.Items {
		port, err := netutil.GetFreePort()
		if err != nil {
			return fmt.Errorf("unable to allocate port %v", err)
		}

		pf := portforward.PortForwarder{
			Config:    k8s.Config,
			Client:    k8s.KubeClient,
			Namespace: k8s.Namespace,
			Pod:       pod.Name,
			Port:      port + ":9091",
		}
		if err := pf.ForwardPorts(); err != nil {
			return err
		}
		defer pf.Close()

		uri := fmt.Sprintf("http://localhost:%s%s", port, "/metrics")
		resp, err := http.Get(uri)
		if err != nil {
			fmt.Printf("unable to collect %s for pod %s\n", uri, pod.Name)
			continue
		}
		defer resp.Body.Close()

		// Buffer up the responses
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			fmt.Printf("unable to read response %s for pod %s\n", uri, pod.Name)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("remote call failed with response: %s %s", resp.Status, string(body))
		}

		responseDataStr := string(body)
		if len(responseDataStr) == 0 {
			return fmt.Errorf("empty response")
		}

		if !strings.Contains(responseDataStr, "couchbase") {
			return fmt.Errorf("response data does not contain any couchbase metrics")
		}
	}

	return nil
}

func MustCheckPrometheus(t *testing.T, k8s *types.Cluster, couchbase *couchbasev2.CouchbaseCluster) {
	if err := CheckPrometheus(k8s, couchbase); err != nil {
		Die(t, err)
	}
}
