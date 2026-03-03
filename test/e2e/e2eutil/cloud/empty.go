/*
Copyright 2022-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package cloud

import (
	"testing"

	"github.com/couchbase/couchbase-operator/test/e2e/types"
	corev1 "k8s.io/api/core/v1"
)

type EmptyProvider struct {
}

func NewEmptyProvider() (Provider, error) {
	return &EmptyProvider{}, nil
}

func (provider *EmptyProvider) CreateBucket(string) error {
	return nil
}

func (provider *EmptyProvider) GetBucket(string) (bool, error) {
	return false, nil
}

func (provider *EmptyProvider) DeleteBucket(string) error {
	return nil
}

func (provider *EmptyProvider) CreateSecret(*types.Cluster) (*corev1.Secret, error) {
	return nil, nil
}

func (provider *EmptyProvider) SetupEnvironment(*testing.T, *types.Cluster) (*corev1.Secret, string, func()) {
	return nil, "", func() {}
}

func (provider *EmptyProvider) PrefixBucket(bucketName string) string {
	return bucketName
}
