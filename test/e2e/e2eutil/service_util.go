package e2eutil

import (
	"context"

	"github.com/couchbase/couchbase-operator/test/e2e/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func DeleteService(k8s *types.Cluster, serviceName string, options *metav1.DeleteOptions) error {
	return k8s.KubeClient.CoreV1().Services(k8s.Namespace).Delete(context.Background(), serviceName, *options)
}
