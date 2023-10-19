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
