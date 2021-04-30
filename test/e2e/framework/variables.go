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
