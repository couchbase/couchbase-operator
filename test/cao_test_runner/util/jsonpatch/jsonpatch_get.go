package jsonpatch

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/sirupsen/logrus"
)

var (
	ErrDecodeError = errors.New("unable to decode")
)

// Get returns the value for the specified path in the document. Pass the document by reference.
// Usage:
//
//	yamlFile, err := os.ReadFile(pathToYAMLFile)
//	if err != nil {
//	return nil, fmt.Errorf("unmarshal yaml file: %w", err)
//	}
//
//	unmarshalledYAML := make(map[string]interface{})
//
//	err = yaml.Unmarshal(yamlFile, &unmarshalledYAML)
//	if err != nil {
//	return nil, fmt.Errorf("unmarshal yaml file: %w", err)
//	}
//	_, err = jsonpatchutil.Get(&yaml, spec)
//	// Handle the error
func Get(document interface{}, path string) (interface{}, error) {
	defer func() {
		if r := recover(); r != nil {
			logrus.Warnf("get data from path %s: recover: %+v", path, r)
		}
	}()

	// Get a reference to the object to update
	v, k, err := LookupPath(document, path)
	if err != nil {
		return nil, ErrPathNotFoundInJSON
	}

	// Dereference pointer types
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	v, err = LookupValue(v, k)
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

// GetString returns the value as string for the specified path in the document. Pass the document by reference.
func GetString(document interface{}, path string) (string, error) {
	value, err := Get(document, path)
	if err != nil {
		return "", fmt.Errorf("get string: %w", err)
	}

	if vString, ok := value.(string); ok {
		return vString, nil
	}

	return "", fmt.Errorf("get string `%v`: %w", value, ErrDecodeError)
}

// UnmarshalStringSlice unmarshal a list of interfaces to slice of strings.
func UnmarshalStringSlice(value interface{}) ([]string, error) {
	var stringSlice []string

	if vInterfaceSlice, ok := value.([]interface{}); ok {
		for _, vInterface := range vInterfaceSlice {
			if vString, ok := vInterface.(string); ok {
				stringSlice = append(stringSlice, vString)
			} else {
				return nil, fmt.Errorf("decode interface `%v`: %w", vInterface, ErrDecodeError)
			}
		}
	} else {
		return nil, fmt.Errorf("decode interface `%v`: %w", vInterfaceSlice, ErrDecodeError)
	}

	return stringSlice, nil
}
