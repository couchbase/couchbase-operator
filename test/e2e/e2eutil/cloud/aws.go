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

	found, err := provider.GetBucket(bucket)
	if err != nil {
		return err
	}

	if found {
		return nil
	}

	// Create the S3 Bucket
	_, err = svc.CreateBucket(context.Background(), &s3.CreateBucketInput{
		ACL:    s3types.BucketCannedACLPrivate,
		Bucket: aws.String(bucket),
		CreateBucketConfiguration: &s3types.CreateBucketConfiguration{
			LocationConstraint: s3types.BucketLocationConstraint(provider.creds.region),
		},
	})

	if err != nil {
		return err
	}

	err = s3.NewBucketExistsWaiter(svc).Wait(context.Background(), &s3.HeadBucketInput{
		Bucket: aws.String(bucket),
	}, e2eutil.S3WaitTimeout)

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

	// Empty the bucket before deleting it
	if err := e2eutil.EmptyS3Bucket(context.Background(), provider.s3, bucket); err != nil {
		return fmt.Errorf("unable to delete objects from bucket %q, %w", bucket, err)
	}

	// Create the S3 Bucket
	_, err = provider.s3.DeleteBucket(context.Background(), &s3.DeleteBucketInput{
		Bucket: aws.String(bucket),
	})

	if err != nil {
		return fmt.Errorf("bucket can not be deleted %w", err)
	}

	err = s3.NewBucketNotExistsWaiter(provider.s3).Wait(context.Background(), &s3.HeadBucketInput{
		Bucket: aws.String(bucket),
	}, e2eutil.S3WaitTimeout)

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
