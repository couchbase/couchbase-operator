// Package jsonpatch implements RFC6902: JavaScript Object Notation (JSON) Patch.
//
// Rather than operating on JSON encoded strings however this operates on
// native go types.  This allows arbitrary data structures to be modified
// and tested without having to hard code explicit variable access.  It
// also allows patches to be loaded and applied from a text based repository.
//
// For more details please see the specification:
// https://tools.ietf.org/html/rfc6902
package jsonpatch

import (
	"fmt"
	"reflect"
	"strconv"

	"github.com/couchbase/couchbase-operator/pkg/util/jsonpointer"
)

var ErrException = fmt.Errorf("caught an exception")
var ErrTypeError = fmt.Errorf("unsupported type")
var ErrOperationInvalid = fmt.Errorf("invalid operation type")

// Operation defines valid operation types.
type Operation string

const (
	Add     Operation = "add"
	Remove  Operation = "remove"
	Replace Operation = "replace"
	Move    Operation = "move"
	Copy    Operation = "copy"
	Test    Operation = "test"
)

// String returns the stringified version of an Operation.
func (op Operation) String() string {
	return string(op)
}

// Patch defines a valid JSON patch command.  The fields are optional and
// dependant on the operation you wish to perform.  In most cases you should
// use the PatchSet type to set the fields for you.
type Patch struct {
	Op    Operation   `json:"op"`
	Path  string      `json:"path,omitempty"`
	From  string      `json:"from,omitempty"`
	Value interface{} `json:"value,omitempty"`
}

// PatchList defines a list of JSON patches.
type PatchList []Patch

// add adds an element to the document.
func add(document interface{}, path string, value interface{}) (err error) {
	// Catch reflection errors
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: failed to add %s: %v", ErrException, path, r)
		}
	}()

	// Get a reference to the object to add to
	v, k, err := jsonpointer.LookupPath(document, path)
	if err != nil {
		err = fmt.Errorf("failed to lookup %s in add: %w", path, err)
		return
	}

	// Dereference pointer types
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	// If it's an object we add.  The special case is structs, as the
	// member already exists, so we just set it
	// If it's an array we insert the element at the specified index.
	switch kind := v.Kind(); kind {
	case reflect.Struct:
		v, err = jsonpointer.LookupValue(v, k)
		if err != nil {
			return
		}

		v.Set(reflect.ValueOf(value))
	case reflect.Map:
		v.SetMapIndex(reflect.ValueOf(k), reflect.ValueOf(value))
	case reflect.Slice:
		var index int

		switch k {
		case "-":
			index = v.Len()
		default:
			index, err = strconv.Atoi(k)
			if err != nil {
				return
			}
		}

		s := reflect.MakeSlice(v.Type(), 0, 0)
		s = reflect.AppendSlice(s, v.Slice(0, index))
		s = reflect.Append(s, reflect.ValueOf(value))
		s = reflect.AppendSlice(s, v.Slice(index, v.Len()))

		v.Set(s)
	default:
		err = fmt.Errorf("%w: unexpected kind %s in add", ErrTypeError, kind)
		return
	}

	return
}

// remove removes an element from the document.
func remove(document interface{}, path string) (err error) {
	// Catch reflection errors
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: failed to remove %s: %v", ErrException, path, r)
		}
	}()

	// Get a reference to the object to delete from
	v, k, err := jsonpointer.LookupPath(document, path)
	if err != nil {
		err = fmt.Errorf("failed to lookup %s in remove: %w", path, err)
		return
	}

	// Dereference pointer types
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	// If it's an object we delete.  The special case is structs, as we cannot
	// delete the member we instead set it to the zero value.
	// If it's an array we delete the element.
	switch kind := v.Kind(); kind {
	case reflect.Struct:
		v, err = jsonpointer.LookupValue(v, k)
		if err != nil {
			return
		}

		v.Set(reflect.Zero(v.Type()))
	case reflect.Map:
		v.SetMapIndex(reflect.ValueOf(k), reflect.Value{})
	case reflect.Slice:
		var index int

		index, err = strconv.Atoi(k)
		if err != nil {
			return
		}

		s := reflect.MakeSlice(v.Type(), 0, 0)
		s = reflect.AppendSlice(s, v.Slice(0, index))
		s = reflect.AppendSlice(s, v.Slice(index+1, v.Len()))

		v.Set(s)
	default:
		err = fmt.Errorf("%w: unexpected kind %s in remove", ErrTypeError, kind)
		return
	}

	return nil
}

// replace changes any field in an document.
func replace(document interface{}, path string, value interface{}) (err error) {
	// Catch reflection errors
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: failed to replace %s: %v", ErrException, path, r)
		}
	}()

	// Get a reference to the object to update
	v, k, err := jsonpointer.LookupPath(document, path)
	if err != nil {
		err = fmt.Errorf("failed to lookup %s in replace: %w", path, err)
		return
	}

	// Dereference pointer types
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	v, err = jsonpointer.LookupValue(v, k)
	if err != nil {
		return err
	}

	v.Set(reflect.ValueOf(value))

	return nil
}

// test looks up a value and compares it with an expected value.
func test(document interface{}, path string, value interface{}) error {
	// Get a reference to the object to update
	v, k, err := jsonpointer.LookupPath(document, path)
	if err != nil {
		return fmt.Errorf("%w: failed to lookup %s in test: %v", ErrException, path, err)
	}

	// Dereference pointer types
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	v, err = jsonpointer.LookupValue(v, k)
	if err != nil {
		return err
	}

	// If the value is nil then we need to use the type of the underlying
	// structure element, or value has no meaning.
	expected := reflect.ValueOf(value)
	if expected.Kind() == reflect.Invalid {
		expected = reflect.Zero(v.Type())
	}

	// Printing a nil map or slice interface{} will actually always display
	// an empty map or slice.  Here we intervene by explicitly setting the
	// interface{} to nil to show the different between a nil and empty map
	// or slice.
	v1 := v.Interface()

	switch v.Kind() {
	case reflect.Slice, reflect.Map:
		if v.IsNil() {
			v1 = nil
		}
	}

	v2 := expected.Interface()

	switch expected.Kind() {
	case reflect.Slice, reflect.Map:
		if expected.IsNil() {
			v2 = nil
		}
	}

	if !reflect.DeepEqual(v.Interface(), value) {
		return fmt.Errorf(`%w: values for "%s" do not match: actual %v %v, required %v %v`, ErrTypeError, path, v1, v.Type().String(), v2, expected.Type().String())
	}

	return nil
}

// Apply applies a patch set to a document.  Patches are applied in order, the
// function returns as soon as an error is detected.
func Apply(document interface{}, patches PatchList) (err error) {
	for _, patch := range patches {
		switch patch.Op {
		case Add:
			err = add(document, patch.Path, patch.Value)
		case Remove:
			err = remove(document, patch.Path)
		case Replace:
			err = replace(document, patch.Path, patch.Value)
		case Test:
			err = test(document, patch.Path, patch.Value)
		default:
			err = fmt.Errorf("%w: %s", ErrOperationInvalid, patch.Op.String())
		}

		if err != nil {
			return
		}
	}

	return
}

// PatchSet is a container for a patch set with an easy to use interface.
type PatchSet interface {
	// Add adds or replaces a value in an object or inserts into an array.  If the
	// target is a struct field then it is set to the specified value.
	Add(string, interface{}) PatchSet
	// Remove removes a value from an object or removes from an array.  If the
	// target is a struct field then it is set to the types zero value.
	Remove(string) PatchSet
	// Replace changes a pre-existing value in an object or array.
	Replace(string, interface{}) PatchSet
	// Test asserts that a value in an object or array matches what is expected.
	Test(string, interface{}) PatchSet
	// Patches returns the patch list for passing to Apply.
	Patches() PatchList
}

// patchSetImpl implements the PatchSet interface.
type patchSetImpl struct {
	// patches is an ordered list of patches to apply
	patches PatchList
}

// NewPatchSet returns a new initialized PatchSet object.
func NewPatchSet() PatchSet {
	return &patchSetImpl{
		patches: PatchList{},
	}
}

// Add adds or replaces a value in an object or inserts into an array.
func (p *patchSetImpl) Add(path string, value interface{}) PatchSet {
	p.patches = append(p.patches, Patch{Op: Add, Path: path, Value: value})
	return p
}

// Remove removes a value from an object or removes from an array.
func (p *patchSetImpl) Remove(path string) PatchSet {
	p.patches = append(p.patches, Patch{Op: Remove, Path: path})
	return p
}

// Replace changes a pre-existing value in an object or array.
func (p *patchSetImpl) Replace(path string, value interface{}) PatchSet {
	p.patches = append(p.patches, Patch{Op: Replace, Path: path, Value: value})
	return p
}

// Test asserts that a value in an object or array matches what is expected.
func (p *patchSetImpl) Test(path string, value interface{}) PatchSet {
	p.patches = append(p.patches, Patch{Op: Test, Path: path, Value: value})
	return p
}

// Patches returns the patch list for passing to Apply.
func (p *patchSetImpl) Patches() PatchList {
	return p.patches
}
