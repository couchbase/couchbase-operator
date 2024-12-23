package workloads

import (
	"errors"
	"fmt"
	"math/rand"
	"reflect"
)

var (
	ErrNotStruct        = errors.New("provided value is not a struct")
	ErrNotSupportedType = errors.New("provided field type is not supported")
)

// BuildArgsList builds the arguments list from struct tags.
/*
 * Supported field types: string, []string.
 * If the tag is not present, the field will be ignored.
 * string   : `args:"--p"`, it will be added to slice as: []string{"--p", "value", ...}.
 * []string : `args:"--s"`, it will be added to slice as: []string{"--s", "value1", "--s", "value2", ...}.
 */
func BuildArgsList(tagName string, v interface{}) ([]string, error) {
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
	for i := range value.NumField() {
		field := value.Field(i)
		fieldType := typ.Field(i)

		if fieldTag := fieldType.Tag.Get(tagName); fieldTag != "" {
			switch field.Kind() {
			case reflect.Slice:
				{
					for j := range field.Len() {
						args = append(args, fieldTag)
						if field.Index(j).Kind() == reflect.String {
							args = append(args, field.Index(j).String())
						} else {
							return nil, fmt.Errorf("build args for field=%s with type=%v: %w", fieldType.Name, field.Index(j).Kind(), ErrNotSupportedType)
						}
					}
				}
			case reflect.String:
				{
					fieldValue := field.String()

					args = append(args, fieldTag)
					args = append(args, fieldValue)
				}
			default:
				{
					return nil, fmt.Errorf("build args for field=%s with type=%v: %w", fieldType.Name, field.Kind(), ErrNotSupportedType)
				}
			}
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
