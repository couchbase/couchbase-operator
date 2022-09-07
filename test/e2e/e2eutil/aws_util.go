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

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/couchbase/couchbase-operator/pkg/config"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
)

type policyDocument struct {
	Version   string
	Statement []statementEntry
}
type statementEntry struct {
	Effect   string
	Action   []string
	Resource []string
}
type roleDocument struct {
	Version   string
	Statement []roleStatementEntry
}

type roleStatementEntry struct {
	Effect    string
	Principal map[string]string
	Action    string
	Condition map[string]map[string]string
}
type AWSUtil struct {
	Sess     *session.Session
	iam      *iam.IAM
	cleanups []func() error
	Policy   *iam.Policy
	Role     *iam.Role
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

	config := &aws.Config{
		Region:      aws.String(o.region),
		Credentials: credentials.NewStaticCredentials(o.accessKey, o.secretID, token),
	}

	if o.endpoint != "" {
		config.Endpoint = &o.endpoint
		config.S3ForcePathStyle = aws.Bool(true)
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
		config.HTTPClient = &client
	}

	helper := AWSUtil{}
	helper.Sess = session.Must(session.NewSession(config))

	return &helper
}

func (helper *AWSUtil) SetupBackupIAM(namespace, accountid, oidcProvider, s3Bucket string) error {
	if err := helper.createPolicy(s3Bucket); err != nil {
		return err
	}

	if err := helper.createRole(namespace, accountid, oidcProvider); err != nil {
		return err
	}

	if err := helper.attachPolicyToRole(); err != nil {
		return err
	}

	return nil
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

	_, err := svc.AttachRolePolicy(&iam.AttachRolePolicyInput{
		PolicyArn: helper.Policy.Arn,
		RoleName:  helper.Role.RoleName,
	})
	dettachPolicy := func() error {
		_, err := svc.DetachRolePolicy(&iam.DetachRolePolicyInput{
			PolicyArn: helper.Policy.Arn,
			RoleName:  helper.Role.RoleName,
		})

		return err
	}

	helper.cleanups = append(helper.cleanups, dettachPolicy)

	return err
}

func (helper *AWSUtil) getIAM() *iam.IAM {
	if helper.iam == nil {
		helper.iam = iam.New(helper.Sess)
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

	result, err := svc.CreatePolicy(&iam.CreatePolicyInput{
		PolicyDocument: aws.String(string(b)),
		PolicyName:     aws.String("certification-test-policy-" + RandomString(6)),
	})
	if err != nil {
		return err
	}

	deletePolicy := func() error {
		_, err := svc.DeletePolicy(&iam.DeletePolicyInput{
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

	result, err := svc.CreateRole(&iam.CreateRoleInput{
		AssumeRolePolicyDocument: aws.String(string(b)),
		RoleName:                 aws.String("certification-test-role-" + RandomString(6)),
	})
	if err != nil {
		return err
	}

	deleteRole := func() error {
		_, err := svc.DeleteRole(&iam.DeleteRoleInput{
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
