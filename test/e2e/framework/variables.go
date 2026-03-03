/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package framework

var (
	// TestDefinitions is a static list of all known tests.
	// This is defined by test/e2e/util_test.go.
	TestDefinitions TestDefList

	// SelectedTests is initialized during framework startup
	// and is a user defined subset of those defined above.
	SelectedTests TestDefList

	// Global holds contextual information for all tests.
	Global *Framework
)
