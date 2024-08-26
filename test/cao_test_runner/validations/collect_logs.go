package validations

import (
	"errors"
	"fmt"
	"os"
	"path"
	"time"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/shell"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/jsonpatch"
	yamlutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/yaml"
	"github.com/sirupsen/logrus"
)

type CollectLogs struct {
	Name          string `yaml:"name" caoCli:"required"`
	State         string `yaml:"state"`
	CBServerLogs  bool   `yaml:"cbServerLogs" caoCli:"required"`
	OperatorLogs  bool   `yaml:"operatorLogs" caoCli:"required"`
	LogSpecPath   string `yaml:"logSpecPath"`
	LogName       string `yaml:"logName" caoCli:"required"`
	CAOBinaryPath string `yaml:"caoBinaryPath"`
}

const (
	defaultLogCollectionSpecPath = "./test_data/curl_workloads/log-collection-pre-swap-rebalance.yaml"
)

var (
	ErrLogNameConstraint = errors.New("log name exceeds log name limit of 50 characters. 11 characters taken by date")
	ErrPathNotFound      = errors.New("path not found")
	curlSleepTime        = 5 * time.Second
)

func (cl *CollectLogs) Run(_ *context.Context) error {
	logrus.Info("log collection running")

	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("collect logs: %w", err)
	}

	var logJobSpecPath string
	if cl.LogSpecPath != "" {
		logJobSpecPath = path.Join(workDir, cl.LogSpecPath)
	} else {
		logJobSpecPath = path.Join(workDir, defaultLogCollectionSpecPath)
	}

	if cl.CBServerLogs {
		// Generate the name for the log. Append the date to the log name.
		logNameMaxChar := 39
		if len(cl.LogName) > logNameMaxChar {
			return fmt.Errorf("collect logs: %w", ErrLogNameConstraint)
		}

		customerName := fmt.Sprintf("%s_%s", time.Now().Format("2006-01-02"), cl.LogName)

		// Get the YAML for the logs job and unmarshal it
		unmarshalledYAML, err := yamlutils.UnmarshalYAMLFile(logJobSpecPath)
		if err != nil {
			return err
		}

		// Update the YAML with the correct log name
		yamlPatchSet := jsonpatch.NewPatchSet().
			Replace("/spec/template/spec/containers/0/args/12", "customer="+customerName)

		err = jsonpatch.Apply(&unmarshalledYAML, yamlPatchSet.Patches())
		if err != nil {
			return fmt.Errorf("collect logs: apply jsonpatch: %w", err)
		}

		// Save the yaml
		err = yamlutils.MarshalYAMLIntoFile(logJobSpecPath, unmarshalledYAML)
		if err != nil {
			return fmt.Errorf("collect logs: %w", err)
		}

		// Apply the YAML of the workload
		err = kubectl.ApplyFiles(logJobSpecPath).InNamespace("default").ExecWithoutOutputCapture()
		if err != nil {
			return fmt.Errorf("kubectl apply log job: %w", err)
		}

		// wait such that the curl is successfully sent
		time.Sleep(curlSleepTime)

		err = kubectl.DeleteFromFiles(logJobSpecPath).InNamespace("default").ExecWithoutOutputCapture()
		if err != nil {
			return fmt.Errorf("kubectl delete log job: %w", err)
		}
	}

	if cl.OperatorLogs {
		if cl.CAOBinaryPath == "" {
			return fmt.Errorf("cao collect logs: cao binary: %w", ErrPathNotFound)
		}

		var caoBinaryPath string

		if cl.CAOBinaryPath[0] == '/' {
			caoBinaryPath = "." + cl.CAOBinaryPath
		} else {
			caoBinaryPath = "./" + cl.CAOBinaryPath
		}

		logrus.Info("cao collect-logs at :", time.Now().Format(time.RFC3339))

		err := shell.RunWithoutOutputCapture(caoBinaryPath, "collect-logs", "--all")
		if err != nil {
			return fmt.Errorf("execute cao collect-logs: %w", err)
		}
	}

	return nil
}

func (cl *CollectLogs) GetState() string {
	return cl.State
}
