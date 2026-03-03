/*
Copyright 2021-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package framework

import (
	"fmt"
	"strings"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
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
	return fmt.Sprint(c.value)
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
	return fmt.Sprint(b.value)
}
