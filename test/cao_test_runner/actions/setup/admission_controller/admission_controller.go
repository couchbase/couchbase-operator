package admissioncontrollersetup

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/cao"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
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
	ErrGhcrSecretExists              = errors.New("ghcr docker-registry secret already exists")
	ErrInvalidSecretParams           = errors.New("ghcr docker-registry secret params invalid")
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
	defaultSecretName = "ghcr-admission-secret"
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

type AdmissionControllerConfig struct {
	Description                 []string            `yaml:"description"`
	AdmissionControllerImage    string              `yaml:"admissionControllerImage" caoCli:"context" env:"ADMISSION_CONTROLLER_IMAGE"`
	CAOBinaryPath               string              `yaml:"caoBinaryPath" caoCli:"required,context" env:"CAO_BINARY_PATH"`
	CPULimit                    int                 `yaml:"cpuLimit"`
	CPURequest                  int                 `yaml:"cpuRequest"`
	ImagePullPolicy             ImagePullPolicyType `yaml:"imagePullPolicy"`
	ImagePullSecret             ImagePullSecret     `yaml:"imagePullSecret"`
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

func (action *SetupAdmissionController) CheckConfig() error {
	if action.yamlConfig == nil {
		return ErrNoAdmissionConfigFound
	}

	c, ok := action.yamlConfig.(*AdmissionControllerConfig)
	if !ok {
		return ErrUnableToDecodeAdmissionConfig
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

	return nil
}

func (action *SetupAdmissionController) RunValidators(ctx *context.Context, state string) error {
	if action.yamlConfig == nil {
		return ErrNoAdmissionConfigFound
	}

	c, ok := action.yamlConfig.(*AdmissionControllerConfig)
	if !ok {
		return ErrUnableToDecodeAdmissionConfig
	}

	if ok, err := validations.RunValidator(ctx, c.Validators, state); !ok {
		return fmt.Errorf("run %s validations: %w", state, err)
	}

	return nil
}

func (action *SetupAdmissionController) Do(ctx *context.Context) error {
	if action.yamlConfig == nil {
		return ErrNoAdmissionConfigFound
	}

	c, ok := action.yamlConfig.(*AdmissionControllerConfig)
	if !ok {
		return ErrUnableToDecodeAdmissionConfig
	}

	logrus.Infof("Admission Controller pod creation started")

	logrus.Info("cao create admission at :", time.Now().Format(time.RFC3339))

	if err := generateImagePullSecret(&c.ImagePullSecret, true); err != nil {
		return fmt.Errorf("failed to generate pull secrets: %w", err)
	}

	cao.WithBinaryPath(c.CAOBinaryPath)

	if err := cao.CreateAdmissionController(c.CPULimit, c.CPURequest, c.MemoryLimit,
		c.MemoryRequest, c.Replicas, c.AdmissionControllerImage, string(c.ImagePullPolicy),
		c.ImagePullSecret.Name, fmt.Sprintf("%d", c.AdmissionControllerLogLevel), string(c.Scope), c.ValidateSecrets,
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
