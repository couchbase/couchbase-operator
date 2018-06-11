package e2e

import (
	"os"
	"testing"

	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	"github.com/sirupsen/logrus"
)

func TestMain(m *testing.M) {
	code := m.Run()

	if err := framework.Teardown(); err != nil {
		logrus.Errorf("fail to teardown framework: %v", err)
		os.Exit(1)
	}
	os.Exit(code)
}
