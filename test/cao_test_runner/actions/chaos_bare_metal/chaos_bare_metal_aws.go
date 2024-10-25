package chaosbaremetal

import (
	"fmt"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	baremetal "github.com/couchbase/couchbase-operator/test/cao_test_runner/bare_metal_sdks"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/triggers"
	"github.com/sirupsen/logrus"
)

// AWSChaos implements the ChaosActionsInterface for executing chaos actions on AWS EC2.
type AWSChaos struct {
	AWSCred *baremetal.CloudProviderCredentials
	AWSSess *baremetal.AWSSession
}

// NewAWSChaos returns an instance of AWSChaos to perform chaos actions on AWS Bare Metal cluster.
func NewAWSChaos(ctxt *context.Context, bareMetalRegion string) (*AWSChaos, error) {
	awsCred, err := baremetal.NewCloudProviderCredentials([]baremetal.CloudProvider{baremetal.AWS})
	if err != nil {
		return nil, fmt.Errorf("new aws chaos: %w", err)
	}

	// Changing the region of aws credentials to the EC2 Bare Metal Cluster location.
	awsCred.AWSCredentials.SetRegion(bareMetalRegion)

	awsSessionStore := baremetal.NewAWSSessionStore()

	awsSess, err := awsSessionStore.GetSession(ctxt.Context(), awsCred)
	if err != nil {
		return nil, fmt.Errorf("new aws chaos: %w", err)
	}

	return &AWSChaos{
		AWSCred: awsCred,
		AWSSess: awsSess,
	}, nil
}

func (awsChaos AWSChaos) RestartNodes(context *context.Context, chaosConfig *CBNodeChaosConfig) error {
	// Get the ec2 instances based on dns name.
	instanceIds, err := awsChaos.AWSSess.DescribeInstancesWithFilter(context.Context(), "dns-name", []string{chaosConfig.CBHostname})
	if err != nil {
		return fmt.Errorf("reboot ec2 instance: %w", err)
	}

	logrus.Infof("Starting to restart CB node `%s` with ec2 instance id: %s", chaosConfig.CBHostname, instanceIds[0])

	// Checking the triggers.
	populateCBInfoUsingChaosConfig(&chaosConfig.TriggerConfig, chaosConfig)

	err = triggers.ApplyTrigger(&chaosConfig.TriggerConfig)
	if err != nil {
		return fmt.Errorf("reboot ec2 instance: %w", err)
	}

	// Reboot the ec2 instance.
	if err = awsChaos.AWSSess.RebootInstances(context.Context(), instanceIds); err != nil {
		return fmt.Errorf("reboot ec2 instance: %w", err)
	}

	logrus.Infof("Successfully restarted CB node `%s` with ec2 instance id: %s", chaosConfig.CBHostname, instanceIds[0])

	return nil
}

func (awsChaos AWSChaos) CBServiceChaos(context *context.Context, chaosConfig *CBNodeChaosConfig) error {
	// TODO implement me
	panic("implement me")
}
