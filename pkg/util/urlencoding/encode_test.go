/*
Copyright 2023-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package urlencoding

import (
	"testing"
)

type Boo struct {
	a *int `url:"a,omitempty"`
	b *int `url:"b,empty=default"`
}

func TestEncoding(t *testing.T) {
	zero := 0
	b := Boo{a: &zero, b: nil}

	byteB, err := Marshal(b)
	if err != nil {
		t.Fatalf("Error marshalling args: %v", err)
	}

	if string(byteB) != "a=0&b=default" {
		t.Errorf("expected %s, but got %s", "a=0&b=default", string(byteB))
	}
}
