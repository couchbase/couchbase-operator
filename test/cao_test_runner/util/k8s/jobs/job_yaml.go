package jobs

import (
	"fmt"
	"os"
	"path/filepath"

	fileutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/file_utils"
	"gopkg.in/yaml.v3"
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

	if err := fileutils.NewDirectory(filepath.Dir(filePath), os.ModePerm).CreateDirectory(); err != nil {
		return fmt.Errorf("create jobs yaml: %w", err)
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

	if err := fileutils.NewDirectory(filepath.Dir(filePath), os.ModePerm).CreateDirectory(); err != nil {
		return fmt.Errorf("create jobs yaml: %w", err)
	}

	if err = fileutils.NewFile(filePath).WriteFile(marshal, os.ModePerm); err != nil {
		return fmt.Errorf("marshal job yamls: %w", err)
	}

	return nil
}
