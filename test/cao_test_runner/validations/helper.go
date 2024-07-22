package validations

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/goccy/go-yaml"
	"github.com/sirupsen/logrus"
)

var (
	ErrUnableToMarshal       = errors.New("failed to marshal")
	ErrFailedToMarshalConfig = errors.New("failed to marshal config object")
)

func checkValidValidation(validate string) Validator {
	if v, ok := RegisterValidators()[validate]; ok {
		return v
	}

	return nil
}

func RunValidator(ctx *context.Context, validators []map[string]any, state string) (bool, error) {
	var errs []error

	for _, validatorMap := range validators {
		if v := checkValidValidation(validatorMap["name"].(string)); v != nil {
			var encoded []byte
			encoded, err := yaml.Marshal(validatorMap)

			if err != nil {
				logrus.Error(fmt.Errorf("%w: %w", ErrFailedToMarshalConfig, err))
				continue
			}

			if err = yaml.NewDecoder(bytes.NewReader(encoded), yaml.Strict()).Decode(v); err != nil {
				logrus.Error(fmt.Errorf("%w: %w", ErrUnableToMarshal, err))
				continue
			}

			if state == v.GetState() {
				if err = v.Run(ctx); err != nil {
					errs = append(errs, err)
				}
			}
		}
	}

	for _, err := range errs {
		if err != nil {
			return false, err
		}
	}

	return true, nil
}

func handlePanic() {
	if a := recover(); a != nil {
		fmt.Println("RECOVER", a)
	}
}
