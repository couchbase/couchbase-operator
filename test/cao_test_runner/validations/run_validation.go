package validations

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
	"github.com/goccy/go-yaml"
)

var (
	ErrUnableToMarshal            = errors.New("failed to marshal")
	ErrFailedToMarshalConfig      = errors.New("failed to marshal config object")
	ErrFailedToReconcileValidator = errors.New("failed to reconcile validator")
)

func checkValidValidation(validate string) Validator {
	if v, ok := RegisterValidators()[validate]; ok {
		return v
	}

	return nil
}

func RunValidator(ctx *context.Context, validators []map[string]any, state string, testAssets assets.TestAssetGetterSetter) (bool, error) {
	for _, validatorMap := range validators {
		if v := checkValidValidation(validatorMap["name"].(string)); v != nil {
			var encoded []byte
			encoded, err := yaml.Marshal(validatorMap)
			if err != nil {
				return false, fmt.Errorf("run validator %w: %w", ErrFailedToMarshalConfig, err)
			}

			if err = yaml.NewDecoder(bytes.NewReader(encoded), yaml.Strict()).Decode(v); err != nil {
				return false, fmt.Errorf("run validator %w: %w", ErrUnableToMarshal, err)
			}

			if _, err := ctx.ReconcileConfig(v); err != nil {
				return false, fmt.Errorf("run validator %w: %w", ErrFailedToReconcileValidator, err)
			}

			if state == v.GetState() {
				if err = v.Run(ctx, testAssets); err != nil {
					return false, fmt.Errorf("run validator: %w", err)
				}
			}
		}
	}

	return true, nil
}
