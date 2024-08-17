package setupkubernetes

import (
	"errors"
	"fmt"

	installutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/install_utils"
)

var (
	ErrNotImplemented = errors.New("not implemented")
)

type CreateClusterUtil interface {
	CreateCluster() error
	ValidateParams() error
}

func NewCreateClusterUtil(p *KubernetesSetupConfig) (CreateClusterUtil, error) {
	switch p.Platform {
	case installutils.Kubernetes:
		switch p.Environment {
		case Kind:
			return &CreateKindCluster{
				ClusterName:              p.ClusterName,
				NumControlPlane:          p.NumControlPlane,
				NumWorkers:               p.NumWorkers,
				ConfigDirectory:          "./tmp", // TODO : Move to results directory
				OperatorImage:            p.OperatorImage,
				AdmissionControllerImage: p.AdmissionControllerImage,
			}, nil

		case Cloud:
			return nil, ErrNotImplemented

		default:
			return nil, fmt.Errorf("unknown environment type: %s", p.Environment)
		}

	case installutils.Openshift:
		return nil, ErrNotImplemented

	default:
		return nil, fmt.Errorf("unknown platform type: %s", p.Platform)
	}
}
