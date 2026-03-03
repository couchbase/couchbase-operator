/*
Copyright 2019-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

// package diff provides helper functions to diff arbitrary objects for the purposes
// of debug and logging.
package diff

import (
	"github.com/couchbase/couchbase-operator/pkg/errors"

	"github.com/ghodss/yaml"
	"github.com/google/go-cmp/cmp"
)

// Diff takes a pair of objects, marshals them into YAML then generates a string
// diff of them.
func Diff(old, new interface{}) (string, error) {
	oldBytes, err := yaml.Marshal(old)
	if err != nil {
		return "", errors.NewStackTracedError(err)
	}

	newBytes, err := yaml.Marshal(new)
	if err != nil {
		return "", errors.NewStackTracedError(err)
	}

	return cmp.Diff(string(oldBytes), string(newBytes)), nil
}
