package assets

import "errors"

type PlatformType string
type EnvironmentType string
type ProviderType string
type ReleaseChannel string

var (
	ErrIllegalPlatform       = errors.New("illegal platform")
	ErrIllegalEnvironment    = errors.New("illegal environment")
	ErrIllegalConfiguration  = errors.New("illegal configuration")
	ErrIllegalProvider       = errors.New("illegal provider")
	ErrInvalidReleaseChannel = errors.New("invalid release channel")
)

const (
	UnspecifiedReleaseChannel ReleaseChannel = "UNSPECIFIED"
	RapidReleaseChannel       ReleaseChannel = "RAPID"
	RegularReleaseChannel     ReleaseChannel = "REGULAR"
	StableReleaseChannel      ReleaseChannel = "STABLE"
)

var (
	ReleaseChannelMap = map[ReleaseChannel]int{
		UnspecifiedReleaseChannel: 0,
		RapidReleaseChannel:       1,
		RegularReleaseChannel:     2,
		StableReleaseChannel:      3,
	}
)

const (
	Kubernetes PlatformType    = "kubernetes"
	Openshift  PlatformType    = "openshift"
	Kind       EnvironmentType = "kind"
	Cloud      EnvironmentType = "cloud"
	AWS        ProviderType    = "aws"
	Azure      ProviderType    = "azure"
	GCP        ProviderType    = "gcp"
)

// ManagedServiceProvider defines the supported Managed K8s Services.
type ManagedServiceProvider struct {
	platform    PlatformType    `yaml:"platform"`
	environment EnvironmentType `yaml:"environment"`
	provider    ProviderType    `yaml:"provider"`
}

func NewManagedServiceProvider(platform PlatformType,
	environment EnvironmentType, provider ProviderType) *ManagedServiceProvider {
	return &ManagedServiceProvider{
		platform:    platform,
		environment: environment,
		provider:    provider,
	}
}

func ValidateManagedServices(ms *ManagedServiceProvider) error {
	switch ms.platform {
	case Kubernetes, Openshift:
		// No-op
	default:
		return ErrIllegalPlatform
	}

	switch ms.environment {
	case Kind:
		// No-op
	case Cloud:
		switch ms.provider {
		case AWS, Azure, GCP:
			// No-op
		default:
			return ErrIllegalProvider
		}
	default:
		return ErrIllegalEnvironment
	}

	if ms.environment == Kind && ms.platform == Openshift {
		return ErrIllegalConfiguration
	}

	return nil
}

func ValidateReleaseChannel(rc ReleaseChannel) (bool, error) {
	switch rc {
	case UnspecifiedReleaseChannel, RapidReleaseChannel, RegularReleaseChannel, StableReleaseChannel:
		return true, nil
	default:
		return false, ErrInvalidReleaseChannel
	}
}

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-------------------ManagedServiceProvider Getters----------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

func (ms *ManagedServiceProvider) GetPlatform() PlatformType {
	return ms.platform
}

func (ms *ManagedServiceProvider) GetEnvironment() EnvironmentType {
	return ms.environment
}
func (ms *ManagedServiceProvider) GetProvider() ProviderType {
	return ms.provider
}
