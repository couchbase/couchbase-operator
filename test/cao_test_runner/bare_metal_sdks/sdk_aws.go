package baremetalsdks

import (
	"context"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// AWSSessionStore implements BareMetalInterface.
/*
 * AWSSessionStore stores multiple AWSSession for each different cluster.
 * To get AWSSession, use `region` as the key for AWSSessions map. E.g.: us-east-2.
 */
type AWSSessionStore struct {
	AWSSessions map[string]*AWSSession
	lock        sync.Mutex
}

// AWSSession holds all the required aws clients and sessions configured for one region.
type AWSSession struct {
	ec2Client *ec2.Client
	cred      *CloudProviderCredentials
	region    string
}

// NewAWSSession initializes a new AWSSession with the provided AWS credentials and region.
func NewAWSSession(ctx context.Context, providerCred *CloudProviderCredentials) (*AWSSession, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(providerCred.AWSCredentials.awsRegion),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(providerCred.AWSCredentials.awsAccessKey, providerCred.AWSCredentials.awsSecretKey, "")))
	if err != nil {
		return nil, fmt.Errorf("create new AWSSession: %w", err)
	}

	return &AWSSession{
		ec2Client: ec2.NewFromConfig(cfg),
		cred:      providerCred,
		region:    providerCred.AWSCredentials.awsRegion,
	}, nil
}

// ConfigAWSSessionStore initialises AWSSessionStore.
func ConfigAWSSessionStore() BareMetalInterface {
	return &AWSSessionStore{
		AWSSessions: make(map[string]*AWSSession),
		lock:        sync.Mutex{},
	}
}

func NewAWSSessionStore() *AWSSessionStore {
	return &AWSSessionStore{
		AWSSessions: make(map[string]*AWSSession),
		lock:        sync.Mutex{},
	}
}

// getAWSKey returns the key for the map AWSSessionStore.AWSSessions to retrieve the appropriate AWSSession for the cluster.
// Key is built using Region. E.g. us-east-2.
func getAWSKey(providerCred *CloudProviderCredentials) string {
	key := providerCred.AWSCredentials.awsRegion
	return key
}

// SetSession adds an AWSSession to the AWSSessionStore for the provided region and credentials.
func (awsSessStore *AWSSessionStore) SetSession(ctx context.Context, providerCred *CloudProviderCredentials) error {
	defer awsSessStore.lock.Unlock()
	awsSessStore.lock.Lock()

	if _, ok := awsSessStore.AWSSessions[getAWSKey(providerCred)]; !ok {
		awsSess, err := NewAWSSession(ctx, providerCred)
		if err != nil {
			return fmt.Errorf("set aws session store: %w", err)
		}

		awsSessStore.AWSSessions[getAWSKey(providerCred)] = awsSess
	}

	return nil
}

// GetSession returns the AWSSession.
// If AWSSession for the region is not present in AWSSessionStore then it sets it.
func (awsSessStore *AWSSessionStore) GetSession(ctx context.Context, providerCred *CloudProviderCredentials) (*AWSSession, error) {
	if _, ok := awsSessStore.AWSSessions[getAWSKey(providerCred)]; !ok {
		err := awsSessStore.SetSession(ctx, providerCred)
		if err != nil {
			return nil, fmt.Errorf("get aws session: %w", err)
		}
	}

	return awsSessStore.AWSSessions[getAWSKey(providerCred)], nil
}

// Check checks if AWS is accessible or not.
func (awsSessStore *AWSSessionStore) Check(ctx context.Context, providerCred *CloudProviderCredentials) error {
	awsSession, err := awsSessStore.GetSession(ctx, providerCred)
	if err != nil {
		return fmt.Errorf("check aws accessibility: %w", err)
	}

	_, err = awsSession.ec2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{})
	if err != nil {
		return fmt.Errorf("check aws accessibility: %w", err)
	}

	return nil
}

// ================================================
// ====== Methods implemented by AWSSession ======
// ================================================

// DescribeInstances EC2 Instance IDs by searching EC2 instances using filters.
func (as *AWSSession) DescribeInstances(ctx context.Context, awsValueName string, awsValues []string) ([]string, error) {
	input := &ec2.DescribeInstancesInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String(awsValueName),
				Values: awsValues,
			},
		},
	}

	describeInstancesOutput, err := as.ec2Client.DescribeInstances(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("describe instances: %w", err)
	}

	var instanceIds []string

	for _, reservation := range describeInstancesOutput.Reservations {
		for _, instance := range reservation.Instances {
			instanceIds = append(instanceIds, *instance.InstanceId)
		}
	}

	return instanceIds, nil
}

func (as *AWSSession) RebootInstances(ctx context.Context, instanceIds []string) error {
	if _, err := as.ec2Client.RebootInstances(
		ctx,
		&ec2.RebootInstancesInput{
			InstanceIds: instanceIds,
		}); err != nil {
		return fmt.Errorf("reboot ec2 instance: %w", err)
	}

	return nil
}

func (as *AWSSession) TerminateInstances(ctx context.Context, instanceIds []string) error {
	if _, err := as.ec2Client.TerminateInstances(
		ctx,
		&ec2.TerminateInstancesInput{
			InstanceIds: instanceIds,
		}); err != nil {
		return fmt.Errorf("terminate ec2 instance: %w", err)
	}

	return nil
}
