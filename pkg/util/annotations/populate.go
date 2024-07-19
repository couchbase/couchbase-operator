package annotations

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	AnnotationDelim  = "."
	AnnotationTag    = "annotation"
	AnnotationPrefix = "cao.couchbase.com/"
)

type InvalidPopulateError struct {
	Type reflect.Type
}

type FieldNotFoundError struct {
	Field string
}

func (e *FieldNotFoundError) Error() string {
	if e.Field == "" {
		return "no field provided"
	}

	return "could not find field " + e.Field
}

func (e *InvalidPopulateError) Error() string {
	if e.Type == nil {
		return "Populate(nil)"
	}

	if e.Type.Kind() != reflect.Pointer {
		return "Populate(non-pointer " + e.Type.String() + ")"
	}

	return "Populate(nil " + e.Type.String() + ")"
}

type PopulateState struct {
	rollback [][]reflect.Value // pointer to the original field with the original value
}

func NewPopulateState() *PopulateState {
	return &PopulateState{}
}

/*
Populate takes a pointer to some value a, and a map[string]string.
The key of this map represents paths within the struct for which the value should be set.
If a pointer is not passed, Populate will immediately return an error.

Populate will only populate a field with an annotation value if the field is exported.

Fields may be nested as long as their parent struct is tagged appropriately with an `annotation`.
THIS WILL INIT A STRUCT IF REQUIRED BY THE PATH.
Which means a struct will contain empty values, without the kubebuilder defaults.
*/
func Populate(a interface{}, annotations map[string]string) error {
	_, err := PopulateWithWarnings(a, annotations)
	return err
}

// PopulateWithWarnings is the same as Populate but returns a list of warnings for each CAO annotation that could not be applied.
func PopulateWithWarnings(a interface{}, annotations map[string]string) ([]string, error) {
	rv := reflect.ValueOf(a)
	if rv.Kind() != reflect.Ptr {
		return nil, &InvalidPopulateError{Type: reflect.TypeOf(a)}
	}

	failed := false
	state := NewPopulateState()

	defer func() {
		if failed {
			for _, v := range state.rollback {
				v[0].Set(v[1])
			}
		}
	}()

	warnings := []string{}

	for k, v := range annotations {
		if !strings.HasPrefix(k, AnnotationPrefix) {
			continue
		}

		path := k[len(AnnotationPrefix):]

		if field := state.FindField(a, strings.Split(path, AnnotationDelim)); field != nil {
			if field.IsValid() && field.CanSet() {
				if err := state.setField(field, v); err != nil {
					failed = true
					return warnings, err
				}
			}
		} else {
			warnings = append(warnings, fmt.Sprintf("No target found for annotation '%s:%s'. Annotation will have no effect.", k, v))
		}
	}

	return warnings, nil
}

func (state *PopulateState) setField(field *reflect.Value, value string) error {
	state.rollback = append(state.rollback, []reflect.Value{*field, reflect.ValueOf(field.Interface())})

	switch field.Kind() {
	case reflect.Struct:
		// we can only support metav1.Durations structs
		if field.Type() == reflect.TypeOf(metav1.Duration{}) {
			duration, err := time.ParseDuration(value)
			if err != nil {
				return err
			}

			field.Set(reflect.ValueOf(metav1.Duration{Duration: duration}))
		}
	case reflect.String:
		field.SetString(value)
	case reflect.Uint64:
		val, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return err
		}

		field.SetUint(val)
	case reflect.Int:
		intVal, err := strconv.Atoi(value)
		if err != nil {
			return err
		}

		field.SetInt(int64(intVal))
	case reflect.Bool:
		boolVal, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}

		field.SetBool(boolVal)
	}

	return nil
}

// given a path peforms a recursive search until the final field is found.
// this field can then be used to set values.
func (state *PopulateState) FindField(a interface{}, path []string) *reflect.Value {
	rv := reflect.ValueOf(a)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}

	if len(path) == 0 {
		return &rv
	}

	t := rv.Type()
	for i := 0; i < t.NumField(); i++ {
		st := t.Field(i)
		if !st.IsExported() {
			continue
		}

		tag, ok := st.Tag.Lookup(AnnotationTag)
		if !ok {
			continue
		}

		parts := strings.Split(tag, ",")
		name, options := parts[0], parts[1:]
		isInline := Contains(options, "inline")

		if path[0] != name && !isInline {
			continue
		}

		fv := rv.FieldByName(st.Name)

		if path[0] == name {
			// found it check if its requires following or the final field
			if foundField := state.getFieldFromStructField(st, fv, path); foundField != nil {
				return foundField
			}
		} else if fv.Kind() == reflect.Struct { // inline struct, check nested struct without updating path.
			if foundField := state.FindField(fv.Addr().Interface(), path); foundField != nil {
				return foundField
			}
		}
	}

	return nil
}

func (state *PopulateState) getFieldFromStructField(st reflect.StructField, fv reflect.Value, path []string) *reflect.Value {
	switch fv.Kind() {
	case reflect.Struct:
		// if this is a struct get a pointer to it and follow it
		return state.FindField(fv.Addr().Interface(), path[1:])
	case reflect.Pointer:
		if st.Type.Elem().Kind() == reflect.Struct {
			if fv.IsNil() {
				state.rollback = append(state.rollback, []reflect.Value{fv, reflect.Zero(fv.Type())})
				fv.Set(reflect.New(st.Type.Elem()))
			}

			return state.FindField(fv.Interface(), path[1:])
		}

		if fv.IsNil() {
			state.rollback = append(state.rollback, []reflect.Value{fv, reflect.Zero(fv.Type())})
			fv.Set(reflect.New(st.Type.Elem()))
		}

		fv = fv.Elem()

		return &fv

	default:
		return &fv
	}
}

func Contains[T comparable](elements []T, element T) bool {
	for _, x := range elements {
		if x == element {
			return true
		}
	}

	return false
}
