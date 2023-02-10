package annotations

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type PointerStruct struct {
	Name    string
	Pointed *SimpleStruct `annotation:"pointed"`
}
type NestedStruct struct {
	Name   string
	Nested SimpleStruct `annotation:"nested"`
}

type SimpleStruct struct {
	Foo  string          `annotation:"foo"`
	Bar  int             `annotation:"bar"`
	Buzz metav1.Duration `annotation:"buzz"`
	Bang bool            `annotations:"bang"`
}

var aboutTen = metav1.Duration{Duration: time.Duration(10) * time.Second}

func TestSimpleEncode(t *testing.T) {
	annotations := map[string]string{
		"cao.couchbase.com/foo":  "foo",
		"cao.couchbase.com/bar":  "5",
		"cao.couchbase.com/buzz": "10s",
	}

	simple := SimpleStruct{Foo: "not foo", Bar: -1}
	expected := SimpleStruct{
		Foo:  "foo",
		Bar:  5,
		Buzz: aboutTen,
	}

	err := Populate(&simple, annotations)
	if err != nil {
		t.Fatalf("unexpected error. %v", err)
	}

	if !cmp.Equal(simple, expected) {
		t.Fatalf("Failed to annotate simple struct. found %v, expected %v", simple, expected)
	}
}

func TestSimpleEncodeRollBack(t *testing.T) {
	annotations := map[string]string{
		"cao.couchbase.com/foo":  "foo",
		"cao.couchbase.com/bar":  "bar", // trying to encode string into an int
		"cao.couchbase.com/buzz": "10s",
	}

	simple := SimpleStruct{Foo: "not foo", Bar: -1}
	expected := SimpleStruct{
		Foo: "not foo",
		Bar: -1,
	}

	err := Populate(&simple, annotations)
	if err == nil {
		t.Fatalf("expected an error")
	}

	if !cmp.Equal(simple, expected) {
		t.Fatalf("Failed to rollback simple struct. found %v, expected %v", simple, expected)
	}
}

func TestNestedStruct(t *testing.T) {
	annotations := map[string]string{
		"cao.couchbase.com/nested.foo":  "foo",
		"cao.couchbase.com/nested.bar":  "5",
		"cao.couchbase.com/nested.buzz": "10s",
	}

	nested := NestedStruct{}
	expected := NestedStruct{
		Nested: SimpleStruct{
			Foo:  "foo",
			Bar:  5,
			Buzz: aboutTen,
		},
	}

	err := Populate(&nested, annotations)
	if err != nil {
		t.Fatalf("unpexpected error. %v", err)
	}

	if !cmp.Equal(nested, expected) {
		t.Fatalf("Failed to annotate nested struct. found %v expected %v", nested, expected)
	}
}

func TestNestedStructRollback(t *testing.T) {
	annotations := map[string]string{
		"cao.couchbase.com/nested.foo":  "foo",
		"cao.couchbase.com/nested.bar":  "bar",
		"cao.couchbase.com/nested.buzz": "10s",
	}

	nested := NestedStruct{}
	expected := NestedStruct{
		Nested: SimpleStruct{},
	}

	err := Populate(&nested, annotations)
	if err == nil {
		t.Fatalf("expected error")
	}

	if !cmp.Equal(nested, expected) {
		t.Fatalf("Failed to rollback nested struct. found %v expected %v", nested, expected)
	}
}
func TestPointerStruct(t *testing.T) {
	annotations := map[string]string{
		"cao.couchbase.com/pointed.foo":  "foo",
		"cao.couchbase.com/pointed.bar":  "5",
		"cao.couchbase.com/pointed.buzz": "10s",
	}

	pointer := PointerStruct{}
	expected := PointerStruct{
		Pointed: &SimpleStruct{
			Foo:  "foo",
			Bar:  5,
			Buzz: aboutTen,
		},
	}

	err := Populate(&pointer, annotations)
	if err != nil {
		t.Fatalf("unexpected error. %v", err)
	}

	if !cmp.Equal(pointer, expected) {
		t.Fatalf("Failed to annotate pointer struct. found %v expected %v", pointer, expected)
	}
}

func TestPointerRollbackStruct(t *testing.T) {
	annotations := map[string]string{
		"cao.couchbase.com/pointed.foo":  "foo",
		"cao.couchbase.com/pointed.bar":  "bar",
		"cao.couchbase.com/pointed.buzz": "10s",
	}

	pointer := PointerStruct{}
	expected := PointerStruct{}

	err := Populate(&pointer, annotations)
	if err == nil {
		t.Fatalf("unexpected error. %v", err)
	}

	if !cmp.Equal(pointer, expected) {
		t.Fatalf("Failed to rollback pointer struct. found %v expected %v", pointer, expected)
	}
}
