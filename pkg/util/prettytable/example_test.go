/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package prettytable

import (
	"os"
)

func ExampleTable_Write() {
	t := &Table{
		Header: Row{"Node", "Class", "Status"},
		Rows: []Row{
			{"cb-example-0003", "data", "managed+active"},
			{"cb-example-0004", "data", "managed+active"},
			{"cb-example-0005", "data", "managed+active"},
			{"cb-example-0006", "query", "managed+active"},
		},
	}

	_ = t.Write(os.Stdout)

	// Output:
	// ┌─────────────────┬───────┬────────────────┐
	// │ Node            │ Class │ Status         │
	// ├─────────────────┼───────┼────────────────┤
	// │ cb-example-0003 │ data  │ managed+active │
	// │ cb-example-0004 │ data  │ managed+active │
	// │ cb-example-0005 │ data  │ managed+active │
	// │ cb-example-0006 │ query │ managed+active │
	// └─────────────────┴───────┴────────────────┘
}
