package dataworkloads

import (
	"errors"
	"fmt"
	"math/rand"
	"reflect"
)

var (
	ErrNotStruct = errors.New("provided value is not a struct")
)

// BuildArgsList builds the arguments list from struct tags. If a parameter Param (value = "hello") has struct tag
// `args:"--param"`, it will be added to slice as: []string{"--param", "hello", ...}.
func BuildArgsList(v interface{}) ([]string, error) {
	var args []string

	value := reflect.ValueOf(v)
	typ := reflect.TypeOf(v)

	// Checking if it is a pointer
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
		typ = typ.Elem()
	}

	// Ensure the provided value is a struct.
	if value.Kind() != reflect.Struct {
		return nil, fmt.Errorf("build args for value=%+v: %w", v, ErrNotStruct)
	}

	// Iterate over the fields of the struct
	for i := 0; i < value.NumField(); i++ {
		field := value.Field(i)
		fieldType := typ.Field(i)

		if fieldTag := fieldType.Tag.Get("args"); fieldTag != "" {
			fieldValue := field.String()

			args = append(args, fieldTag)
			args = append(args, fieldValue)
		}
	}

	return args, nil
}

func GetRandomString(length int) string {
	letters := "abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, length)

	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}

	return string(b)
}
