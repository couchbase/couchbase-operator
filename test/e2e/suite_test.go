/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

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
