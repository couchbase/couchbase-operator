package e2e

import (
	"os"
	"testing"

	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	"github.com/sirupsen/logrus"

	_ "k8s.io/client-go/plugin/pkg/client/auth/azure"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

func init() {
	if err := framework.ReadYamlData(); err != nil {
		logrus.Error(err)
		os.Exit(1)
	}
}

func TestMain(m *testing.M) {
	// Call to start timer for test timeout
	framework.StartTimeoutTimer()

	// Run Test module
	code := m.Run()

	if err := framework.Teardown(); err != nil {
		logrus.Errorf("Failed to teardown framework: %v", err)
		os.Exit(1)
	}
	os.Exit(code)
}
