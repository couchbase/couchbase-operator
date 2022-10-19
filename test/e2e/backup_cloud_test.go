package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"

	v2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil/cloud"
	"github.com/couchbase/couchbase-operator/test/e2e/framework"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func MustNewProvider(t *testing.T, kubernetes *types.Cluster, providerType cloud.ProviderType) cloud.Provider {
	f := framework.Global

	var creds []string

	switch providerType {
	case cloud.CloudProviderAWS:
		framework.Requires(t, kubernetes).AtLeastVersion("6.6.0").HasS3Parameters()

		creds = []string{f.S3AccessKey, f.S3SecretID, f.S3Region}

	case cloud.CloudProviderAzure:
		framework.Requires(t, kubernetes).AtLeastVersion("6.6.0").HasAzureParameters()

		creds = []string{f.AZAccountName, f.AZAccountKey}

	case cloud.CloudProviderGCP: //TODO: are these versions right?
		framework.Requires(t, kubernetes).AtLeastVersion("6.6.0").HasGCPParameters()

		creds = []string{f.GCPClientID, f.GCPClientSecret, f.GCPRefreshToken}
	default:
		break
	}

	provider, err := cloud.NewProvider(providerType, creds...)
	if err != nil {
		e2eutil.Die(t, err)
	}

	return provider
}

// Deprecated: Use Cloud provider methods instead.
func createS3Bucket(t *testing.T, bucket, accessKey, secretID, region, endpoint string, cert []byte) error {
	// create S3 bucket
	helper := e2eutil.AwsHelper(accessKey, secretID, region).WithEndpoint(endpoint).WithEndpointCert(cert).Create()

	// Create S3 service client
	svc := s3.New(helper.Sess)

	if MustGetS3Bucket(t, svc, bucket) {
		return nil
	}

	// Create the S3 Bucket
	_, err := svc.CreateBucket(&s3.CreateBucketInput{
		ACL:    aws.String("private"),
		Bucket: aws.String(bucket),
		CreateBucketConfiguration: &s3.CreateBucketConfiguration{
			LocationConstraint: aws.String(region),
		},
	})
	if err != nil {
		return err
	}

	// Make Objects of the bucket private
	if !strings.Contains(endpoint, "minio") { // minio doesn't support this action.
		_, err = svc.PutPublicAccessBlock(&s3.PutPublicAccessBlockInput{
			Bucket: aws.String(bucket),
			PublicAccessBlockConfiguration: &s3.PublicAccessBlockConfiguration{
				BlockPublicAcls:       aws.Bool(true),
				IgnorePublicAcls:      aws.Bool(true),
				BlockPublicPolicy:     aws.Bool(true),
				RestrictPublicBuckets: aws.Bool(true),
			},
		})

		if err != nil {
			return err
		}
	}

	err = svc.WaitUntilBucketExists(&s3.HeadBucketInput{
		Bucket: aws.String(bucket),
	})

	if err != nil {
		return fmt.Errorf("Error occurred while waiting for bucket to be created, %w", err)
	}

	return nil
}

// Deprecated: Use Cloud provider methods instead.
func MustCreateS3Bucket(t *testing.T, bucket, accessKey, secretID, region string, endpoint string, cert []byte) {
	if err := createS3Bucket(t, bucket, accessKey, secretID, region, endpoint, cert); err != nil {
		MustDeleteS3Bucket(t, bucket, accessKey, secretID, region, endpoint, cert)
		e2eutil.Die(t, err)
	}
}

// Deprecated: Use Cloud provider methods instead.
func getS3Bucket(svc *s3.S3, bucket string) (bool, error) {
	result, err := svc.ListBuckets(&s3.ListBucketsInput{})
	if err != nil {
		return false, err
	}

	var bucketPresent bool

	for _, s3bucket := range result.Buckets {
		if bucket == *s3bucket.Name {
			bucketPresent = true
			break
		}
	}

	return bucketPresent, nil
}

// Deprecated: Use Cloud provider methods instead.
func MustGetS3Bucket(t *testing.T, svc *s3.S3, bucket string) bool {
	bucketPresent, err := getS3Bucket(svc, bucket)
	if err != nil {
		e2eutil.Die(t, err)
	}

	return bucketPresent
}

// Deprecated: Use Cloud provider methods instead.
func deleteS3Bucket(t *testing.T, bucket, accessKey, secretID, region string, endpoint string, cert []byte) error {
	// create S3 bucket
	helper := e2eutil.AwsHelper(accessKey, secretID, region).WithEndpoint(endpoint).WithEndpointCert(cert).Create()

	// Create S3 service client
	svc := s3.New(helper.Sess)

	// Check if the bucket is present
	if bucketPresent := MustGetS3Bucket(t, svc, bucket); bucketPresent == false {
		return nil
	}

	// Empty the bucket before deleting it
	// Setup BatchDeleteIterator to iterate through a list of objects.
	iter := s3manager.NewDeleteListIterator(svc, &s3.ListObjectsInput{
		Bucket: aws.String(bucket),
	})

	// Traverse iterator deleting each object
	if err := s3manager.NewBatchDeleteWithClient(svc).Delete(aws.BackgroundContext(), iter); err != nil {
		return fmt.Errorf("Unable to delete objects from bucket %q, %w", bucket, err)
	}

	// Create the S3 Bucket
	_, err := svc.DeleteBucket(&s3.DeleteBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return fmt.Errorf("Bucket can not be deleted %w", err)
	}

	err = svc.WaitUntilBucketNotExists(&s3.HeadBucketInput{
		Bucket: aws.String(bucket),
	})

	if err != nil {
		return fmt.Errorf("Error occurred while waiting for bucket to be deleted, %w", err)
	}

	return nil
}

// Deprecated: Use Cloud provider methods instead.
func MustDeleteS3Bucket(t *testing.T, bucket, accessKey, secretID, region string, endpoint string, cert []byte) {
	if err := deleteS3Bucket(t, bucket, accessKey, secretID, region, endpoint, cert); err != nil {
		e2eutil.Die(t, err)
	}
}

// exact same as createS3Secret but with custom endpoint and cert.
func createObjEndpointS3Secret(t *testing.T, kubernetes *types.Cluster, endpoint string, cert []byte) (*corev1.Secret, string, func()) {
	f := framework.Global

	framework.Requires(t, kubernetes).AtLeastVersion("6.6.0")

	s3secret := "s3-secret"
	s3BucketName := "s3bucket-" + kubernetes.Namespace

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: s3secret,
		},
		Data: map[string][]byte{
			"region":            []byte(f.MinioRegion),
			"access-key-id":     []byte(f.MinioAccessKey),
			"secret-access-key": []byte(f.MinioSecretID),
		},
	}

	if _, err := kubernetes.KubeClient.CoreV1().Secrets(kubernetes.Namespace).Create(context.Background(), secret, metav1.CreateOptions{}); err != nil {
		e2eutil.Die(t, err)
	}

	MustCreateS3Bucket(t, s3BucketName, f.MinioAccessKey, f.MinioSecretID, f.MinioRegion, endpoint, cert)

	// Note: deferred functions must not call Die.
	cleanup := func() {
		_ = deleteS3Bucket(t, s3BucketName, f.MinioAccessKey, f.MinioSecretID, f.MinioRegion, endpoint, cert)
	}

	return secret, s3BucketName, cleanup
}

func createS3RegionSecret(t *testing.T, kubernetes *types.Cluster) (*corev1.Secret, string, func()) {
	f := framework.Global

	framework.Requires(t, kubernetes).AtLeastVersion("6.6.0").PlatformIs(v2.PlatformTypeAWS).HasS3Parameters()

	s3secret := "s3-secret"
	s3BucketName := "s3bucket-" + kubernetes.Namespace

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: s3secret,
		},
		Data: map[string][]byte{
			"region": []byte(f.S3Region),
		},
	}

	if _, err := kubernetes.KubeClient.CoreV1().Secrets(kubernetes.Namespace).Create(context.Background(), secret, metav1.CreateOptions{}); err != nil {
		e2eutil.Die(t, err)
	}

	MustCreateS3Bucket(t, s3BucketName, f.S3AccessKey, f.S3SecretID, f.S3Region, "", nil)

	// Note: deferred functions must not call Die.
	cleanup := func() {
		_ = deleteS3Bucket(t, s3BucketName, f.S3AccessKey, f.S3SecretID, f.S3Region, "", nil)
	}

	return secret, s3BucketName, cleanup
}
