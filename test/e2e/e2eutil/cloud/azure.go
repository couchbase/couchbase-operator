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

type AzureProvider struct {
	client *azblob.ServiceClient
	creds  *Credentials
}

func NewAzureProvider(creds *Credentials) (Provider, error) {
	accountName := creds.accessKeyID
	accountKey := creds.secretAccessKey
	cred, err := azblob.NewSharedKeyCredential(accountName, accountKey)

	if err != nil {
		return nil, err
	}

	serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net", accountName)
	client, err := azblob.NewServiceClientWithSharedKey(serviceURL, cred, nil)

	if err != nil {
		return nil, err
	}

	return &AzureProvider{client: client, creds: creds}, nil
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

	_, err = provider.client.CreateContainer(ctx, bucketName, &azblob.ContainerCreateOptions{})
	if err != nil {
		return err
	}

	return nil
}

func (provider *AzureProvider) GetBucket(bucketName string) (bool, error) {
	pager := provider.client.ListContainers(nil)
	if pager.Err() != nil {
		return false, pager.Err()
	}

	ctx := context.Background()

	for pager.NextPage(ctx) {
		resp := pager.PageResponse()
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
			"access-key-id":     []byte(provider.creds.accessKeyID),
			"secret-access-key": []byte(provider.creds.secretAccessKey),
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
