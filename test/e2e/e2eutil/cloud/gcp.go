package cloud

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"cloud.google.com/go/storage"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type GCPCredentials struct {
	clientID     string
	clientSecret string
	refreshToken string
}

type GCPProvider struct {
	creds  GCPCredentials
	client *storage.Client
}

const gcpScopeFull = "https://www.googleapis.com/auth/devstorage.full_control"

func NewGCPProvider(creds ...string) (*GCPProvider, error) {
	clientID := creds[0]
	clientSecret := creds[1]
	refreshToken := creds[2]

	ctx := context.Background()
	config := (&oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     google.Endpoint,
		Scopes:       []string{gcpScopeFull},
	}).TokenSource(ctx, &oauth2.Token{RefreshToken: refreshToken})

	client, err := storage.NewClient(ctx, option.WithTokenSource(config))
	if err != nil {
		return nil, err
	}

	return &GCPProvider{client: client, creds: GCPCredentials{clientID: clientID, clientSecret: clientSecret, refreshToken: refreshToken}}, nil
}

func (provider *GCPProvider) CreateBucket(bucketName string) error {
	ctx := context.Background()

	bkt := provider.client.Bucket(bucketName)

	return bkt.Create(ctx, "couchbase-engineering", nil)
}

func (provider *GCPProvider) GetBucket(bucketName string) (bool, error) {
	ctx := context.Background()
	it := provider.client.Buckets(ctx, "couchbase-engineering")

	for {
		battrs, err := it.Next()

		if errors.Is(err, iterator.Done) {
			break
		}

		if err != nil {
			return false, err
		}

		if battrs.Name == bucketName {
			return true, nil
		}
	}

	return false, nil
}

func (provider *GCPProvider) DeleteBucket(bucketName string) error {
	// Check if the bucket is present
	found, err := provider.GetBucket(bucketName)

	if err != nil {
		return err
	}

	if !found {
		return nil
	}

	bucket := provider.client.Bucket(bucketName)

	if err := bucket.Delete(context.Background()); err != nil {
		return fmt.Errorf("Bucket(%q).Delete: %w", bucketName, err)
	}

	return nil
}

// creates the secret containing gcp credentials.
func (provider *GCPProvider) CreateSecret(cluster *types.Cluster) (*corev1.Secret, error) {
	s3secret := "gs-secret"
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: s3secret,
		},
		Data: map[string][]byte{
			"access-key-id":     []byte(provider.creds.clientID),
			"secret-access-key": []byte(provider.creds.clientSecret),
			"refresh-token":     []byte(provider.creds.refreshToken),
		},
	}

	var err error
	if secret, err = cluster.KubeClient.CoreV1().Secrets(cluster.Namespace).Create(context.Background(), secret, metav1.CreateOptions{}); err != nil {
		return nil, err
	}

	return secret, nil
}

func (provider *GCPProvider) SetupEnvironment(t *testing.T, cluster *types.Cluster) (*corev1.Secret, string, func()) {
	gcpBucketName := "gcpbucket-" + cluster.Namespace

	secret, err := provider.CreateSecret(cluster)

	if err != nil {
		e2eutil.Die(t, err)
	}

	err = provider.CreateBucket(gcpBucketName)
	if err != nil {
		_ = provider.DeleteBucket(gcpBucketName)

		e2eutil.Die(t, err)
	}

	cleanup := func() {
		_ = provider.DeleteBucket(gcpBucketName)
	}

	return secret, gcpBucketName, cleanup
}

func (provider *GCPProvider) PrefixBucket(bucketName string) string {
	return fmt.Sprintf("gs://%s", bucketName)
}
