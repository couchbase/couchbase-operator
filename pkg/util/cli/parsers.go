/*
Copyright 2021-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

// package cli provides common helpers for interactive shell programming.
package cli

import (
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/pkg/errors"
)

// ParseYesNo accepts a 'y' or 'n' and returns a boolean.
func ParseYesNo(input string, defaultValue bool) (bool, error) {
	// Sanitise the input, it will probably have a trailling new line.
	input = strings.TrimSpace(input)

	// No input, this is fine
	if input == "" {
		return defaultValue, nil
	}

	if len(input) != 1 {
		return false, fmt.Errorf("%w: invalid input", errors.NewStackTracedError(errors.ErrUserInput))
	}

	switch input[0] {
	case 'y', 'Y':
		return true, nil
	case 'n', 'N':
		return false, nil
	}

	return false, fmt.Errorf("%w: invalid input", errors.NewStackTracedError(errors.ErrUserInput))
}
