package framework

import (
	"fmt"
	"strings"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"

	corev1 "k8s.io/api/core/v1"
)

// RegistryConfigValue allows multiple container image registries to be passed on the command
// line e.g:
// --registry https://index.docker.io/v1/,organization,password.
type RegistryConfigValue struct {
	values []RegistryConfig
}

func (v *RegistryConfigValue) Set(value string) error {
	fields := strings.Split(value, ",")
	if len(fields) != 3 {
		return fmt.Errorf("invalid cluster config value, expected SERVER,USERNAME,PASSWORD")
	}

	config := RegistryConfig{
		Server:   fields[0],
		Username: fields[1],
		Password: fields[2],
	}

	v.values = append(v.values, config)

	return nil
}

func (v *RegistryConfigValue) String() string {
	return ""
}

// TestConfigValue represents an explicit set of tests to run, so you can choose to
// not run a 12h suite and only a single 5m test.
type TestConfigValue struct {
	values []string
}

func (v *TestConfigValue) Set(value string) error {
	v.values = append(v.values, value)
	return nil
}

func (v *TestConfigValue) String() string {
	return ""
}

// SuiteConfigValue represents an explcit set of suites to run.
type SuiteConfigValue struct {
	values []string
}

func (v *SuiteConfigValue) Set(value string) error {
	v.values = append(v.values, value)
	return nil
}

func (v *SuiteConfigValue) String() string {
	return ""
}

// durationVar represents a duration of time, used to set the pod timeout.
type durationVar struct {
	value time.Duration
}

func (v *durationVar) Set(s string) error {
	value, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("duration invalid: %w", err)
	}

	v.value = value

	return nil
}

func (v *durationVar) Type() string {
	return "string"
}

func (v *durationVar) String() string {
	return v.value.String()
}

// bucketCompression represents the compression mode of the buckets created when testing.
type bucketCompression struct {
	value couchbasev2.CouchbaseBucketCompressionMode
}

func (c *bucketCompression) Set(s string) error {
	s = strings.ToLower(s)
	switch s {
	case "off":
		c.value = couchbasev2.CouchbaseBucketCompressionModeOff
	case "passive":
		c.value = couchbasev2.CouchbaseBucketCompressionModePassive
	case "active":
		c.value = couchbasev2.CouchbaseBucketCompressionModeActive
	default:
		return fmt.Errorf("invalid compression mode: %s", s)
	}

	return nil
}

func (c *bucketCompression) String() string {
	return string(c.value)
}

// bucketType represents the type of bucket created for tests.
type bucketType struct {
	value e2eutil.BucketType
}

func (b *bucketType) Set(s string) error {
	s = strings.ToLower(s)

	switch s {
	case "couchbase":
		b.value = e2eutil.BucketTypeCouchbase
	case "ephemeral":
		b.value = e2eutil.BucketTypeEphemeral
	case "memcached":
		b.value = e2eutil.BucketTypeMemcached
	default:
		return fmt.Errorf("invalid bucket type: %s", s)
	}

	return nil
}

func (b *bucketType) String() string {
	return string(b.value)
}

// istioMTLSMode is an Istio MTLS mode, either permissive or strict.
// We default to STRICT because that will catch more bugs, no one
// should be using permissive, and thus we shouldn't really be bothered
// with testing it unless in very specific circumstances.
type istioMTLSMode string

const (
	istioMTLSModePermissive istioMTLSMode = "PERMISSIVE"
	istioMTLSModeStrict     istioMTLSMode = "STRICT"
)

// istioMTLSModeFlag is an ISTIO TLS policy.
type istioMTLSModeFlag struct {
	value istioMTLSMode
}

func newIstioMTLSModeFlag() istioMTLSModeFlag {
	return istioMTLSModeFlag{value: istioMTLSModeStrict}
}

func (i *istioMTLSModeFlag) Set(s string) error {
	switch istioMTLSMode(s) {
	case istioMTLSModePermissive, istioMTLSModeStrict:
		i.value = istioMTLSMode(s)
		return nil
	}

	return fmt.Errorf("istio mTLS mode must be one of %v or %v", istioMTLSModePermissive, istioMTLSModeStrict)
}

func (i *istioMTLSModeFlag) Type() string {
	return "string"
}

func (i *istioMTLSModeFlag) String() string {
	return string(i.value)
}

// PullPolicyFlag implements flag.Value for passing pull policy command arguments.
type PullPolicyFlag struct {
	policy corev1.PullPolicy
}

func (p *PullPolicyFlag) Set(s string) error {
	switch corev1.PullPolicy(s) {
	case corev1.PullAlways, corev1.PullNever, corev1.PullIfNotPresent:
		p.policy = corev1.PullPolicy(s)
		return nil
	}

	return fmt.Errorf("failed to set unknown pull policy: %s", s)
}

func (p *PullPolicyFlag) String() string {
	return string(p.policy)
}

type TLSVer struct {
	version couchbasev2.TLSVersion
}

func (v *TLSVer) Set(s string) error {
	switch s {
	case "1.0":
		v.version = couchbasev2.TLS10
	case "1.1":
		v.version = couchbasev2.TLS11
	case "1.2":
		v.version = couchbasev2.TLS12
	case "1.3":
		v.version = couchbasev2.TLS13
	default:
		return fmt.Errorf("invalid TLS version: %s", s)
	}

	return nil
}

func (v *TLSVer) String() string {
	return string(v.version)
}
