package chaos

import (
	"errors"
	"fmt"
	"time"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	cbrestapi "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cb_rest_api_utils/cb_rest_api"
)

var (
	ErrCBClusterChaosInvalid = errors.New("cb cluster chaos invalid")
)

// CBClusterChaosAction represents the CB Cluster chaos action.
type CBClusterChaosAction string

const (
	StopRebalance CBClusterChaosAction = "stopRebalance"
)

type CBClusterChaosConfig struct {
	ClusterChaosAction CBClusterChaosAction `yaml:"clusterChaosAction" caoCli:"required"`
}

type CBClusterChaosInterface interface {
	StopRebalance(ctx *context.Context) error
}

func ExecuteCBClusterChaos(ctx *context.Context, chaosConfig *CBPodChaosConfig) error {
	switch chaosConfig.CBClusterChaosConfig.ClusterChaosAction {
	case StopRebalance:
		{
			return chaosConfig.CBClusterChaosConfig.StopRebalance(ctx)
		}
	default:
		return fmt.Errorf("execute cb cluster chaos: %w", ErrCBClusterChaosInvalid)
	}
}

func (c *CBClusterChaosConfig) StopRebalance(ctx *context.Context) error {
	cbAPIClient, err := cbrestapi.NewClusterNodesAPI("localhost", 8091, "", "", "cb-example-auth", "default", 5*time.Second, false)
	if err != nil {
		return fmt.Errorf("chaos stop rebalance: %w", err)
	}

	err = cbAPIClient.StopRebalance()
	if err != nil {
		return fmt.Errorf("chaos stop rebalance: %w", err)
	}

	return nil
}
