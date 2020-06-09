// Package jsonpointer implements RFC6901: JavaScript Object Notation (JSON) Pointer.
//
// Rather than operate on JSON encoded strings this operates on native go data
// structures allowing arbitrary fields to be modified and tested in a generic
// and programatic way.
//
// For more details please see the specification:
// https://tools.ietf.org/html/rfc6901
package jsonpointer

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

var ErrInvalidPointer = fmt.Errorf("invalid json pointer token")

// unescape translates escape characters back into their native form.
func unescape(token string) string {
	replacer := strings.NewReplacer("~1", "/", "~0", "~")
	return replacer.Replace(token)
}

// LookupValue return a reference to the named key in the containing object.
func LookupValue(v reflect.Value, k string) (value reflect.Value, err error) {
	// Reflection will panic, so be sure to catch and return an error.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("invalid token %s: %v: %w", k, r, ErrInvalidPointer)
		}
	}()

	// Set default return values.
	value = v

	// If an interface get the underlying typed value.
	if value.Kind() == reflect.Interface {
		value = reflect.ValueOf(value.Interface())
	}

	// Dereference if it's a pointer.
	if value.Kind() == reflect.Ptr {
		value = value.Elem()
	}

	// If the type we are reflecting on is an object (a struct or map)
	// then select the named member.
	// If the type we are reflecting on is an array we expect a numeric
	// index.
	switch kind := value.Kind(); kind {
	case reflect.Struct:
		value = value.FieldByName(k)
	case reflect.Map:
		value = value.MapIndex(reflect.ValueOf(k))
	case reflect.Slice:
		var i int

		if i, err = strconv.Atoi(k); err != nil {
			err = fmt.Errorf("malformed array index %s: %w", k, ErrInvalidPointer)
			return
		}

		value = value.Index(i)
	default:
		err = fmt.Errorf("unexpected kind %s in lookup: %w", kind, ErrInvalidPointer)
	}

	return
}

// LookupPath validates the pointer string, then iteratively descends through
// the specified object interface.  It returns a reference to the penultimate
// value in the pointer, and the last token so that it may be used to perform
// specific operations e.g. add/remove on context specific types e.g. slice/map.
func LookupPath(object interface{}, pointer string) (value reflect.Value, key string, err error) {
	// Reflection will panic, so be sure to catch and return an error.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("invalid pointer %s: %v: %w", pointer, r, ErrInvalidPointer)
		}
	}()

	// Ensure the pointer is correctly specified.
	re := regexp.MustCompile(`^(/[\w\d~-]*)+$`)
	if !re.MatchString(pointer) {
		err = fmt.Errorf("malformed pointer %s: %w", pointer, ErrInvalidPointer)
		return
	}

	// Split pointer into tokens and discard the first empty element.
	tokens := strings.Split(pointer, "/")[1:]

	// Translate escape sequences.
	for index, token := range tokens {
		tokens[index] = unescape(token)
	}

	// Get the first value in the chain and the key to add/update/remove etc.
	value = reflect.ValueOf(object)
	key = tokens[len(tokens)-1]

	// Iteratively descend through the structure.
	for _, token := range tokens[:len(tokens)-1] {
		value, err = LookupValue(value, token)
		if err != nil {
			return
		}
	}

	return
}
