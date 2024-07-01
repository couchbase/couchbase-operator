package jsonpatch

import (
	"errors"
	"reflect"

	"github.com/couchbase/couchbase-operator/pkg/util/jsonpointer"
)

var (
	ErrPathNotFoundInJSON = errors.New("failed to lookup path in json document")
)

func Get(document interface{}, path string) (interface{}, error) {
	// Get a reference to the object to update
	v, k, err := jsonpointer.LookupPath(document, path)
	if err != nil {
		return nil, ErrPathNotFoundInJSON
	}

	// Dereference pointer types
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	v, err = jsonpointer.LookupValue(v, k)
	if err != nil {
		return nil, err
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

	return v1, nil
}
