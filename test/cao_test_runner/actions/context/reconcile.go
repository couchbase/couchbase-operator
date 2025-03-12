package context

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
)

var (
	ErrConfigNotAddr       = errors.New("config not addressable it shall be a pointer")
	ErrUnsupportedTag      = errors.New("unsupported tag")
	ErrFieldNotRegistered  = errors.New("field not registered")
	ErrConfigZeroValue     = errors.New("required config field has zero value")
	ErrValueNotConvertable = errors.New("could not convert value to required type")
	ErrFailedToSetValue    = errors.New("failed to set env var value")
	ErrFieldNil            = errors.New("field is nil")
	ErrFieldZeroValue      = errors.New("field has zero value")
	ErrEmptyEnvValue       = errors.New("env value is empty")
	ErrEmptyCtxEnvValue    = errors.New("context and env are empty")
)

const (
	// caoCliTagName holds the tag name for the cao test runner custom unmarshaller.
	caoCliTagName = "caoCli"

	// envTagName specifies that a field should be fetched from an environment variable.
	envTagName = "env"
)

// Tags below are defined under caoCliTagName. `caoCli:"required,context"`
const (
	// caoCliRequired specifies that a field must not be the zero value after reconciling config
	// i.e. after checking from yaml > context > env.
	caoCliRequired = "required"

	// caoCliContext specifies that a field must be taken from the context.
	caoCliContext = "context"
)

// ReconcileConfig takes in an action config and appropriately gets the value for the config fields.
/*
 * Check for the following tags: `caoCli:"required, context"` and `env:"ENV_VAR_NAME"`.
 * Recursively check for the tags in the struct fields. Checks in all nested supported types e.g. slices, structs and maps.
 * Reconciliation precedence: yaml > context > env.
 */
func (c *Context) ReconcileConfig(config interface{}) (interface{}, error) {
	// catch any panics just in case it ever happens.
	defer func() {
		if r := recover(); r != nil {
			logrus.Errorf("recover from a panic while reconciling config: %+v", r)
		}
	}()

	// some actions can have nil configs.
	if config == nil {
		return nil, nil
	}

	t := reflect.TypeOf(config).Elem()
	v := reflect.ValueOf(config).Elem()

	if !v.CanAddr() || !v.CanSet() {
		return nil, fmt.Errorf("%w : %s", ErrConfigNotAddr, v.Type())
	}

	errList := make([]error, 0)

	for i := range v.NumField() {
		if err := c.ReconcileStructField(t.Field(i), v.Field(i)); err != nil {
			errList = append(errList, err)
		}
	}

	if len(errList) != 0 {
		logrus.Errorf("Errors during reconciling:")
		for i, err := range errList {
			logrus.Errorf("%d: %v", i+1, err)
		}

		return config, errList[0]
	}

	return config, nil
}

// ReconcileStructField reconciles the config fields based on the caoCli and env tags present in the struct fields.
// NOTE: if the value is req and takes a zero value then it will be not be reconciled - it will throw an error.
// TODO add errList here otherwise only first error in recursion will be returned.
// TODO check if t can be removed.
func (c *Context) ReconcileStructField(t reflect.StructField, v reflect.Value) error {
	if v.CanSet() {
		logrus.Debugf("Start reconciling: %s %s = %+v", v.Type().String(), t.Name, v.Interface())
	}

	caoCliTagValue := t.Tag.Get(caoCliTagName)
	envTagValue := t.Tag.Get(envTagName)

	var isReq bool    // True if `caoCli:"required"` is present.
	var fromYaml bool // True if the value is present in yaml.
	var fromCtx bool  // True if the value is to be taken from context `caoCli:"context"`.
	var fromEnv bool  // True if the value is to be taken from env `env:"ENV_VAR_NAME"`.

	if caoCliTagValue != "" {
		caoCliValues := strings.Split(caoCliTagValue, ",")

		for _, caoCliValue := range caoCliValues {
			switch caoCliValue {
			case caoCliRequired:
				isReq = true
			case caoCliContext:
				fromCtx = true
			}
		}
	}

	if envTagValue != "" {
		fromEnv = true
	}

	// If it is a pointer we dereference it.
	if v.Kind() == reflect.Ptr {
		// If v is nil, but it is either required or to be filled from ctx / env then we assign an address to it.
		if v.IsNil() && (fromEnv || fromCtx || isReq) {
			v.Set(reflect.New(v.Type().Elem()))
		} else if v.IsNil() {
			return nil
		}

		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Struct:
		{
			if v.IsZero() && isReq {
				return fmt.Errorf("reconcile var %s %s : %w", t.Name, v.Type().String(), ErrFieldZeroValue)
			} else if v.IsZero() {
				return nil
			}

			tempV := v
			tempT := tempV.Type()

			for i := range tempT.NumField() {
				if err := c.ReconcileStructField(tempT.Field(i), tempV.Field(i)); err != nil {
					return fmt.Errorf("reconcile struct %s: %w", tempV.Type().String(), err)
				}
			}

			return nil
		}
	case reflect.Slice:
		{
			if v.IsNil() && isReq {
				return fmt.Errorf("reconcile var %s %s : %w", t.Name, v.Type().String(), ErrFieldNil)
			} else if v.IsNil() {
				return nil
			}

			for j := 0; j < v.Len(); j++ {
				tempV := v.Index(j)

				if err := c.ReconcileStructField(t, tempV); err != nil {
					return fmt.Errorf("reconcile var %s %s for idx %d : %w", t.Name, v.Type().String(), j, err)
				}
			}

			return nil
		}
	case reflect.Map:
		{
			if v.IsNil() && isReq {
				return fmt.Errorf("reconcile var %s %s : %w", t.Name, v.Type().String(), ErrFieldNil)
			} else if v.IsNil() {
				return nil
			}

			for _, mapKey := range v.MapKeys() {
				mapValue := v.MapIndex(mapKey)

				if err := c.ReconcileStructField(t, mapValue); err != nil {
					return fmt.Errorf("reconcile var %s %s for key %s: %w", t.Name, v.Type().String(), mapKey.String(), err)
				}
			}

			return nil
		}
	}

	// Precedence is yaml > ctx > env.
	// If context is empty then its value will be set using yaml/env. It will never be overridden.
	// If in the end the field has ZeroValue & field is required then we will throw an error.
	// Stating the combinations for yaml, context and env:
	/*
	 * yaml=true: yaml has value; ctx=true: take value from ctx (it may or may not have it); env=true: take value from env (it may or may not have it).
	 *
	 * yaml=true ; ctx=false; env=false - yaml.
	 * yaml=true ; ctx=true ; env=false - yaml; if context empty then set it.
	 * yaml=true ; ctx=true ; env=true  - yaml; if context empty then set it.
	 * yaml=true ; ctx=false; env=true  - yaml;
	 * yaml=false; ctx=true ; env=false - context.
	 * yaml=false; ctx=true ; env=true  - context > env; if ctx empty then set it.
	 * yaml=false; ctx=false; env=true  - env.
	 * yaml=false; ctx=false; env=false - none.
	 */

	// Here we come with the v and t having a struct field value and type. It won't be a slice, map, or struct.
	logrus.Debugf("Reconciling: %s %s = %+v", v.Type().Name(), t.Name, v.Interface())

	if !v.IsZero() {
		fromYaml = true
	}

	if !v.CanSet() {
		return fmt.Errorf("field %s %s: %w", t.Name, v.Type().String(), ErrFailedToSetValue)
	}

	if fromCtx {
		fieldName := t.Name

		fieldCtxKey, ok := contextUnmarshalKeys[fieldName]
		if !ok {
			return fmt.Errorf("%w :%s", ErrFieldNotRegistered, fieldName)
		}

		fieldCtxValue := ValueIDInterface(c.ctx, fieldCtxKey)

		if fieldCtxValue == nil && fromYaml {
			// If we have value from yaml and context is nil then we set it.
			c.WithIDInterface(fieldCtxKey, v.Interface())
		} else if fieldCtxValue != nil && !fromYaml {
			// If we don't have value from yaml and context is not nil then we take context value.
			fieldValueType := v.Type()
			ctxValueType := reflect.ValueOf(fieldCtxValue).Type()

			convertedValue, err := maybeConvertValue(fieldValueType, ctxValueType, fieldCtxValue)
			if err != nil {
				return fmt.Errorf("set context value '%s' into field '%s': %w", fieldCtxValue, fieldName, err)
			}

			v.Set(convertedValue)
		}
	}

	if fromEnv {
		if envValue, ok := os.LookupEnv(envTagValue); ok && envValue != "" && v.IsZero() {
			fieldValueType := v.Type()
			envValueType := reflect.ValueOf(envValue).Type()

			value, err := maybeConvertValue(fieldValueType, envValueType, envValue)
			if err != nil {
				return fmt.Errorf("field %s %s and env = %s: %w", t.Name, v.Type().String(), envValue, err)
			}

			v.Set(value)

			// If value has to be added into context
			if fromCtx {
				fieldName := t.Name

				fieldCtxKey, ok := contextUnmarshalKeys[fieldName]
				if !ok {
					return fmt.Errorf("%w :%s", ErrFieldNotRegistered, fieldName)
				}

				fieldCtxValue := ValueIDInterface(c.ctx, fieldCtxKey)
				if fieldCtxValue == nil && !v.IsZero() {
					c.WithIDInterface(fieldCtxKey, v.Interface())
				}
			}
		}
	}

	if isReq && v.IsZero() {
		return fmt.Errorf("required field %s %s: %w", t.Name, v.Type().String(), ErrFieldZeroValue)
	}

	logrus.Debugf("Reconciled: %s %s = %+v", v.Type().Name(), t.Name, v.Interface())

	return nil
}

func maybeConvertValue(expectedType, actualType reflect.Type, value interface{}) (reflect.Value, error) {
	if actualType == expectedType {
		// no need to convert the type
		return reflect.ValueOf(value), nil
	}

	if actualType.String() == "string" && expectedType.String() == "int" {
		val, err := strconv.Atoi(value.(string))
		if err != nil {
			return reflect.Value{}, fmt.Errorf("maybe convert value: %w", err)
		}

		return reflect.ValueOf(val).Convert(expectedType), nil
	}

	if actualType.String() == "string" && expectedType.String() == "bool" {
		val, err := strconv.ParseBool(value.(string))
		if err != nil {
			return reflect.Value{}, fmt.Errorf("maybe convert value: %w", err)
		}

		return reflect.ValueOf(val).Convert(expectedType), nil
	}

	if actualType.ConvertibleTo(expectedType) {
		// we have to convert the type
		return reflect.ValueOf(value).Convert(expectedType), nil
	}

	return reflect.Value{}, fmt.Errorf("maybe convert value: %w", ErrValueNotConvertable)
}
