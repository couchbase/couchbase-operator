package context

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
)

var (
	ErrCantAssign          = errors.New("cannot assign to the config of, shall be a pointer")
	ErrUnsupportedTag      = errors.New("unsupported tag")
	ErrFieldNotRegistered  = errors.New("field not register")
	ErrConfigZeroValue     = errors.New("required config field has zero value")
	ErrValueNotConvertable = errors.New("could not convert value to required type")
	ErrFailedToSetValue    = errors.New("failed to set env var value")
)

const (
	// caoCliTagString holds the tag string for the custom context unmarshaller.
	caoCliTagString = "caoCli"

	/* the below are the specific tags that are defined under caoCliTagString.
	contextTag specifies that a field can reside in the context,the context value takes priority over the yaml value.
	*/

	// The yaml value is to be placed into the context if the context is empty.
	contextTag = "context"
	// requiredTag specifies that a field must not be the zero value after checking the config and context.
	requiredTag = "required"
	// loggerTag specifies that a field should be added to the logger.
	// loggerTag = "logger"
	// envTag specifies that a field should be pulled from an environment variable.
	envTag = "env"
)

// ReconcileConfig takes in an action config and applies context values to it.
func (c *Context) ReconcileConfig(config interface{}) (interface{}, error) {
	// catch any panics just in case it ever happens.
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("recovered from a panic while attempting to unmarshal", r)
		}
	}()
	// some actions have nil configs.
	if config == nil {
		return nil, nil
	}

	t := reflect.TypeOf(config).Elem()
	v := reflect.ValueOf(config).Elem()

	if !v.CanAddr() || !v.CanSet() {
		return nil, fmt.Errorf("%w : %s", ErrCantAssign, t)
	}

	// iterate over all available fields and read the tag value.
	for i := 0; i < t.NumField(); i++ { //nolint:intrange
		fieldType := t.Field(i)
		fieldValue := v.Field(i)

		// get the field tag value
		tagString := fieldType.Tag.Get(caoCliTagString)
		if tagString == "" {
			// no tag we care about here.
			continue
		}

		envTag := fieldType.Tag.Get(envTag)

		// multiple tags are comma separated.
		tags := strings.Split(tagString, ",")

		// go through and work out what actions we must complete.
		contextTagFound := false
		requiredTagFound := false

		for _, tag := range tags {
			switch tag {
			case contextTag:
				contextTagFound = true
			case requiredTag:
				requiredTagFound = true
			default:
				return nil, fmt.Errorf("%w : %s", ErrUnsupportedTag, tag)
			}
		}

		name := fieldType.Name

		takeFromEnv := envTag != ""

		if contextTagFound {
			// this value is tagged as context
			// check that the field name is registered
			key, ok := contextUnmarshalKeys[name]
			if !ok {
				return nil, fmt.Errorf("%w :%s", ErrFieldNotRegistered, name)
			}

			ctxVal := ValueIDInterface(c.ctx, key)

			// If context value is nil and value already present in field (from yaml), the context will be set with fieldValue

			// If the context value is not nil and field value is nil, then the field value will be set from context

			// If the context value is not nil and field value is not nil, then the field value takes precedence.
			// Also this does not override the context value. The previous context value remains.
			// The override of the context value has to be taken care of by the action
			// What this implies is that the context value will be set the first time a fieldValue is encountered.
			// Further, the context value will not be overridden by the new fieldValues and that has to be taken care by the action.

			switch {
			case ctxVal == nil:
				// nothing is set in the context so we should put our config value in there.
				if !fieldValue.IsZero() {
					c.WithIDInterface(key, fieldValue.Interface())

					takeFromEnv = false
				}
			case !fieldValue.IsZero():
				takeFromEnv = false
			default:
				// values set via context take precedent over env vars.
				takeFromEnv = false

				if fieldValue.CanSet() {
					/* set our config value with the context value for types such as `type Volume string` we need to
					convert to allow setting the value.
					*/
					fieldValueType := fieldValue.Type()
					ctxValueType := reflect.ValueOf(ctxVal).Type()

					value, err := maybeConvertValue(fieldValueType, ctxValueType, ctxVal)
					if err != nil {
						return nil, fmt.Errorf("failed to set context value '%s' into field '%s': %w", ctxVal, name, err)
					}

					fieldValue.Set(value)
				}
			}
		}

		if takeFromEnv {
			envVal, ok := os.LookupEnv(envTag)
			if ok && envVal != "" && fieldValue.CanSet() {
				fieldValueType := fieldValue.Type()
				envValueType := reflect.ValueOf(envVal).Type()
				value, err := maybeConvertValue(fieldValueType, envValueType, envVal)

				if err != nil {
					return nil, fmt.Errorf("%w %s %s %w", ErrFailedToSetValue, envVal, name, err)
				}

				fieldValue.Set(value)

				key, ok := contextUnmarshalKeys[name]
				// set value in context if it's a context value.
				if ok {
					c.WithIDInterface(key, fieldValue.Interface())
				}
			}
		}

		if requiredTagFound && fieldValue.IsZero() {
			return nil, fmt.Errorf("%w:%s", ErrConfigZeroValue, name)
		}
	}

	return config, nil
}

func maybeConvertValue(expectedType, actualType reflect.Type, value interface{}) (reflect.Value, error) {
	if actualType == expectedType {
		// no need to convert the type
		return reflect.ValueOf(value), nil
	}

	if actualType.String() == "string" && expectedType.String() == "int" {
		val, err := strconv.Atoi(value.(string))
		if err != nil {
			return reflect.Value{}, err
		}

		return reflect.ValueOf(val).Convert(expectedType), nil
	}

	if actualType.String() == "string" && expectedType.String() == "bool" {
		val, err := strconv.ParseBool(value.(string))
		if err != nil {
			return reflect.Value{}, err
		}

		return reflect.ValueOf(val).Convert(expectedType), nil
	}

	if actualType.ConvertibleTo(expectedType) {
		// we have to convert the type
		return reflect.ValueOf(value).Convert(expectedType), nil
	}

	return reflect.Value{}, ErrValueNotConvertable
}
