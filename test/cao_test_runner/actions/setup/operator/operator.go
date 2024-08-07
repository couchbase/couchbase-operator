package operatorsetup

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
	ErrUnableToDecodeOperatorConfig = errors.New("unable to decode OperatorConfig in Do()")
	ErrNoOperatorConfigFound        = errors.New("no config found for creating operator pod")
	ErrIllegalImagePullPolicy       = errors.New("illegal image pull policy")
	ErrIllegalScope                 = errors.New("illegal scope")
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
	DefaultOperatorImage      string              = "docker.io/couchbase/operator:latest"
	DefaultCPULimit           int                 = 1
	DefaultCPURequest         int                 = 500
	DefaultImagePullPolicy    ImagePullPolicyType = IfNotPresent
	DefaultLogLevl            int                 = 2
	DefaultMemoryLimit        int                 = 400
	DefaultMemoryRequest      int                 = 200
	DefaultPodCreationTimeout string              = "10m0s"
	DefaultPodDeleteDelay     string              = "0s"
	DefaultPodReadinessDelay  string              = "10s"
	DefaultPodReadinessPeriod string              = "20s"
	DefaultScope              ScopeType           = Namespace
)

const (
	cpuLimitArgs           string = "--cpu-limit"
	cpuRequestArgs         string = "--cpu-request"
	imageArgs              string = "--image"
	imagePullPolicyArgs    string = "--image-pull-policy"
	imagePullSecretArgs    string = "--image-pull-secret"
	logLevelArgs           string = "--log-level"
	memoryLimitArgs        string = "--memory-limit"
	memoryRequestArgs      string = "--memory-request"
	podCreationTimeoutArgs string = "--pod-creation-timeout"
	podDeleteDelayArgs     string = "--pod-delete-delay"
	podReadinessDelayArgs  string = "--pod-readiness-delay"
	podReadinessPeriodArgs string = "--pod-readiness-period"
	scopeArgs              string = "--scope"
)

type OperatorConfig struct {
	OperatorImage      string              `yaml:"operatorImage"`
	CAOBinaryPath      string              `yaml:"caoBinaryPath" caoCli:"required"`
	CPULimit           int                 `yaml:"cpuLimit"`
	CPURequest         int                 `yaml:"cpuRequest"`
	ImagePullPolicy    ImagePullPolicyType `yaml:"imagePullPolicy"`
	ImagePullSecret    string              `yaml:"imagePullSecret,omitempty"`
	OperatorLogLevel   int                 `yaml:"operatorLogLevel"`
	MemoryLimit        int                 `yaml:"memoryLimit"`
	MemoryRequest      int                 `yaml:"memoryRequest"`
	PodCreationTimeout string              `yaml:"podCreationTimeout"`
	PodDeleteDelay     string              `yaml:"podDeleteDelay"`
	PodReadinessDelay  string              `yaml:"podReadinessDelay"`
	PodReadinessPeriod string              `yaml:"podReadinessPeriod"`
	Scope              ScopeType           `yaml:"scope"`
	Validators         []map[string]any    `yaml:"validators,omitempty"`
}

func NewSetupOperatorConfig(config interface{}) (actions.Action, error) {
	if config != nil {
		c, ok := config.(*OperatorConfig)
		if !ok {
			return nil, ErrUnableToDecodeOperatorConfig
		}

		if c.OperatorImage == "" {
			c.OperatorImage = DefaultOperatorImage
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

		if c.OperatorLogLevel == 0 {
			c.OperatorLogLevel = DefaultLogLevl
		}

		if c.MemoryLimit == 0 {
			c.MemoryLimit = DefaultMemoryLimit
		}

		if c.MemoryRequest == 0 {
			c.MemoryRequest = DefaultMemoryRequest
		}

		if c.PodCreationTimeout == "" {
			c.PodCreationTimeout = DefaultPodCreationTimeout
		}

		if c.PodDeleteDelay == "" {
			c.PodDeleteDelay = DefaultPodDeleteDelay
		}

		if c.PodReadinessDelay == "" {
			c.PodReadinessDelay = DefaultPodReadinessDelay
		}

		if c.PodReadinessPeriod == "" {
			c.PodReadinessPeriod = DefaultPodReadinessPeriod
		}

		return &SetupOperator{
			desc:       "Setup operator pod",
			yamlConfig: c,
		}, nil
	}

	return nil, ErrNoOperatorConfigFound
}

type SetupOperator struct {
	desc       string
	yamlConfig interface{}
}

func (action *SetupOperator) Describe() string {
	return action.desc
}

func (action *SetupOperator) Checks(ctx *context.Context, config interface{}, state string) error {
	c, ok := action.yamlConfig.(*OperatorConfig)
	if !ok {
		return ErrNoOperatorConfigFound
	}

	if ok, err := validations.RunValidator(ctx, c.Validators, state); !ok {
		return fmt.Errorf("run %s validations: %w", state, err)
	}

	return nil
}

func generatOperatorArguments(c *OperatorConfig) []string {
	commandArgs := []string{"create", "operator"}

	argMap := map[string]string{
		cpuLimitArgs:           fmt.Sprintf("%d", c.CPULimit),
		cpuRequestArgs:         fmt.Sprintf("%d", c.CPURequest),
		imageArgs:              c.OperatorImage,
		imagePullPolicyArgs:    string(c.ImagePullPolicy),
		logLevelArgs:           fmt.Sprintf("%d", c.OperatorLogLevel),
		memoryLimitArgs:        fmt.Sprintf("%d", c.MemoryLimit),
		memoryRequestArgs:      fmt.Sprintf("%d", c.MemoryRequest),
		podCreationTimeoutArgs: c.PodCreationTimeout,
		podDeleteDelayArgs:     c.PodDeleteDelay,
		podReadinessDelayArgs:  c.PodReadinessDelay,
		podReadinessPeriodArgs: c.PodReadinessPeriod,
		scopeArgs:              string(c.Scope),
	}

	if c.ImagePullSecret != "" {
		argMap[imagePullSecretArgs] = c.ImagePullSecret
	}

	for argument, value := range argMap {
		commandArgs = append(commandArgs, argument, value)
	}

	return commandArgs
}

func (action *SetupOperator) Do(ctx *context.Context, _ interface{}) error {
	c, ok := action.yamlConfig.(*OperatorConfig)
	if !ok {
		return ErrNoOperatorConfigFound
	}

	logrus.Infof("Operator pod creation started")

	commandArgs := generatOperatorArguments(c)

	logrus.Info("cao create operator at :", time.Now().Format(time.RFC3339))

	err := shell.RunWithoutOutputCapture(c.CAOBinaryPath, commandArgs...)
	if err != nil {
		return fmt.Errorf("failed to execute cao create operator: %w", err)
	}

	ctx.WithID(context.OperatorIDKey, c.OperatorImage)
	ctx.WithID(context.CAOBinaryPathKey, c.CAOBinaryPath)

	return nil
}

func (action *SetupOperator) Config() interface{} {
	return action.yamlConfig
}
