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
	cfg      aws.Config
	endpoint string
	iam      *iam.Client
	cleanups []func() error
	Policy   *iamtypes.Policy
	Role     *iamtypes.Role
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

	loadOpts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(o.region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(o.accessKey, o.secretID, token)),
	}

	if o.cert != nil {
		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(o.cert)

		t := &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: caCertPool,
			},
		}
		client := &http.Client{Transport: t, Timeout: 15 * time.Second}
		loadOpts = append(loadOpts, awsconfig.WithHTTPClient(client))
	}

	cfg, err := awsconfig.LoadDefaultConfig(context.Background(), loadOpts...)
	if err != nil {
		panic(err)
	}

	return &AWSUtil{cfg: cfg, endpoint: o.endpoint}
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

	ctx := context.Background()

	_, err := svc.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
		PolicyArn: helper.Policy.Arn,
		RoleName:  helper.Role.RoleName,
	})
	dettachPolicy := func() error {
		_, err := svc.DetachRolePolicy(ctx, &iam.DetachRolePolicyInput{
			PolicyArn: helper.Policy.Arn,
			RoleName:  helper.Role.RoleName,
		})

		return err
	}

	helper.cleanups = append(helper.cleanups, dettachPolicy)

	return err
}

// NewS3Client returns an S3 client. We use path style
// addressing for the custom endpoint case since MinIO
// needs it.
func (helper *AWSUtil) NewS3Client() *s3.Client {
	return s3.NewFromConfig(helper.cfg, func(o *s3.Options) {
		if helper.endpoint != "" {
			o.BaseEndpoint = aws.String(helper.endpoint)
			o.UsePathStyle = true
		}
	})
}

func (helper *AWSUtil) getIAM() *iam.Client {
	if helper.iam == nil {
		helper.iam = iam.NewFromConfig(helper.cfg, func(o *iam.Options) {
			if helper.endpoint != "" {
				o.BaseEndpoint = aws.String(helper.endpoint)
			}
		})
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

	ctx := context.Background()

	result, err := svc.CreatePolicy(ctx, &iam.CreatePolicyInput{
		PolicyDocument: aws.String(string(b)),
		PolicyName:     aws.String("certification-test-policy-" + RandomString(6)),
	})
	if err != nil {
		return err
	}

	deletePolicy := func() error {
		_, err := svc.DeletePolicy(ctx, &iam.DeletePolicyInput{
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

	ctx := context.Background()

	result, err := svc.CreateRole(ctx, &iam.CreateRoleInput{
		AssumeRolePolicyDocument: aws.String(string(b)),
		RoleName:                 aws.String("certification-test-role-" + RandomString(6)),
	})
	if err != nil {
		return err
	}

	deleteRole := func() error {
		_, err := svc.DeleteRole(ctx, &iam.DeleteRoleInput{
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
