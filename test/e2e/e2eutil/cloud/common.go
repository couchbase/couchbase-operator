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

type ProviderType int

const (
	NoCloudProvider = iota
	CloudProviderAWS
	CloudProviderAzure
	CloudProviderGCP
)

type Provider interface {
	CreateBucket(string) error
	GetBucket(string) (bool, error)
	CreateSecret(*types.Cluster) (*corev1.Secret, error)
	DeleteBucket(string) error
	SetupEnvironment(*testing.T, *types.Cluster) (*corev1.Secret, string, func())
	PrefixBucket(string) string
}

func NewProvider(providerType ProviderType, creds ...string) (Provider, error) {
	switch providerType {
	case CloudProviderAWS:
		return NewAWSProvider(creds...)
	case CloudProviderAzure:
		return NewAzureProvider(creds...)
	case CloudProviderGCP:
		return NewGCPProvider(creds...)
	default:
		return NewEmptyProvider()
	}
}
