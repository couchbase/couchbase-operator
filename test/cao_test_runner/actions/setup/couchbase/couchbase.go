package couchbasesetup

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
	yamlutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/yaml"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/validations"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/sirupsen/logrus"
)

const (
	randomStringLen = 8
)

var (
	ErrCouchbaseConfigDecode = errors.New("unable to decode CouchbaseConfig")
	ErrMissingYaml           = errors.New("missing yaml spec for couchbase deployment")
	ErrConfigCouchbase       = errors.New("no config found for couchbase deployment")
	ErrWrongSpecPath         = errors.New("wrong spec path")
)

type CouchbaseConfig struct {
	Description             []string         `yaml:"description"`
	CBClusterSpecPath       string           `yaml:"cbClusterSpecPath" caoCli:"required"`
	CBBucketsSpecPath       string           `yaml:"cbBucketsSpecPath"`
	CBSecretsSpecPath       string           `yaml:"cbSecretsSpecPath"`
	ApplyClusterSpecChanges map[string]any   `yaml:"applyClusterSpecChanges"`
	ApplyBucketSpecChanges  map[string]any   `yaml:"applyBucketSpecChanges"`
	Validators              []map[string]any `yaml:"validators,omitempty"`
}

func NewCouchbaseConfig(config interface{}) (actions.Action, error) {
	if config != nil {
		c, ok := config.(*CouchbaseConfig)
		if !ok {
			return nil, ErrCouchbaseConfigDecode
		}

		if c.CBClusterSpecPath == "" {
			return nil, ErrMissingYaml
		}

		return &Couchbase{
			desc:       "setup or configure couchbase using cluster yaml specification",
			yamlConfig: c,
		}, nil
	}

	return nil, ErrConfigCouchbase
}

type Couchbase struct {
	desc       string
	yamlConfig interface{}
}

func (s *Couchbase) Checks(ctx *context.Context, _ interface{}, state string) error {
	c, _ := s.yamlConfig.(*CouchbaseConfig)

	if ok, err := validations.RunValidator(ctx, c.Validators, state); !ok {
		return fmt.Errorf("run %s validations: %w", state, err)
	}

	return nil
}

func (s *Couchbase) Describe() string {
	return s.desc
}

func (s *Couchbase) Do(ctx *context.Context, _ interface{}) error {
	c, _ := s.yamlConfig.(*CouchbaseConfig)

	var cbClusterSpecPath, modifiedClusterSpecPath, cbBucketSpecPath, modifiedBucketSpecPath string

	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("configure cb cluster: %w", err)
	}

	// Checking if the cluster and buckets spec path is saved in the context.
	if specPath := context.ValueID(ctx.Context(), context.ClusterSpecPathIDKey); specPath != "0" {
		cbClusterSpecPath = specPath
	} else {
		cbClusterSpecPath = path.Join(dir, c.CBClusterSpecPath)
	}

	if specPath := context.ValueID(ctx.Context(), context.BucketsSpecPathIDKey); specPath != "0" {
		cbBucketSpecPath = specPath
	} else {
		cbBucketSpecPath = path.Join(dir, c.CBBucketsSpecPath)
	}

	logrus.Infof("Configuring couchbase cluster")

	// Parse and apply the spec changes to the cb cluster yaml file.
	modifiedClusterSpecPath, err = applySpecChanges(c.ApplyClusterSpecChanges, cbClusterSpecPath)
	if err != nil {
		return fmt.Errorf("configure cb cluster: %w", err)
	}

	// Parse and apply the spec changes to the cb buckets yaml file.
	modifiedBucketSpecPath, err = applySpecChanges(c.ApplyBucketSpecChanges, cbBucketSpecPath)
	if err != nil {
		return fmt.Errorf("configure cb buckets: %w", err)
	}

	// Apply the couchbase secrets yaml.
	if c.CBSecretsSpecPath != "" {
		err = kubectl.ApplyFiles(path.Join(dir, c.CBSecretsSpecPath)).InNamespace("default").ExecWithoutOutputCapture()
		if err != nil {
			return fmt.Errorf("kubectl apply couchbase secrets yaml: %w", err)
		}
	}

	// Apply the couchbase cluster yaml with all the required spec changes.
	err = kubectl.ApplyFiles(modifiedClusterSpecPath).InNamespace("default").ExecWithoutOutputCapture()
	if err != nil {
		return fmt.Errorf("kubectl apply couchbase cluster yaml: %w", err)
	}

	// Apply the couchbase buckets yaml with all the required spec changes.
	if c.CBBucketsSpecPath != "" {
		err = kubectl.ApplyFiles(modifiedBucketSpecPath).InNamespace("default").ExecWithoutOutputCapture()
		if err != nil {
			return fmt.Errorf("kubectl apply couchbase buckets yaml: %w", err)
		}
	}

	// Add the new updated yaml path to the context.
	ctx.WithID(context.ClusterSpecPathIDKey, modifiedClusterSpecPath)
	ctx.WithID(context.BucketsSpecPathIDKey, modifiedBucketSpecPath)

	logrus.Infof("Couchbase cluster configured")

	return nil
}

func (s *Couchbase) Config() interface{} {
	return s.yamlConfig
}

// getModifiedSpecPath returns the modified spec path.
/*
 * Modified spec path is in the ./cao_test_runner/modified_test_data/... directory.
 * The spec file name is renamed as <old-file-name>-<random-8-chars>.extension.
 */
func getModifiedSpecPath(specPath string) (string, error) {
	fileDir, fileName := filepath.Split(specPath)
	isModified := false

	if !strings.Contains(fileDir, "modified_test_data") {
		if !strings.Contains(fileDir, "test_data") {
			return "", fmt.Errorf("get modified spec path: %w", ErrWrongSpecPath)
		}

		fileDir = strings.Replace(fileDir, "test_data", "modified_test_data", 1)
	} else {
		isModified = true
	}

	fileNameWithoutExt := fileName[0 : len(fileName)-len(filepath.Ext(fileName))]

	// Making sure all the directories exist
	err := os.MkdirAll(fileDir, os.ModePerm)
	if err != nil {
		return "", fmt.Errorf("get modified spec path: %w", err)
	}

	// Removing the `-<random-8-chars>` if the spec path is modified
	if isModified {
		// e.g. file-name-qwerty12 changes to file-name-
		// So, for each modification the file name changes as:  file-name, file-name-qwerty12, file-name--random34 ...
		fileNameWithoutExt = fileNameWithoutExt[0 : len(fileNameWithoutExt)-8]
	}

	// The spec file name is renamed as <old-file-name>-<random-8-chars>.extension.
	modifiedSpecPath := filepath.Join(fileDir,
		fmt.Sprintf("%s-%s%s", fileNameWithoutExt, e2eutil.RandomString(randomStringLen), filepath.Ext(fileName)))

	_, modifiedFileName := filepath.Split(modifiedSpecPath)
	logrus.Infof("modified file name: %s", modifiedFileName)

	return modifiedSpecPath, nil
}

// applySpecChanges applies all the specification changes to the YAML file (whose location is specPath).
func applySpecChanges(specChanges map[string]interface{}, specPath string) (string, error) {
	var modifiedSpecPath string

	if specChanges != nil {
		yaml, err := yamlutils.UnmarshalYAMLFile(specPath)
		if err != nil {
			return "", fmt.Errorf("apply spec changes: %w", err)
		}

		for spec, value := range specChanges {
			err = yamlutils.UpdateMapStringInterface(yaml, spec, value)
			if err != nil {
				return "", fmt.Errorf("apply spec changes: %w", err)
			}
		}

		modifiedSpecPath, err = getModifiedSpecPath(specPath)
		if err != nil {
			return "", fmt.Errorf("apply spec changes: %w", err)
		}

		err = yamlutils.MarshalYAMLIntoFile(modifiedSpecPath, yaml)
		if err != nil {
			return "", fmt.Errorf("apply spec changes: %w", err)
		}
	} else {
		return specPath, nil
	}

	return modifiedSpecPath, nil
}
