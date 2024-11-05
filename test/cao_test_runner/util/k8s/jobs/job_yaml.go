package jobs

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
	fileutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/file_utils"
	"gopkg.in/yaml.v3"
)

var (
	ErrJobNotFound = errors.New("job not found")
)

// MarshalJobsIntoSingleYAML marshals multiple jobs into a single YAML file.
func MarshalJobsIntoSingleYAML(jobs []*Job, filePath string) error {
	var yamlFileBytes []byte

	for _, job := range jobs {
		jobYAML, err := yaml.Marshal(job)
		if err != nil {
			return fmt.Errorf("create jobs yaml: %w", err)
		}

		yamlFileBytes = append(yamlFileBytes, jobYAML...)
		yamlFileBytes = append(yamlFileBytes, "---\n"...)
	}

	if directory := fileutils.NewDirectory(filepath.Dir(filePath), os.ModePerm); !directory.IsDirectoryExists() {
		if err := directory.CreateDirectory(); err != nil {
			return fmt.Errorf("create jobs yaml: %w", err)
		}
	}

	if err := fileutils.NewFile(filePath).WriteFile(yamlFileBytes, os.ModePerm); err != nil {
		return fmt.Errorf("create jobs yaml: %w", err)
	}

	return nil
}

// MarshalJobYAML marshals a job into a YAML file.
func MarshalJobYAML(job *Job, filePath string) error {
	marshal, err := yaml.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal job yamls: %w", err)
	}

	if directory := fileutils.NewDirectory(filepath.Dir(filePath), os.ModePerm); !directory.IsDirectoryExists() {
		if err := directory.CreateDirectory(); err != nil {
			return fmt.Errorf("create jobs yaml: %w", err)
		}
	}

	if err = fileutils.NewFile(filePath).WriteFile(marshal, os.ModePerm); err != nil {
		return fmt.Errorf("marshal job yamls: %w", err)
	}

	return nil
}

// UnmarshalJob unmarshal a job information (into Job struct) obtained in JSON format using kubectl get.
func UnmarshalJob(jobName, namespace string) (*Job, error) {
	stdout, err := kubectl.Get("job", jobName).FormatOutput("json").InNamespace(namespace).Output()
	if err != nil {
		if strings.Contains(stdout, fmt.Sprintf("Error from server (NotFound): jobs.batch \"%s\" not found", jobName)) {
			return nil, ErrJobNotFound
		}

		return nil, fmt.Errorf("unmarshal job: %w", err)
	}

	var job Job

	err = json.Unmarshal([]byte(stdout), &job)
	if err != nil {
		return nil, fmt.Errorf("unmarshal job: %w", err)
	}

	return &job, nil
}
