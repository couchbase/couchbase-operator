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

type Hoo struct {
	a *int  `url:"a,omitempty"`
	b *bool `url:"b,omitempty"`
	c *bool `url:"c,omitempty"`
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

func TestEncodingBoolPointer(t *testing.T) {
	zero := 0
	f := false
	b := Hoo{a: &zero, b: nil, c: &f}

	byteB, err := Marshal(b)
	if err != nil {
		t.Fatalf("Error marshalling args: %v", err)
	}

	if string(byteB) != "a=0&c=false" {
		t.Errorf("expected %s, but got %s", "a=0&c=false", string(byteB))
	}
}

type String string

type Foo struct {
	a *String `url:"a,omitempty"`
}

func TestStringPointer(t *testing.T) {
	s := String("fooey")
	foo := Foo{a: &s}
	byteF, err := Marshal(foo)

	if err != nil {
		t.Fatalf("Error marshalling args: %v", err)
	}

	if string(byteF) != "a=fooey" {
		t.Errorf("expected %s, but got %s", "a=fooey", string(byteF))
	}
}
