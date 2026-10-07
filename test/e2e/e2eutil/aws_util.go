/*
Copyright 2022-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2eutil

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/couchbase/couchbase-operator/pkg/config"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
)

type policyDocument struct {
	Version   string           `json:"version"`
	Statement []statementEntry `json:"statement"`
}
type statementEntry struct {
	Effect   string   `json:"effect"`
	Action   []string `json:"action"`
	Resource []string `json:"resource"`
}
type roleDocument struct {
	Version   string               `json:"version"`
	Statement []roleStatementEntry `json:"statement"`
}

type roleStatementEntry struct {
	Effect    string
	Principal map[string]string
	Action    string
	Condition map[string]map[string]string
}
type AWSUtil struct {
	Cfg       aws.Config
	pathStyle bool
	iam       *iam.Client
	cleanups  []func() error
	Policy    *iamtypes.Policy
	Role      *iamtypes.Role
}

type AWSHelperOptions struct {
	accessKey string
	secretID  string
	region    string
	endpoint  string
	cert      []byte
}

func AwsHelper(accessKey, secretID, region string) *AWSHelperOptions {
	return &AWSHelperOptions{
		accessKey: accessKey,
		secretID:  secretID,
		region:    region,
	}
}

func (o *AWSHelperOptions) WithEndpoint(endpoint string) *AWSHelperOptions {
	o.endpoint = endpoint

	return o
}

func (o *AWSHelperOptions) WithEndpointCert(cert []byte) *AWSHelperOptions {
	o.cert = cert

	return o
}

func (o *AWSHelperOptions) Create() *AWSUtil {
	token := ""

	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(o.region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(o.accessKey, o.secretID, token)),
	}

	helper := AWSUtil{}

	if o.endpoint != "" {
		opts = append(opts, awsconfig.WithBaseEndpoint(o.endpoint))
		helper.pathStyle = true
	}

	if o.cert != nil {
		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(o.cert)

		t := &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: caCertPool,
			},
		}
		client := http.Client{Transport: t, Timeout: 15 * time.Second}
		opts = append(opts, awsconfig.WithHTTPClient(&client))
	}

	cfg, err := awsconfig.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		panic(err)
	}

	helper.Cfg = cfg

	return &helper
}

// S3WaitTimeout is the longest we wait for a bucket to appear or disappear.
const S3WaitTimeout = 5 * time.Minute

// EmptyS3Bucket deletes every object in the bucket. aws-sdk-go-v2 has no BatchDelete.
func EmptyS3Bucket(ctx context.Context, svc *s3.Client, bucket string) error {
	pages := s3.NewListObjectsV2Paginator(svc, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
	})

	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return err
		}

		if len(page.Contents) == 0 {
			continue
		}

		ids := make([]s3types.ObjectIdentifier, 0, len(page.Contents))
		for _, obj := range page.Contents {
			ids = append(ids, s3types.ObjectIdentifier{Key: obj.Key})
		}

		out, err := svc.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(bucket),
			Delete: &s3types.Delete{Objects: ids},
		})
		if err != nil {
			return err
		}

		// Per-key failures come back in a 200 response.
		if len(out.Errors) > 0 {
			return fmt.Errorf("failed to delete %d objects, first %q: %s", len(out.Errors), aws.ToString(out.Errors[0].Key), aws.ToString(out.Errors[0].Message))
		}
	}

	return nil
}

// S3 returns an S3 client, using path style addressing when a custom endpoint is set.
func (helper *AWSUtil) S3() *s3.Client {
	return s3.NewFromConfig(helper.Cfg, func(o *s3.Options) {
		o.UsePathStyle = helper.pathStyle
	})
}

func (helper *AWSUtil) SetupBackupIAM(namespace, accountid, oidcProvider, s3Bucket string) error {
	if err := helper.createPolicy(s3Bucket); err != nil {
		return err
	}

	if err := helper.createRole(namespace, accountid, oidcProvider); err != nil {
		return err
	}

	return helper.attachPolicyToRole()
}

func MustSetupBackupIAM(t *testing.T, kubernetes *types.Cluster, aws *AWSUtil, accountid, oidcprovider, s3Bucket string) {
	if err := aws.SetupBackupIAM(kubernetes.Namespace, accountid, oidcprovider, s3Bucket); err != nil {
		aws.Cleanup()
		Die(t, err)
	}

	annotateServiceRoleWithIAM(t, kubernetes, *aws.Role.Arn)
}

func (helper *AWSUtil) attachPolicyToRole() error {
	svc := helper.getIAM()

	_, err := svc.AttachRolePolicy(context.Background(), &iam.AttachRolePolicyInput{
		PolicyArn: helper.Policy.Arn,
		RoleName:  helper.Role.RoleName,
	})
	dettachPolicy := func() error {
		_, err := svc.DetachRolePolicy(context.Background(), &iam.DetachRolePolicyInput{
			PolicyArn: helper.Policy.Arn,
			RoleName:  helper.Role.RoleName,
		})

		return err
	}

	helper.cleanups = append(helper.cleanups, dettachPolicy)

	return err
}

func (helper *AWSUtil) getIAM() *iam.Client {
	if helper.iam == nil {
		helper.iam = iam.NewFromConfig(helper.Cfg)
	}

	return helper.iam
}

func (helper *AWSUtil) createPolicy(s3Bucket string) error {
	svc := helper.getIAM()

	policy := policyDocument{
		Version: "2012-10-17",
		Statement: []statementEntry{
			{
				Effect: "Allow",
				Action: []string{
					"s3:*",
				},
				Resource: []string{
					fmt.Sprintf("arn:aws:s3:::%s/*", s3Bucket),
					fmt.Sprintf("arn:aws:s3:::%s", s3Bucket),
				},
			},
		},
	}

	b, err := json.Marshal(&policy)
	if err != nil {
		return err
	}

	result, err := svc.CreatePolicy(context.Background(), &iam.CreatePolicyInput{
		PolicyDocument: aws.String(string(b)),
		PolicyName:     aws.String("certification-test-policy-" + RandomString(6)),
	})
	if err != nil {
		return err
	}

	deletePolicy := func() error {
		_, err := svc.DeletePolicy(context.Background(), &iam.DeletePolicyInput{
			PolicyArn: result.Policy.Arn,
		})

		return err
	}

	helper.cleanups = append(helper.cleanups, deletePolicy)

	helper.Policy = result.Policy

	return nil
}

func (helper *AWSUtil) createRole(namespace string, accountid string, oidcProvider string) error {
	svc := helper.getIAM()

	role := roleDocument{
		Version: "2012-10-17",
		Statement: []roleStatementEntry{
			{
				Effect: "Allow",
				Principal: map[string]string{
					"Federated": fmt.Sprintf("arn:aws:iam::%s:oidc-provider/%s", accountid, oidcProvider),
				},
				Action: "sts:AssumeRoleWithWebIdentity",
				Condition: map[string]map[string]string{
					"StringEquals": {
						fmt.Sprintf("%s:sub", oidcProvider): fmt.Sprintf("system:serviceaccount:%s:%s", namespace, config.BackupResourceName),
					},
				},
			},
		},
	}

	b, err := json.Marshal(&role)
	if err != nil {
		return err
	}

	result, err := svc.CreateRole(context.Background(), &iam.CreateRoleInput{
		AssumeRolePolicyDocument: aws.String(string(b)),
		RoleName:                 aws.String("certification-test-role-" + RandomString(6)),
	})
	if err != nil {
		return err
	}

	deleteRole := func() error {
		_, err := svc.DeleteRole(context.Background(), &iam.DeleteRoleInput{
			RoleName: result.Role.RoleName,
		})

		return err
	}

	helper.cleanups = append(helper.cleanups, deleteRole)

	helper.Role = result.Role

	return err
}

func (helper *AWSUtil) Cleanup() {
	length := len(helper.cleanups)

	for i := length - 1; i >= 0; i-- {
		if err := helper.cleanups[i](); err != nil {
			fmt.Println(err)
		}
	}
}

func annotateServiceRoleWithIAM(t *testing.T, kubernetes *types.Cluster, arn string) {
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		serviceAccount, err := kubernetes.KubeClient.CoreV1().ServiceAccounts(kubernetes.Namespace).Get(context.Background(), config.BackupResourceName, v1.GetOptions{})
		if err != nil {
			Die(t, err)
		}

		serviceAccount.ObjectMeta.Annotations[config.BackupIAMAnnotation] = arn

		_, updateErr := kubernetes.KubeClient.CoreV1().ServiceAccounts(kubernetes.Namespace).Update(context.TODO(), serviceAccount, v1.UpdateOptions{})
		return updateErr
	})

	if retryErr != nil {
		Die(t, retryErr)
	}
}
