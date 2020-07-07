package e2e

import (
	"testing"

	"github.com/couchbase/couchbase-operator/test/e2e/framework"
)

func TestOperator(t *testing.T) {
	f := framework.Global

	for _, testName := range f.SuiteYmlData.TestCase {
		if _, ok := TestFuncMap[testName]; !ok {
			t.Logf("Skipping %s.. Undefined test", testName)
			continue
		}

		t.Run(testName, TestFuncMap[testName])
	}
}
