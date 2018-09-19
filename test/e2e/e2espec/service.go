package e2espec

import (
	"github.com/couchbase/couchbase-operator/test/e2e/constants"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// Creates a NodePort service exposing port 8091
func NewNodePortService(namespace string) *v1.Service {
	ports := []v1.ServicePort{{
		Port:       constants.CbClusterRestPort,
		TargetPort: intstr.FromInt(int(constants.CbClusterRestPort)),
		Protocol:   v1.ProtocolTCP,
	}}
	return NewService(namespace, "test-nodesvc-", ports, v1.ServiceTypeNodePort)
}

// Templated service creation.  Additional customization can be done by callee
func NewService(namespace, genName string, ports []v1.ServicePort, serviceType v1.ServiceType) *v1.Service {
	return &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    namespace,
			GenerateName: genName,
		},
		Spec: v1.ServiceSpec{
			Type:     serviceType,
			Ports:    ports,
			Selector: constants.CbAppSelectorMap,
		},
	}
}
