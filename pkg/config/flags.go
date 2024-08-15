package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type MetricLabel string

func (m *MetricLabel) String() string {
	return string(*m)
}

func (m *MetricLabel) Set(v string) error {
	switch v {
	case "uuid-only", "uuid-and-name":
		*m = MetricLabel(v)
		return nil
	default:
		return errors.New("must be one of 'uuid-only', 'uuid-and-name'")
	}
}

func (m *MetricLabel) Type() string {
	return "string"
}

// LabelSelectorVar allows parsing of a label selector from the CLI.
type LabelSelectorVar struct {
	LabelSelector *metav1.LabelSelector
}

// String returns the default label selector: none.
func (v *LabelSelectorVar) String() string {
	return ""
}

// Set parses a label selector in the form k=v,k=v.
func (v *LabelSelectorVar) Set(value string) error {
	if value == "" {
		return nil
	}

	pairs := strings.Split(value, ",")

	for _, pair := range pairs {
		kv := strings.Split(pair, "=")

		if len(kv) != 2 {
			return fmt.Errorf(`label selector "%s" not formatted as string=string`, pair)
		}

		if v.LabelSelector == nil {
			v.LabelSelector = &metav1.LabelSelector{
				MatchLabels: map[string]string{},
			}
		}

		v.LabelSelector.MatchLabels[kv[0]] = kv[1]
	}

	return nil
}

// Type returns the variable type.
func (v *LabelSelectorVar) Type() string {
	return "map"
}

// scopeVar describes the scope of a deployment, either cluser scope or namespace scope.
type scopeVar struct {
	value scopeType
}

// newScopeVar constructs a new variable with a default.
func newScopeVar(value scopeType) scopeVar {
	return scopeVar{
		value: value,
	}
}

// Set sets the variable from CLI input.
func (v *scopeVar) Set(s string) error {
	switch t := scopeType(s); t {
	case scopeCluster, scopeNamespace:
		v.value = t
	default:
		return fmt.Errorf("scope must be one of [%v, %v]", scopeCluster, scopeNamespace)
	}

	return nil
}

// Type returns the variable type.
func (v *scopeVar) Type() string {
	return "string"
}

// String returns the default value.
func (v *scopeVar) String() string {
	return string(v.value)
}

// durationVar contains a validated Go duration.
type durationVar struct {
	value time.Duration
}

// newDurationVar constructs a new variable with a default.
func newDurationVar(value time.Duration) durationVar {
	return durationVar{
		value: value,
	}
}

// Set sets the variable from CLI input.
func (v *durationVar) Set(s string) error {
	value, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("duration invalid: %w", err)
	}

	v.value = value

	return nil
}

// Type returns the variable type.
func (v *durationVar) Type() string {
	return "string"
}

// String returns the default value.
func (v *durationVar) String() string {
	return v.value.String()
}

// zapLogLevelVar contains a valid Operator log level.
type zapLogLevelVar struct {
	value string
}

// newZapLogLevelVar constructs a new variable with a default.
func newZapLogLevelVar(value string) zapLogLevelVar {
	return zapLogLevelVar{
		value: value,
	}
}

// Set sets the variable from CLI input.
func (v *zapLogLevelVar) Set(s string) error {
	switch s {
	case "info", "0", "debug", "1", "2":
		v.value = s
	default:
		return fmt.Errorf("log level must be one of [info, 0, debug, 1, 2]")
	}

	return nil
}

// Type returns the variable type.
func (v *zapLogLevelVar) Type() string {
	return "string"
}

// String returns the default value.
func (v *zapLogLevelVar) String() string {
	return v.value
}

// imagePullSecretVar contains image pull secret names.
type imagePullSecretVar struct {
	imagePullSecrets []string
}

// Set sets the variable from CLI input.
func (v *imagePullSecretVar) Set(s string) error {
	if v.imagePullSecrets == nil {
		v.imagePullSecrets = []string{}
	}

	v.imagePullSecrets = append(v.imagePullSecrets, s)

	return nil
}

// Type returns the variable type.
func (v *imagePullSecretVar) Type() string {
	return "string"
}

// String returns the default value.
func (v *imagePullSecretVar) String() string {
	return ""
}

// quantityVar allows the specification of quantity for platforms that
// require requests and limits.
type quantityVar struct {
	value resource.Quantity
}

// newQuantityVar returns a new initialized quantity.
func newQuantityVar(s string) quantityVar {
	return quantityVar{
		value: resource.MustParse(s),
	}
}

// Set sets the variable from CLI input.
func (v *quantityVar) Set(s string) error {
	value, err := resource.ParseQuantity(s)
	if err != nil {
		return err
	}

	v.value = value

	return nil
}

// Type returns the variable type.
func (v *quantityVar) Type() string {
	return "quantity"
}

// String returns the default value.
func (v *quantityVar) String() string {
	return v.value.String()
}
