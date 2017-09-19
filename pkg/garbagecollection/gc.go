package garbagecollection

import (
	"github.com/couchbaselabs/couchbase-operator/pkg/util/k8sutil"
	"github.com/sirupsen/logrus"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

const (
	NullUID = ""
)

var pkgLogger = logrus.WithField("pkg", "gc")

type GC struct {
	logger *logrus.Entry

	kubecli kubernetes.Interface
	ns      string
}

func New(kubecli kubernetes.Interface, ns string) *GC {
	return &GC{
		logger:  pkgLogger,
		kubecli: kubecli,
		ns:      ns,
	}
}

// CollectCluster collects resources that matches cluster label, but
// does not belong to the cluster with given clusterUID
func (gc *GC) CollectCluster(cluster string, clusterUID types.UID) {
	gc.collectResources(k8sutil.ClusterListOpt(cluster), map[types.UID]bool{clusterUID: true})
}

func (gc *GC) collectResources(option metav1.ListOptions, runningSet map[types.UID]bool) {
	if err := gc.collectPods(option, runningSet); err != nil {
		gc.logger.Errorf("gc pods failed: %v", err)
	}
	if err := gc.collectServices(option, runningSet); err != nil {
		gc.logger.Errorf("gc services failed: %v", err)
	}
	if err := gc.collectDeployment(option, runningSet); err != nil {
		gc.logger.Errorf("gc deployments failed: %v", err)
	}
}

func (gc *GC) collectPods(option metav1.ListOptions, runningSet map[types.UID]bool) error {
	pods, err := gc.kubecli.CoreV1().Pods(gc.ns).List(option)
	if err != nil {
		return err
	}

	for _, p := range pods.Items {
		if len(p.OwnerReferences) == 0 {
			gc.logger.Warningf("failed to check pod %s: no owner", p.GetName())
			continue
		}
		// Pods failed due to liveness probe are also collected
		if !runningSet[p.OwnerReferences[0].UID] || p.Status.Phase == v1.PodFailed {
			// kill bad pods without grace period to kill it immediately
			err = gc.kubecli.CoreV1().Pods(gc.ns).Delete(p.GetName(), metav1.NewDeleteOptions(0))
			if err != nil && !k8sutil.IsKubernetesResourceNotFoundError(err) {
				return err
			}
			gc.logger.Infof("deleted pod (%v)", p.GetName())
		}
	}
	return nil
}

func (gc *GC) collectServices(option metav1.ListOptions, runningSet map[types.UID]bool) error {
	srvs, err := gc.kubecli.CoreV1().Services(gc.ns).List(option)
	if err != nil {
		return err
	}

	for _, srv := range srvs.Items {
		if len(srv.OwnerReferences) == 0 {
			gc.logger.Warningf("failed to check service %s: no owner", srv.GetName())
			continue
		}
		if !runningSet[srv.OwnerReferences[0].UID] {
			err = gc.kubecli.CoreV1().Services(gc.ns).Delete(srv.GetName(), nil)
			if err != nil && !k8sutil.IsKubernetesResourceNotFoundError(err) {
				return err
			}
			gc.logger.Infof("deleted service (%v)", srv.GetName())
		}
	}

	return nil
}

func (gc *GC) collectDeployment(option metav1.ListOptions, runningSet map[types.UID]bool) error {
	ds, err := gc.kubecli.AppsV1beta1().Deployments(gc.ns).List(option)
	if err != nil {
		return err
	}

	for _, d := range ds.Items {
		if len(d.OwnerReferences) == 0 {
			gc.logger.Warningf("failed to GC deployment (%s): no owner", d.GetName())
			continue
		}
		if !runningSet[d.OwnerReferences[0].UID] {
			err = gc.kubecli.AppsV1beta1().Deployments(gc.ns).Delete(d.GetName(), k8sutil.CascadeDeleteOptions(0))
			if err != nil {
				if !k8sutil.IsKubernetesResourceNotFoundError(err) {
					return err
				}
			}
			gc.logger.Infof("deleted deployment (%s)", d.GetName())
		}
	}

	return nil
}
