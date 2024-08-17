package destroykubernetes

import (
	"errors"
	"fmt"

	installutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/install_utils"
)

var (
	ErrNotImplemented = errors.New("not implemented")
)

type DeleteClusterUtil interface {
	DeleteCluster() error
	ValidateParams() error
}

func NewDeleteClusterUtil(p *KubernetesDestroyConfig) (DeleteClusterUtil, error) {
	switch p.Platform {
	case installutils.Kubernetes:
		switch p.Environment {
		case Kind:
			return &DeleteKindCluster{
				ClusterName: p.ClusterName,
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
