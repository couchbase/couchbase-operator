package jsonpatch

import (
	"testing"
)

type Fixture struct {
	A int
	B bool
	C string
	D float64
	E *Fixture
	F []int
	G []Fixture
	H []*Fixture
	I map[string]string
	J map[string]Fixture
	K map[string]*Fixture
}

var fixture = &Fixture{}

func mustAdd(t *testing.T, document interface{}, path string, value interface{}) {
	if err := add(document, path, value); err != nil {
		t.Fatal(err)
	}
}

func mustReplace(t *testing.T, document interface{}, path string, value interface{}) {
	if err := replace(document, path, value); err != nil {
		t.Fatal(err)
	}
}

func mustTest(t *testing.T, document interface{}, path string, value interface{}) {
	if err := test(document, path, value); err != nil {
		t.Fatal(err)
	}
}

func TestReplaceInt(t *testing.T) {
	mustReplace(t, fixture, "/A", 20)
	mustTest(t, fixture, "/A", 20)
}

func TestReplaceBool(t *testing.T) {
	mustReplace(t, fixture, "/B", true)
	mustTest(t, fixture, "/B", true)
}

func TestReplaceString(t *testing.T) {
	mustReplace(t, fixture, "/C", "dog")
	mustTest(t, fixture, "/C", "dog")
}

func TestReplaceFloat(t *testing.T) {
	mustReplace(t, fixture, "/D", 9.99)
	mustTest(t, fixture, "/D", 9.99)
}

func TestReplacePointer(t *testing.T) {
	mustReplace(t, fixture, "/E", &Fixture{A: 1})
	mustTest(t, fixture, "/E", &Fixture{A: 1})
}

func TestReplacePointerMember(t *testing.T) {
	mustReplace(t, fixture, "/E/A", 20)
	mustTest(t, fixture, "/E/A", 20)
}

func TestReplaceScalarSlice(t *testing.T) {
	mustReplace(t, fixture, "/F", []int{1})
	mustTest(t, fixture, "/F", []int{1})
}

func TestReplaceScalarSliceMember(t *testing.T) {
	mustReplace(t, fixture, "/F/0", 2)
	mustTest(t, fixture, "/F/0", 2)
}

func TestReplaceScalarSliceMemberAdd(t *testing.T) {
	mustAdd(t, fixture, "/F/-", 7)
	mustTest(t, fixture, "/F/1", 7)
}

func TestReplaceJsonPointerSlice(t *testing.T) {
	mustReplace(t, fixture, "/G", []Fixture{{A: 1}})
	mustTest(t, fixture, "/G", []Fixture{{A: 1}})
}

func TestReplaceJsonPointerSliceMember(t *testing.T) {
	mustReplace(t, fixture, "/G/0", Fixture{A: 2})
	mustTest(t, fixture, "/G/0", Fixture{A: 2})
}

func TestReplaceJsonPointerSliceMemberAdd(t *testing.T) {
	mustAdd(t, fixture, "/G/-", Fixture{A: 7})
	mustTest(t, fixture, "/G/1", Fixture{A: 7})
}

func TestReplaceJsonPointerSliceMemberProperty(t *testing.T) {
	mustReplace(t, fixture, "/G/0/A", 3)
	mustTest(t, fixture, "/G/0/A", 3)
}

func TestReplaceJsonPointerPointerSlice(t *testing.T) {
	mustReplace(t, fixture, "/H", []*Fixture{{A: 1}})
	mustTest(t, fixture, "/H", []*Fixture{{A: 1}})
}

func TestReplaceJsonPointerPointerSliceMember(t *testing.T) {
	mustReplace(t, fixture, "/H/0", &Fixture{A: 2})
	mustTest(t, fixture, "/H/0", &Fixture{A: 2})
}

func TestReplaceJsonPointerPointerSliceMemberAdd(t *testing.T) {
	mustAdd(t, fixture, "/H/-", &Fixture{A: 7})
	mustTest(t, fixture, "/H/1", &Fixture{A: 7})
}

func TestReplaceJsonPointerPointerSliceMemberProperty(t *testing.T) {
	mustReplace(t, fixture, "/H/0/A", 3)
	mustTest(t, fixture, "/H/0/A", 3)
}

func TestReplaceScalarMap(t *testing.T) {
	mustReplace(t, fixture, "/I", map[string]string{"name": "alice"})
	mustTest(t, fixture, "/I", map[string]string{"name": "alice"})
}

func TestReplaceJsonPointerMap(t *testing.T) {
	mustReplace(t, fixture, "/J", map[string]Fixture{"jsonpatch1": {A: 1}})
	mustTest(t, fixture, "/J", map[string]Fixture{"jsonpatch1": {A: 1}})
}

func TestReplaceJsonPointerPointerMap(t *testing.T) {
	mustReplace(t, fixture, "/K", map[string]*Fixture{"jsonpatch1": {A: 1}})
	mustTest(t, fixture, "/K", map[string]*Fixture{"jsonpatch1": {A: 1}})
}

func TestReplaceJsonPointerPointerMapMemberProperty(t *testing.T) {
	mustReplace(t, fixture, "/K/jsonpatch1/A", 3)
	mustTest(t, fixture, "/K/jsonpatch1/A", 3)
}
