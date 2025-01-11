package oldvalidators

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
	jsonpatchutil "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/jsonpatch"
	"github.com/sirupsen/logrus"
)

const (
	defaultSizeCheckDuration = 25 * 60 * time.Second // 25 Minutes
	defaultSizeCheckInterval = 60 * time.Second
	statusConditionType      = "/items/0/status/conditions/%d/type"
	specServerNameYAMLPath   = "/items/0/spec/servers/%d/name"
	specServerSizeYAMLPath   = "/items/0/spec/servers/%d/size"
)

var (
	ErrDecodeError                    = errors.New("unable to decode")
	ErrScalingInProcess               = errors.New("scaling is in progress")
	ErrMapServerNameToSizeNotProvided = errors.New("MapServerNameToSize not provided")
)

type CouchbaseClusterSize struct {
	Name                string             `yaml:"name" caoCli:"required"`
	State               string             `yaml:"state" caoCli:"required"`
	MapServerNameToSize map[string]float64 `yaml:"mapServerNameToSize"`
	DurationInSecs      int64              `yaml:"durationInSecs"`
	IntervalInSecs      int64              `yaml:"intervalInSecs"`
	PreRunWaitInSecs    int64              `yaml:"preRunWaitInSecs"`
}

func (c *CouchbaseClusterSize) Run(_ *context.Context, testAssets assets.TestAssetGetterSetter) error {
	defer handlePanic()

	// Check if Cluster Scaling has started or not.
	// Check this a few seconds after applying patch, because the scaling does not start immediately.
	funcCheckClusterScaling := func() error {
		// Get information of the couchbasecluster in JSON
		stdout, err := kubectl.Get("couchbasecluster").FormatOutput("json").InNamespace("default").Output()
		if err != nil {
			return fmt.Errorf("get couchbasecluster information: %w", err)
		}

		// Unmarshal JSON into the map variable
		var jsonOutput map[string]interface{}

		err = json.Unmarshal([]byte(stdout), &jsonOutput)
		if err != nil {
			return fmt.Errorf("parse json: %w", err)
		}

		// Getting the indices of the status.conditions[i].status: Scaling, ScalingUp, ScalingDown
		i := 0
		check := 0

		for {
			scalingTypeName, err := jsonpatchutil.Get(&jsonOutput, fmt.Sprintf(statusConditionType, i))
			if err != nil {
				if errors.Is(err, jsonpatchutil.ErrPathNotFoundInJSON) {
					break
				}

				return err
			}

			i++

			if scalingTypeNameString, ok := scalingTypeName.(string); ok {
				switch scalingTypeNameString {
				case "Scaling":
					check++
				case "ScalingUp":
					check++
				case "ScalingDown":
					check++
				default:
					continue
				}
			}
		}

		// Whenever Scaling occurs, either two or all of [Scaling, ScalingUp, ScalingDown] condition types are present
		if check > 1 {
			// returning error in order to keep retrying this function
			return ErrScalingInProcess
		} else {
			// Either Scaling is not happening, or finished since check < 2.
			// Let's verify the server sizes provided
			j := 0

			for {
				serverName, err := jsonpatchutil.Get(&jsonOutput, fmt.Sprintf(specServerNameYAMLPath, j))
				if err != nil {
					if errors.Is(err, jsonpatchutil.ErrPathNotFoundInJSON) {
						break
					}

					return err
				}

				if serverNameString, ok := serverName.(string); ok {
					requiredServerSize := c.MapServerNameToSize[serverNameString]
					patchSet := jsonpatchutil.NewPatchSet().
						Test(fmt.Sprintf(specServerSizeYAMLPath, j), requiredServerSize)

					err = jsonpatchutil.Apply(&jsonOutput, patchSet.Patches())
					if err != nil {
						return fmt.Errorf("server size apply patch: %w", err)
					}

					j++
				} else {
					return fmt.Errorf("decode server name '%s': %w", specServerNameYAMLPath, ErrDecodeError)
				}
			}
		}

		return nil
	}

	// If the DurationInMinutes and IntervalInMinutes is provided then it will be used, else default values to be used
	checkDuration := defaultSizeCheckDuration
	checkInterval := defaultSizeCheckInterval

	if c.DurationInSecs != 0 {
		checkDuration = time.Duration(c.DurationInSecs) * time.Second
	}

	if c.IntervalInSecs != 0 {
		checkInterval = time.Duration(c.IntervalInSecs) * time.Second
	}

	if c.MapServerNameToSize == nil {
		return ErrMapServerNameToSizeNotProvided
	}

	// Wait before cluster size check is to be started.
	if c.PreRunWaitInSecs != 0 {
		logrus.Infof("Waiting for %d seconds before starting cluster size check", c.PreRunWaitInSecs)
		time.Sleep(time.Duration(c.PreRunWaitInSecs) * time.Second)
	}

	logrus.Info("Couchbase cluster size check started")

	err := util.RetryFunctionTillTimeout(funcCheckClusterScaling, checkDuration, checkInterval)
	if err != nil {
		return fmt.Errorf("retry function: %w", err)
	}

	logrus.Info("Couchbase cluster size check successful")

	return nil
}

func (c *CouchbaseClusterSize) GetState() string {
	return c.State
}
