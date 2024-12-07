package operatorsetup

import (
	"errors"
	"fmt"
	"time"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/cao"
	fileutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/file_utils"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/validations"
	"github.com/sirupsen/logrus"
)

var (
	ErrUnableToDecodeOperatorConfig = errors.New("unable to decode OperatorConfig in Do()")
	ErrNoOperatorConfigFound        = errors.New("no config found for creating operator pod")
	ErrIllegalImagePullPolicy       = errors.New("illegal image pull policy")
	ErrIllegalScope                 = errors.New("illegal scope")
	ErrCAOBinaryPathInvalid         = errors.New("cao binary path does not exist")
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

type OperatorConfig struct {
	Description        []string            `yaml:"description"`
	OperatorImage      string              `yaml:"operatorImage" caoCli:"context" env:"OPERATOR_IMAGE"`
	CAOBinaryPath      string              `yaml:"caoBinaryPath" caoCli:"required,context" env:"CAO_BINARY_PATH"`
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
		return ErrIllegalImagePullPolicy
	}

	switch c.Scope {
	case BlankScope:
		c.Scope = DefaultScope
	case Namespace, Cluster:
		// No-op
	default:
		return ErrIllegalScope
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

	if ok = fileutils.NewFile(c.CAOBinaryPath).IsFileExists(); !ok {
		return fmt.Errorf("cao binary path %s does not exist: %w", c.CAOBinaryPath, ErrCAOBinaryPathInvalid)
	}

	if ok, err := validations.RunValidator(ctx, c.Validators, state); !ok {
		return fmt.Errorf("run %s validations: %w", state, err)
	}

	return nil
}

func (action *SetupOperator) Do(ctx *context.Context, _ interface{}) error {
	c, ok := action.yamlConfig.(*OperatorConfig)
	if !ok {
		return ErrNoOperatorConfigFound
	}

	logrus.Infof("Operator pod creation started")

	logrus.Info("cao create operator at :", time.Now().Format(time.RFC3339))

	cao.WithBinaryPath(c.CAOBinaryPath)

	if err := cao.CreateOperator(c.CPULimit, c.CPURequest, c.MemoryLimit, c.MemoryRequest,
		c.OperatorImage, string(c.ImagePullPolicy), c.ImagePullSecret,
		fmt.Sprintf("%d", c.OperatorLogLevel), string(c.Scope), c.PodCreationTimeout,
		c.PodDeleteDelay, c.PodReadinessDelay, c.PodReadinessPeriod).ExecWithoutOutputCapture(); err != nil {
		return fmt.Errorf("failed to execute cao create operator: %w", err)
	}

	ctx.WithID(context.OperatorIDKey, c.OperatorImage)
	ctx.WithID(context.CAOBinaryPathKey, c.CAOBinaryPath)

	return nil
}

func (action *SetupOperator) Config() interface{} {
	return action.yamlConfig
}
