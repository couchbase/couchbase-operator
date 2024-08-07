package admissioncontrollersetup

import (
	"errors"
	"fmt"
	"time"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/shell"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/validations"
	"github.com/sirupsen/logrus"
)

var (
	ErrUnableToDecodeAdmissionConfig = errors.New("unable to decode AdmissionControllerConfig in Do()")
	ErrNoAdmissionConfigFound        = errors.New("no config found for creating admission pod")
	ErrIllegalImagePullPolicy        = errors.New("illegal image pull policy")
	ErrIllegalScope                  = errors.New("illegal scope")
)

type ImagePullPolicyType string
type ScopeType string

const (
	Always               ImagePullPolicyType = "Always"
	IfNotPresent         ImagePullPolicyType = "IfNotPresent"
	Never                ImagePullPolicyType = "Never"
	BlankImagePullPolicy ImagePullPolicyType = ""
	Namespace            ScopeType           = "namespace"
	Cluster              ScopeType           = "cluster"
	BlankScope           ScopeType           = ""
)

const (
	DefaultAdmissionImage  string              = "docker.io/couchbase/admission:latest"
	DefaultCPULimit        int                 = 1
	DefaultCPURequest      int                 = 500
	DefaultImagePullPolicy ImagePullPolicyType = IfNotPresent
	DefaultLogLevl         int                 = 2
	DefaultMemoryLimit     int                 = 400
	DefaultMemoryRequest   int                 = 200
	DefaultScope           ScopeType           = Cluster
	DefaultReplicas        int                 = 1
)

const (
	cpuLimitArgs               string = "--cpu-limit"
	cpuRequestArgs             string = "--cpu-request"
	imageArgs                  string = "--image"
	imagePullPolicyArgs        string = "--image-pull-policy"
	imagePullSecretArgs        string = "--image-pull-secret"
	logLevelArgs               string = "--log-level"
	memoryLimitArgs            string = "--memory-limit"
	memoryRequestArgs          string = "--memory-request"
	replicasArgs               string = "--replicas"
	scopeArgs                  string = "--scope"
	validateSecretsArgs        string = "--validate-secrets"
	validateStorageClassesArgs string = "--validate-storage-classes"
)

type AdmissionControllerConfig struct {
	AdmissionControllerImage    string              `yaml:"admissionControllerImage"`
	CAOBinaryPath               string              `yaml:"caoBinaryPath" caoCli:"required"`
	CPULimit                    int                 `yaml:"CPULimit"`
	CPURequest                  int                 `yaml:"CPURequest"`
	ImagePullPolicy             ImagePullPolicyType `yaml:"imagePullPolicy"`
	ImagePullSecret             string              `yaml:"imagePullSecret,omitempty"`
	AdmissionControllerLogLevel int                 `yaml:"admissionControllerLogLevel"`
	MemoryLimit                 int                 `yaml:"memoryLimit"`
	MemoryRequest               int                 `yaml:"memoryRequest"`
	Replicas                    int                 `yaml:"replicas"`
	Scope                       ScopeType           `yaml:"scope"`
	ValidateSecrets             bool                `yaml:"validateSecrets" caoCli:"required"`
	ValidateStorageClasses      bool                `yaml:"validateStorageClasses" caoCli:"required"`
	Validators                  []map[string]any    `yaml:"validators,omitempty"`
}

func NewSetupAdmissionControllerConfig(config interface{}) (actions.Action, error) {
	if config != nil {
		c, ok := config.(*AdmissionControllerConfig)
		if !ok {
			return nil, ErrUnableToDecodeAdmissionConfig
		}

		if c.AdmissionControllerImage == "" {
			c.AdmissionControllerImage = DefaultAdmissionImage
		}

		if c.CPULimit == 0 {
			c.CPULimit = DefaultCPULimit
		}

		if c.CPURequest == 0 {
			c.CPURequest = DefaultCPURequest
		}

		switch c.ImagePullPolicy {
		case BlankImagePullPolicy:
			c.ImagePullPolicy = DefaultImagePullPolicy
		case Always, IfNotPresent, Never:
			// No-op
		default:
			return nil, ErrIllegalImagePullPolicy
		}

		switch c.Scope {
		case BlankScope:
			c.Scope = DefaultScope
		case Namespace, Cluster:
			// No-op
		default:
			return nil, ErrIllegalScope
		}

		if c.AdmissionControllerLogLevel == 0 {
			c.AdmissionControllerLogLevel = DefaultLogLevl
		}

		if c.MemoryLimit == 0 {
			c.MemoryLimit = DefaultMemoryLimit
		}

		if c.MemoryRequest == 0 {
			c.MemoryRequest = DefaultMemoryRequest
		}

		if c.Replicas == 0 {
			c.Replicas = DefaultReplicas
		}

		return &SetupAdmissionController{
			desc:       "Setup admission controller pod",
			yamlConfig: c,
		}, nil
	}

	return nil, ErrNoAdmissionConfigFound
}

type SetupAdmissionController struct {
	desc       string
	yamlConfig interface{}
}

func (action *SetupAdmissionController) Describe() string {
	return action.desc
}

func (action *SetupAdmissionController) Checks(ctx *context.Context, config interface{}, state string) error {
	c, ok := action.yamlConfig.(*AdmissionControllerConfig)
	if !ok {
		return ErrNoAdmissionConfigFound
	}

	if ok, err := validations.RunValidator(ctx, c.Validators, state); !ok {
		return fmt.Errorf("run %s validations: %w", state, err)
	}

	return nil
}

func generateAdmissionArguments(c *AdmissionControllerConfig) []string {
	commandArgs := []string{"create", "admission"}

	argMap := map[string]string{
		cpuLimitArgs:        fmt.Sprintf("%d", c.CPULimit),
		cpuRequestArgs:      fmt.Sprintf("%d", c.CPURequest),
		imageArgs:           c.AdmissionControllerImage,
		imagePullPolicyArgs: string(c.ImagePullPolicy),
		logLevelArgs:        fmt.Sprintf("%d", c.AdmissionControllerLogLevel),
		memoryLimitArgs:     fmt.Sprintf("%d", c.MemoryLimit),
		memoryRequestArgs:   fmt.Sprintf("%d", c.MemoryRequest),
		scopeArgs:           string(c.Scope),
		replicasArgs:        fmt.Sprintf("%d", c.Replicas),
	}

	if c.ImagePullSecret != "" {
		argMap[imagePullSecretArgs] = c.ImagePullSecret
	}

	if c.ValidateSecrets {
		argMap[validateSecretsArgs] = ""
	}

	if c.ValidateStorageClasses {
		argMap[validateStorageClassesArgs] = ""
	}

	for argument, value := range argMap {
		commandArgs = append(commandArgs, argument, value)
	}

	return commandArgs
}

func (action *SetupAdmissionController) Do(ctx *context.Context, _ interface{}) error {
	c, ok := action.yamlConfig.(*AdmissionControllerConfig)
	if !ok {
		return ErrNoAdmissionConfigFound
	}

	logrus.Infof("Admission Controller pod creation started")

	commandArgs := generateAdmissionArguments(c)

	logrus.Info("cao create admission at :", time.Now().Format(time.RFC3339))

	err := shell.RunWithoutOutputCapture(c.CAOBinaryPath, commandArgs...)
	if err != nil {
		return fmt.Errorf("failed to execute cao create admission: %w", err)
	}

	ctx.WithID(context.AdmissionIDKey, c.AdmissionControllerImage)
	ctx.WithID(context.CAOBinaryPathKey, c.CAOBinaryPath)

	return nil
}

func (action *SetupAdmissionController) Config() interface{} {
	return action.yamlConfig
}
