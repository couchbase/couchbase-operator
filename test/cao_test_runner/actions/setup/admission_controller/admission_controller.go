package admissioncontrollersetup

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
	ErrUnableToDecodeAdmissionConfig = errors.New("unable to decode AdmissionControllerConfig in Do()")
	ErrNoAdmissionConfigFound        = errors.New("no config found for creating admission pod")
	ErrIllegalImagePullPolicy        = errors.New("illegal image pull policy")
	ErrIllegalScope                  = errors.New("illegal scope")
	ErrCAOBinaryPathInvalid          = errors.New("cao binary path does not exist")
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

type AdmissionControllerConfig struct {
	Description                 []string            `yaml:"description"`
	AdmissionControllerImage    string              `yaml:"admissionControllerImage" caoCli:"context" env:"ADMISSION_CONTROLLER_IMAGE"`
	CAOBinaryPath               string              `yaml:"caoBinaryPath" caoCli:"required,context" env:"CAO_BINARY_PATH"`
	CPULimit                    int                 `yaml:"cpuLimit"`
	CPURequest                  int                 `yaml:"cpuRequest"`
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

	if ok = fileutils.NewFile(c.CAOBinaryPath).IsFileExists(); !ok {
		return fmt.Errorf("cao binary path %s does not exist: %w", c.CAOBinaryPath, ErrCAOBinaryPathInvalid)
	}

	if ok, err := validations.RunValidator(ctx, c.Validators, state); !ok {
		return fmt.Errorf("run %s validations: %w", state, err)
	}

	return nil
}

func (action *SetupAdmissionController) Do(ctx *context.Context, _ interface{}) error {
	c, ok := action.yamlConfig.(*AdmissionControllerConfig)
	if !ok {
		return ErrNoAdmissionConfigFound
	}

	logrus.Infof("Admission Controller pod creation started")

	logrus.Info("cao create admission at :", time.Now().Format(time.RFC3339))

	cao.WithBinaryPath(c.CAOBinaryPath)

	if err := cao.CreateAdmissionController(c.CPULimit, c.CPURequest, c.MemoryLimit,
		c.MemoryRequest, c.Replicas, c.AdmissionControllerImage, string(c.ImagePullPolicy),
		c.ImagePullSecret, fmt.Sprintf("%d", c.AdmissionControllerLogLevel), string(c.Scope), c.ValidateSecrets,
		c.ValidateStorageClasses).ExecWithoutOutputCapture(); err != nil {
		return fmt.Errorf("failed to execute cao create admission: %w", err)
	}

	ctx.WithID(context.AdmissionIDKey, c.AdmissionControllerImage)
	ctx.WithID(context.CAOBinaryPathKey, c.CAOBinaryPath)

	return nil
}

func (action *SetupAdmissionController) Config() interface{} {
	return action.yamlConfig
}
