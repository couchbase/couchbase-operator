package operatorsetup

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/cao"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
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
	ErrGhcrSecretExists             = errors.New("ghcr docker-registry secret already exists")
	ErrInvalidSecretParams          = errors.New("ghcr docker-registry secret params invalid")
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
	defaultSecretName = "ghcr-operator-secret"
	secretServerEnv   = "IMAGE_REGISTRY_SERVER"
	secretUsernameEnv = "IMAGE_REGISTRY_USERNAME"
	secretPasswordEnv = "IMAGE_REGISTRY_PASSWORD"
	secretEmailEnv    = "IMAGE_REGISTRY_EMAIL"
)

type ImagePullSecret struct {
	Name     string `yaml:"name"`
	Server   string `yaml:"server" env:"IMAGE_REGISTRY_SERVER"`
	Username string `yaml:"username" env:"IMAGE_REGISTRY_USERNAME"`
	Password string `yaml:"password" env:"IMAGE_REGISTRY_PASSWORD"`
	Email    string `yaml:"email" env:"IMAGE_REGISTRY_EMAIL"`
}

type OperatorConfig struct {
	Description        []string            `yaml:"description"`
	OperatorImage      string              `yaml:"operatorImage" caoCli:"context" env:"OPERATOR_IMAGE"`
	CAOBinaryPath      string              `yaml:"caoBinaryPath" caoCli:"required,context" env:"CAO_BINARY_PATH"`
	CPULimit           int                 `yaml:"cpuLimit"`
	CPURequest         int                 `yaml:"cpuRequest"`
	ImagePullPolicy    ImagePullPolicyType `yaml:"imagePullPolicy"`
	ImagePullSecret    ImagePullSecret     `yaml:"imagePullSecret"`
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

func (action *SetupOperator) RunValidators(ctx *context.Context,
	state string, testAssets assets.TestAssetGetterSetter) error {
	if action.yamlConfig == nil {
		return ErrNoOperatorConfigFound
	}

	c, ok := action.yamlConfig.(*OperatorConfig)
	if !ok {
		return ErrUnableToDecodeOperatorConfig
	}

	if ok, err := validations.RunValidator(ctx, c.Validators, state, testAssets); !ok {
		return fmt.Errorf("run %s validations: %w", state, err)
	}

	return nil
}

func (action *SetupOperator) CheckConfig() error {
	if action.yamlConfig == nil {
		return ErrNoOperatorConfigFound
	}

	c, ok := action.yamlConfig.(*OperatorConfig)
	if !ok {
		return ErrUnableToDecodeOperatorConfig
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

	return nil
}

func (action *SetupOperator) Do(ctx *context.Context, testAssets assets.TestAssetGetter) error {
	if action.yamlConfig == nil {
		return ErrNoOperatorConfigFound
	}

	c, ok := action.yamlConfig.(*OperatorConfig)
	if !ok {
		return ErrUnableToDecodeOperatorConfig
	}

	logrus.Infof("Operator pod creation started")

	logrus.Info("cao create operator at :", time.Now().Format(time.RFC3339))

	if err := generateImagePullSecret(&c.ImagePullSecret, true); err != nil {
		return fmt.Errorf("failed to generate pull secrets: %w", err)
	}

	cao.WithBinaryPath(c.CAOBinaryPath)

	if err := cao.CreateOperator(c.CPULimit, c.CPURequest, c.MemoryLimit, c.MemoryRequest,
		c.OperatorImage, string(c.ImagePullPolicy), c.ImagePullSecret.Name,
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

func generateImagePullSecret(imagePullSecret *ImagePullSecret, ignoreAlreadyExists bool) error {

	getSecretCredentials(imagePullSecret)

	if imagePullSecret.Email == "" && imagePullSecret.Name == "" &&
		imagePullSecret.Password == "" && imagePullSecret.Server == "" &&
		imagePullSecret.Username == "" {
		// No secret needs to be generated here
		return nil
	}

	out, _, err := kubectl.GetSecretNames().ExecWithOutputCapture()
	if err != nil {
		return fmt.Errorf("cannot fetch secrets: %w", err)
	}

	allSecrets := strings.Split(out, "\n")

	if imagePullSecret.Name == "" {
		imagePullSecret.Name = defaultSecretName
	}

	if slices.Contains(allSecrets, "secret/"+imagePullSecret.Name) {
		if ignoreAlreadyExists {
			logrus.Warnf("ghcr secret %s already exists", imagePullSecret.Name)
			return nil
		} else {
			return fmt.Errorf("ghcr secret %s already exists: %w", imagePullSecret.Name, ErrGhcrSecretExists)
		}
	}

	if imagePullSecret.Email == "" || imagePullSecret.Name == "" ||
		imagePullSecret.Password == "" || imagePullSecret.Server == "" ||
		imagePullSecret.Username == "" {
		// secret has invalid params
		return fmt.Errorf("the values for creating the ghcr secret is invalid: %w", ErrInvalidSecretParams)
	}

	if err := kubectl.CreateSecretDockerRegistry(imagePullSecret.Name, imagePullSecret.Server,
		imagePullSecret.Username, imagePullSecret.Password, imagePullSecret.Email).ExecWithoutOutputCapture(); err != nil {
		return fmt.Errorf("unable to create secret docker-registry %s: %w", imagePullSecret.Name, err)
	}

	return nil
}

func getSecretCredentials(imagePullSecret *ImagePullSecret) {
	if imagePullSecret.Server == "" {
		if envValue, ok := os.LookupEnv(secretServerEnv); ok {
			imagePullSecret.Server = envValue
		}
	}

	if imagePullSecret.Email == "" {
		if envValue, ok := os.LookupEnv(secretEmailEnv); ok {
			imagePullSecret.Email = envValue
		}
	}

	if imagePullSecret.Password == "" {
		if envValue, ok := os.LookupEnv(secretPasswordEnv); ok {
			imagePullSecret.Password = envValue
		}
	}

	if imagePullSecret.Username == "" {
		if envValue, ok := os.LookupEnv(secretUsernameEnv); ok {
			imagePullSecret.Username = envValue
		}
	}
}
