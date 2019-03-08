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
