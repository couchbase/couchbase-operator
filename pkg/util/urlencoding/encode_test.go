package urlencoding

import (
	"strconv"
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

type Woo struct {
	a *int `url:"a,omitempty"`
	b *int `url:"b,empty=default"`
	c *int `url:"c,handleundefined"`
	d int  `url:"d,handleundefined"`
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

func TestEncodingHandleUndefined(t *testing.T) {
	zero := 0
	minusOne := -1
	fiveHundred := 500
	testcases := []struct {
		field    Woo
		expected string
	}{
		{
			field:    Woo{a: &zero, c: &zero, d: zero},
			expected: "a=0&b=default&c=0&d=undefined",
		},
		{
			field:    Woo{a: &zero, c: &minusOne, d: minusOne},
			expected: "a=0&b=default&c=-1",
		},
		{
			field:    Woo{a: &fiveHundred},
			expected: "a=500&b=default&c=undefined&d=undefined",
		},
		{
			field:    Woo{c: &minusOne, d: zero},
			expected: "b=default&c=-1&d=undefined",
		},
		{
			field:    Woo{a: &zero, b: nil, c: nil, d: zero},
			expected: "a=0&b=default&c=undefined&d=undefined",
		},
		{
			field:    Woo{a: &fiveHundred, b: &fiveHundred, c: &fiveHundred, d: fiveHundred},
			expected: "a=500&b=500&c=500&d=500",
		},
	}

	for _, testcase := range testcases {
		byteF, err := Marshal(testcase.field)

		if err != nil {
			t.Fatalf("Error marshalling args: %v", err)
		}

		if string(byteF) != testcase.expected {
			t.Errorf("expected %s, but got %s", testcase.expected, string(byteF))
		}
	}
}

// ImplementsStringer is a local test struct that implements fmt.Stringer, which is used to ensure encoding works using this custom string implementation.
type ImplementsStringer struct {
	FixedVal *int
	Setting  *string
}

// String implements fmt.Stringer.
func (t *ImplementsStringer) String() string {
	if t == nil {
		return ""
	}

	if t.FixedVal != nil {
		return strconv.Itoa(*t.FixedVal)
	}

	if t.Setting != nil {
		return *t.Setting
	}

	return ""
}

type ImplementsStringerType struct {
	FieldOmitsEmpty *ImplementsStringer `url:"omit_empty_field,omitempty"`
	FieldHasEmpty   *ImplementsStringer `url:"has_empty_field,empty=balanced"`
}

func TestImplementsStringerEncoding(t *testing.T) {
	intVal := 4
	stringVal := "balanced"

	testcases := []struct {
		field    ImplementsStringerType
		expected string
	}{
		{
			field:    ImplementsStringerType{FieldOmitsEmpty: &ImplementsStringer{FixedVal: &intVal}},
			expected: "has_empty_field=balanced&omit_empty_field=4",
		},
		{
			field:    ImplementsStringerType{FieldOmitsEmpty: &ImplementsStringer{Setting: &stringVal}},
			expected: "has_empty_field=balanced&omit_empty_field=balanced",
		},
		{
			field:    ImplementsStringerType{FieldOmitsEmpty: nil, FieldHasEmpty: &ImplementsStringer{FixedVal: &intVal}},
			expected: "has_empty_field=4",
		},
		{
			field:    ImplementsStringerType{FieldOmitsEmpty: nil, FieldHasEmpty: nil},
			expected: "has_empty_field=balanced",
		},
	}

	for _, testcase := range testcases {
		byteResult, err := Marshal(testcase.field)
		if err != nil {
			t.Fatalf("Error marshalling ImplementsStringerType: %v", err)
		}

		if string(byteResult) != testcase.expected {
			t.Errorf("expected %s, but got %s", testcase.expected, string(byteResult))
		}
	}
}
