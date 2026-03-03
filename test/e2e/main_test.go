/*
Copyright 2017-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2e

import (
	"net/http"
	_ "net/http/pprof"
	"os"
	"testing"

	"github.com/couchbase/couchbase-operator/test/e2e/analyzer"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"

	"github.com/sirupsen/logrus"

	_ "k8s.io/client-go/plugin/pkg/client/auth/azure"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

func TestMain(m *testing.M) {
	go func() {
		_ = http.ListenAndServe("localhost:6060", nil)
	}()

	// Perform any static initialization
	if err := framework.Init(); err != nil {
		logrus.Error(err)
		os.Exit(1)
	}

	if err := framework.Setup(); err != nil {
		logrus.Error(err)
		os.Exit(1)
	}

	// Run Test module
	code := m.Run()

	analyzer.Report(framework.SuiteName)

	os.Exit(code)
}
