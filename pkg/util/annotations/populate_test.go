package annotations

import (
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type InlineStruct struct {
	SimpleStruct `annotation:",inline"`
	Field        string `annotation:"field"`
}

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

type ArrayContainingStruct struct {
	Enabled       bool     `annotation:"enabled"`
	ProxyServices []string `annotation:"proxyServices"`
	ProxyBools    []bool   `annotation:"proxyBools"`
	ProxyInts     []int    `annotation:"proxyInts"`
}

type ProxyValues string

type ProxyValuesList []ProxyValues

type ProxyBools bool

type ProxyBoolsList []ProxyBools

type ProxyInts int
type ProxyIntsList []ProxyInts

type ArrayContainingIndirectStruct struct {
	Enabled          bool            `annotation:"enabled"`
	ProxyValues      ProxyValuesList `annotation:"proxyValues"`
	ProxyBoolsValues ProxyBoolsList  `annotation:"proxyBoolsValues"`
	ProxyIntsValues  ProxyIntsList   `annotation:"proxyIntsValues"`
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

func TestArrayEncoding(t *testing.T) {
	annotations := map[string]string{
		"cao.couchbase.com/enabled":       "true",
		"cao.couchbase.com/proxyServices": "mgmt,query",
		"cao.couchbase.com/proxyBools":    "true,false,true",
		"cao.couchbase.com/proxyInts":     "1,3,5,7",
	}

	container := ArrayContainingStruct{}
	expected := ArrayContainingStruct{
		Enabled:       true,
		ProxyServices: []string{"mgmt", "query"},
		ProxyBools:    []bool{true, false, true},
		ProxyInts:     []int{1, 3, 5, 7},
	}

	err := Populate(&container, annotations)
	if err != nil {
		t.Fatalf("unexpected error. %v", err)
	}

	if !cmp.Equal(container, expected) {
		t.Fatalf("Failed to annotate array struct. found %v expected %v", container, expected)
	}
}

func TestArrayIndirectEncoding(t *testing.T) {
	annotations := map[string]string{
		"cao.couchbase.com/enabled":          "true",
		"cao.couchbase.com/proxyValues":      "mgmt,query",
		"cao.couchbase.com/proxyBoolsValues": "true,false,true",
		"cao.couchbase.com/proxyIntsValues":  "1,3,5,7",
	}

	container := ArrayContainingIndirectStruct{}
	expected := ArrayContainingIndirectStruct{
		Enabled:          true,
		ProxyValues:      ProxyValuesList{"mgmt", "query"},
		ProxyBoolsValues: ProxyBoolsList{true, false, true},
		ProxyIntsValues:  ProxyIntsList{1, 3, 5, 7},
	}

	err := Populate(&container, annotations)
	if err != nil {
		t.Fatalf("unexpected error. %v", err)
	}

	if !cmp.Equal(container, expected) {
		t.Fatalf("Failed to annotate array struct. found %v expected %v", container, expected)
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

func TestInlineEncode(t *testing.T) {
	annotations := map[string]string{
		"cao.couchbase.com/foo":   "foo",
		"cao.couchbase.com/bar":   "5",
		"cao.couchbase.com/buzz":  "10s",
		"cao.couchbase.com/field": "field",
	}

	simple := InlineStruct{Field: "100"}
	expected := InlineStruct{
		Field: "field", SimpleStruct: SimpleStruct{
			Foo:  "foo",
			Bar:  5,
			Buzz: aboutTen,
		},
	}

	err := Populate(&simple, annotations)
	if err != nil {
		t.Fatalf("unexpected error. %v", err)
	}

	if !cmp.Equal(simple, expected) {
		t.Fatalf("Failed to annotate simple struct. found %v, expected %v", simple, expected)
	}
}

func TestPopulateWithOverwriteWarnings(t *testing.T) {
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

	warnings, err := PopulateWithWarnings(&simple, annotations)

	if err != nil {
		t.Fatalf("unexpected error. %v", err)
	}

	if !cmp.Equal(simple, expected) {
		t.Fatalf("Failed to annotate simple struct. found %v, expected %v", simple, expected)
	}

	if len(warnings) != 2 {
		t.Fatalf("Expected 2 warnings, got %d", len(warnings))
	}

	expectedWarningPrefix := "Overwriting existing value for annotation"
	for _, warning := range warnings {
		if !strings.HasPrefix(warning, expectedWarningPrefix) {
			t.Fatalf("Expected warning (%s) to start with: %s", warning, expectedWarningPrefix)
		}
	}
}

func TestPopulateWithMissingFieldWarnings(t *testing.T) {
	annotations := map[string]string{
		"cao.couchbase.com/foo":   "foo",
		"cao.couchbase.com/bar":   "5",
		"cao.couchbase.com/buzz":  "10s",
		"cao.couchbase.com/field": "field",
	}

	simple := SimpleStruct{}
	expected := SimpleStruct{
		Foo:  "foo",
		Bar:  5,
		Buzz: aboutTen,
	}

	warnings, err := PopulateWithWarnings(&simple, annotations)

	if err != nil {
		t.Fatalf("unexpected error. %v", err)
	}

	if !cmp.Equal(simple, expected) {
		t.Fatalf("Failed to annotate simple struct. found %v, expected %v", simple, expected)
	}

	if len(warnings) != 1 {
		t.Fatalf("Expected 1 warning, got %d", len(warnings))
	}

	expectedWarningPrefix := "No target found for annotation"
	for _, warning := range warnings {
		if !strings.HasPrefix(warning, expectedWarningPrefix) {
			t.Fatalf("Expected warning (%s) to start with: %s", warning, expectedWarningPrefix)
		}
	}
}
