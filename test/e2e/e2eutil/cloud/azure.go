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
	"context"
	"fmt"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type AzureCredentials struct {
	accountName string
	accountKey  string
}
type AzureProvider struct {
	client *azblob.Client
	creds  *AzureCredentials
}

func NewAzureProvider(creds ...string) (Provider, error) {
	accountName := creds[0]
	accountKey := creds[1]

	azCreds := &AzureCredentials{
		accountName: accountName,
		accountKey:  accountKey,
	}
	cred, err := azblob.NewSharedKeyCredential(accountName, accountKey)

	if err != nil {
		return nil, err
	}

	serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net", accountName)
	client, err := azblob.NewClientWithSharedKeyCredential(serviceURL, cred, nil)

	if err != nil {
		return nil, err
	}

	return &AzureProvider{client: client, creds: azCreds}, nil
}

func (provider *AzureProvider) CreateBucket(bucketName string) error {
	ctx := context.Background()

	found, err := provider.GetBucket(bucketName)
	if err != nil {
		return err
	}

	if found {
		return nil
	}

	_, err = provider.client.CreateContainer(ctx, bucketName, &azblob.CreateContainerOptions{})
	if err != nil {
		return err
	}

	return nil
}

func (provider *AzureProvider) GetBucket(bucketName string) (bool, error) {
	pager := provider.client.NewListContainersPager(nil)
	ctx := context.Background()

	for pager.More() {
		resp, err := pager.NextPage(ctx)

		if err != nil {
			return false, err
		}

		for _, container := range resp.ContainerItems {
			if *container.Name == bucketName {
				return true, nil
			}
		}
	}

	return false, nil
}

func (provider *AzureProvider) DeleteBucket(bucketName string) error {
	ctx := context.Background()

	found, err := provider.GetBucket(bucketName)
	if err != nil {
		return err
	}

	if !found {
		return nil
	}

	_, err = provider.client.DeleteContainer(ctx, bucketName, nil)

	return err
}

func (provider *AzureProvider) CreateSecret(cluster *types.Cluster) (*corev1.Secret, error) {
	s3secret := "az-secret"
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: s3secret,
		},
		Data: map[string][]byte{
			"access-key-id":     []byte(provider.creds.accountName),
			"secret-access-key": []byte(provider.creds.accountKey),
		},
	}

	var err error
	if secret, err = cluster.KubeClient.CoreV1().Secrets(cluster.Namespace).Create(context.Background(), secret, metav1.CreateOptions{}); err != nil {
		return nil, err
	}

	return secret, nil
}

func (provider *AzureProvider) SetupEnvironment(t *testing.T, cluster *types.Cluster) (*corev1.Secret, string, func()) {
	bucketName := "azbucket-" + cluster.Namespace

	secret, err := provider.CreateSecret(cluster)

	if err != nil {
		e2eutil.Die(t, err)
	}

	err = provider.CreateBucket(bucketName)
	if err != nil {
		_ = provider.DeleteBucket(bucketName)

		e2eutil.Die(t, err)
	}

	cleanup := func() {
		_ = provider.DeleteBucket(bucketName)
	}

	return secret, bucketName, cleanup
}

func (provider *AzureProvider) PrefixBucket(bucketName string) string {
	return fmt.Sprintf("az://%s", bucketName)
}
