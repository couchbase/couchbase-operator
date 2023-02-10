package annotations

import (
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
	rv := reflect.ValueOf(a)
	if rv.Kind() != reflect.Ptr {
		return &InvalidPopulateError{Type: reflect.TypeOf(a)}
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

	for k, v := range annotations {
		if !strings.HasPrefix(k, AnnotationPrefix) {
			continue
		}

		path := k[len(AnnotationPrefix):]

		if field := state.FindField(a, strings.Split(path, AnnotationDelim)); field != nil {
			if field.IsValid() && field.CanSet() {
				if err := state.setField(field, v); err != nil {
					failed = true
					return err
				}
			}
		} else {
			failed = true
			return &FieldNotFoundError{Field: k}
		}
	}

	return nil
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

		if path[0] == tag {
			// found it check if its requires following or the final field
			fv := rv.FieldByName(st.Name)
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

			default:
				return &fv
			}
		}
	}

	return nil
}
