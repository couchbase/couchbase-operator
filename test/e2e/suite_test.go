package e2e

import (
	"testing"

	"github.com/couchbase/couchbase-operator/test/e2e/framework"
)

func TestOperator(t *testing.T) {
	for _, test := range framework.SelectedTests {
		test.Run(t)
	}
}
