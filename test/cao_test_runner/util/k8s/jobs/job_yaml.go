package jobs

import (
	"fmt"
	"os"
	"path/filepath"

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

	err := os.MkdirAll(filepath.Dir(filePath), os.ModePerm)
	if err != nil {
		return fmt.Errorf("create jobs yaml: %w", err)
	}

	err = os.WriteFile(filePath, yamlFileBytes, os.ModePerm)
	if err != nil {
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

	err = os.MkdirAll(filepath.Dir(filePath), os.ModePerm)
	if err != nil {
		return fmt.Errorf("create jobs yaml: %w", err)
	}

	err = os.WriteFile(filePath, marshal, os.ModePerm)
	if err != nil {
		return fmt.Errorf("marshal job yamls: %w", err)
	}

	return nil
}
