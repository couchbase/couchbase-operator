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
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type AWSCredentials struct {
	accessKeyID     string
	secretAccessKey string
	region          string
}
type AWSProvider struct {
	s3    *s3.Client
	creds *AWSCredentials
}

func NewAWSProvider(creds ...string) (Provider, error) {
	accessKeyID := creds[0]
	secretAccessKey := creds[1]
	region := creds[2]

	awsCreds := &AWSCredentials{
		accessKeyID: accessKeyID, secretAccessKey: secretAccessKey, region: region,
	}

	cfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, "")),
	)
	if err != nil {
		return nil, err
	}

	s3Svc := s3.NewFromConfig(cfg)
	provider := AWSProvider{s3: s3Svc, creds: awsCreds}

	return &provider, nil
}

func (provider *AWSProvider) CreateBucket(bucket string) error {
	svc := provider.s3
	ctx := context.Background()

	found, err := provider.GetBucket(bucket)
	if err != nil {
		return err
	}

	if found {
		return nil
	}

	// Create the S3 Bucket
	_, err = svc.CreateBucket(ctx, &s3.CreateBucketInput{
		ACL:    s3types.BucketCannedACLPrivate,
		Bucket: aws.String(bucket),
		CreateBucketConfiguration: &s3types.CreateBucketConfiguration{
			LocationConstraint: s3types.BucketLocationConstraint(provider.creds.region),
		},
	})

	if err != nil {
		return err
	}

	err = s3.NewBucketExistsWaiter(svc).Wait(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucket),
	}, 5*time.Minute)

	if err != nil {
		return fmt.Errorf("error occurred while waiting for bucket to be created, %w", err)
	}

	return nil
}

func (provider *AWSProvider) GetBucket(bucket string) (bool, error) {
	result, err := provider.s3.ListBuckets(context.Background(), &s3.ListBucketsInput{})
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

func (provider *AWSProvider) DeleteBucket(bucket string) error {
	// Check if the bucket is present
	found, err := provider.GetBucket(bucket)

	if err != nil {
		return err
	}

	if !found {
		return nil
	}

	ctx := context.Background()

	// A bucket has to be empty before we can delete it, so page through its
	// objects and delete each batch.
	paginator := s3.NewListObjectsV2Paginator(provider.s3, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("unable to list objects in bucket %q, %w", bucket, err)
		}

		if len(page.Contents) == 0 {
			continue
		}

		objectIDs := make([]s3types.ObjectIdentifier, 0, len(page.Contents))
		for _, obj := range page.Contents {
			objectIDs = append(objectIDs, s3types.ObjectIdentifier{Key: obj.Key})
		}

		if _, err := provider.s3.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(bucket),
			Delete: &s3types.Delete{Objects: objectIDs},
		}); err != nil {
			return fmt.Errorf("unable to delete objects from bucket %q, %w", bucket, err)
		}
	}

	// Now delete the bucket itself
	_, err = provider.s3.DeleteBucket(ctx, &s3.DeleteBucketInput{
		Bucket: aws.String(bucket),
	})

	if err != nil {
		return fmt.Errorf("bucket can not be deleted %w", err)
	}

	err = s3.NewBucketNotExistsWaiter(provider.s3).Wait(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucket),
	}, 5*time.Minute)

	if err != nil {
		return fmt.Errorf("error occurred while waiting for bucket to be deleted, %w", err)
	}

	return nil
}

// creates the secret containing s3 credentials.
func (provider *AWSProvider) CreateSecret(cluster *types.Cluster) (*corev1.Secret, error) {
	s3secret := "s3-secret"
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: s3secret,
		},
		Data: map[string][]byte{
			"region":            []byte(provider.creds.region),
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

func (provider *AWSProvider) SetupEnvironment(t *testing.T, cluster *types.Cluster) (*corev1.Secret, string, func()) {
	s3BucketName := "s3bucket-" + cluster.Namespace

	secret, err := provider.CreateSecret(cluster)

	if err != nil {
		e2eutil.Die(t, err)
	}

	err = provider.CreateBucket(s3BucketName)
	if err != nil {
		_ = provider.DeleteBucket(s3BucketName)

		e2eutil.Die(t, err)
	}

	cleanup := func() {
		_ = provider.DeleteBucket(s3BucketName)
	}

	return secret, s3BucketName, cleanup
}

func (provider *AWSProvider) PrefixBucket(bucketName string) string {
	return fmt.Sprintf("s3://%s", bucketName)
}
