package cluster

import (
	"fmt"
	"sort"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// janitorAbstractionInterface is an abstraction of PVCs for the benefit of unit testing.
type janitorAbstractionInterface interface {
	// LogPVCList returns a list of all logging PVCs for the specified cluster.
	LogPVCList() ([]*corev1.PersistentVolumeClaim, error)
	// LogPVCUpdate updates the specified PVC.
	LogPVCUpdate(*corev1.PersistentVolumeClaim) error
	// LogPVCDelete deleted the specified PVC.
	LogPVCDelete(name string) error
	// PodExists returns whether a pod exists or not.
	PodExists(name string) (bool, error)
}

// janitorAbstractionInterfaceImpl implements the janitorAbstractionInterface interface.
type janitorAbstractionInterfaceImpl struct {
	// cluster is a pointer to the cluster containing spec and clients.
	cluster *Cluster
}

// LogPVCList returns a list of all logging PVCs for the specified cluster.
func (j *janitorAbstractionInterfaceImpl) LogPVCList() ([]*corev1.PersistentVolumeClaim, error) {
	// Fetch the list of PVCs.
	pvcs := j.cluster.k8s.PersistentVolumeClaims.List()

	logPvcs := []*corev1.PersistentVolumeClaim{}

	for _, pvc := range pvcs {
		// If it's not a log volume ignore it.
		if !k8sutil.IsLogPVC(pvc) {
			continue
		}

		logPvcs = append(logPvcs, pvc)
	}

	return logPvcs, nil
}

// LogPVCUpdate updates the specified PVC.
func (j *janitorAbstractionInterfaceImpl) LogPVCUpdate(pvc *corev1.PersistentVolumeClaim) error {
	if _, err := j.cluster.k8s.KubeClient.CoreV1().PersistentVolumeClaims(j.cluster.cluster.Namespace).Update(pvc); err != nil {
		return err
	}

	return nil
}

// LogPVCDelete deleted the specified PVC.
func (j *janitorAbstractionInterfaceImpl) LogPVCDelete(name string) error {
	if err := j.cluster.k8s.KubeClient.CoreV1().PersistentVolumeClaims(j.cluster.cluster.Namespace).Delete(name, &metav1.DeleteOptions{}); err != nil {
		return err
	}

	return nil
}

// PodExists returns whether a pod exists or not.
func (j *janitorAbstractionInterfaceImpl) PodExists(name string) (bool, error) {
	pod, found := j.cluster.k8s.Pods.Get(name)
	if !found {
		return false, nil
	}

	// Check this isn't cbopinfo doing a collection.
	if v, ok := pod.Labels["app"]; ok && v == "cbopinfo" {
		return false, nil
	}

	return true, nil
}

// janitor encapsulates a janitor process for the specified cluster.
type janitor struct {
	// cluster is a pointer to the cluster containing spec and clients.
	cluster *Cluster
	// abstraction is the interface used to hide away low-level operations
	// for the benfit of unit testing.
	abstraction janitorAbstractionInterface
}

// newJanitor creates a new janitor object.
func newJanitor(cluster *Cluster) *janitor {
	return &janitor{
		cluster: cluster,
		abstraction: &janitorAbstractionInterfaceImpl{
			cluster: cluster,
		},
	}
}

// getDetachedLogPVCs return log volumes not attached to a pod.
func (j *janitor) getDetachedLogPVCs() ([]*corev1.PersistentVolumeClaim, error) {
	pvcs, err := j.abstraction.LogPVCList()
	if err != nil {
		return nil, err
	}

	detachedPVCs := []*corev1.PersistentVolumeClaim{}

	for _, pvc := range pvcs {
		if _, ok := pvc.Annotations[constants.VolumeDetachedAnnotation]; !ok {
			continue
		}

		detachedPVCs = append(detachedPVCs, pvc)
	}

	return detachedPVCs, nil
}

// updateDetachedAnnotation looks at a PVC and determines whether it's pod exists.  If it does
// but it is annotated as detached (due to a race) remove the annotation.  If it doesn't and it
// isn't annotated, add the annotation recording the detached time as now.  Finally update the
// reosurce if we performed any modifications.
func (j *janitor) updateDetachedAnnotation(pvc *corev1.PersistentVolumeClaim) error {
	// Extract the pod name from the PVC.
	podName, ok := pvc.Labels[constants.LabelNode]
	if !ok {
		return fmt.Errorf("pvc '%s' missing label '%s': %w", pvc.Name, constants.LabelNode, errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	// Look up the pod and calculate whether the PVC is attached.
	attached, err := j.abstraction.PodExists(podName)
	if err != nil {
		return err
	}

	// Manage the detached time annotation.
	if attached {
		if _, ok := pvc.Annotations[constants.VolumeDetachedAnnotation]; !ok {
			return nil
		}

		log.Info("PVC marked as attached", "cluster", j.cluster.namespacedName(), "name", pvc.Name)
		delete(pvc.Annotations, constants.VolumeDetachedAnnotation)
	} else {
		if _, ok := pvc.Annotations[constants.VolumeDetachedAnnotation]; ok {
			return nil
		}

		if pvc.Annotations == nil {
			pvc.Annotations = map[string]string{}
		}

		log.Info("PVC marked as detached", "cluster", j.cluster.namespacedName(), "name", pvc.Name)
		pvc.Annotations[constants.VolumeDetachedAnnotation] = time.Now().Format(time.RFC3339)
	}

	// Update the resource, may fail due to conflicts but we'll fix it up in a minute...
	if err := j.abstraction.LogPVCUpdate(pvc); err != nil {
		return err
	}

	return nil
}

// updateDetachedAnnotations performs the updateDetachedAnnotation for each log volume defined
// in Kubernetes.
func (j *janitor) updateDetachedAnnotations() error {
	pvcs, err := j.abstraction.LogPVCList()
	if err != nil {
		return err
	}

	for _, pvc := range pvcs {
		if err := j.updateDetachedAnnotation(pvc); err != nil {
			return err
		}
	}

	return nil
}

// deleteTimedoutVolumes deletes volumes that have over stayed the current
// retention period.
func (j *janitor) deleteTimedOutVolumes() error {
	// An empty or zero value means do nothing
	if j.cluster.cluster.Spec.Logging.LogRetentionTime == "" {
		return nil
	}

	logRetentionTime, err := time.ParseDuration(j.cluster.cluster.Spec.Logging.LogRetentionTime)
	if err != nil {
		return fmt.Errorf("logRetentionTime improperly formatted: %w", err)
	}

	if logRetentionTime == 0 {
		return nil
	}

	pvcs, err := j.getDetachedLogPVCs()
	if err != nil {
		return err
	}

	for _, pvc := range pvcs {
		detachedTime, err := time.Parse(time.RFC3339, pvc.Annotations[constants.VolumeDetachedAnnotation])
		if err != nil {
			return err
		}

		if detachedTime.Add(logRetentionTime).Before(time.Now()) {
			log.Info("PVC retention time threshold exceeded, deleting", "cluster", j.cluster.namespacedName(), "name", pvc.Name)

			if err := j.abstraction.LogPVCDelete(pvc.Name); err != nil {
				return err
			}
		}
	}

	return nil
}

// deleteOverCapacityVolumes deletes volumes who are over the maximum persissible
// count.
func (j *janitor) deleteOverCapacityVolumes() error {
	// A "zero" value means do nothing.
	logRetentionCount := j.cluster.cluster.Spec.Logging.LogRetentionCount
	if logRetentionCount == 0 {
		return nil
	}

	pvcs, err := j.getDetachedLogPVCs()
	if err != nil {
		return err
	}

	// If we are below or equal to the threshold do nothing.
	logCount := len(pvcs)
	if logCount <= logRetentionCount {
		return nil
	}

	// Sort based on detached annotation, RFC3339 usefully is sortable as a string
	sorter := func(i, j int) bool {
		a := pvcs[i].Annotations[constants.VolumeDetachedAnnotation]
		b := pvcs[j].Annotations[constants.VolumeDetachedAnnotation]

		return a < b
	}

	sort.SliceStable(pvcs, sorter)

	// Finally kill off the overflow
	for _, pvc := range pvcs[0 : logCount-logRetentionCount] {
		log.Info("PVC count thershold exceeded, deleting", "cluster", j.cluster.namespacedName(), "name", pvc.Name)

		if err := j.abstraction.LogPVCDelete(pvc.Name); err != nil {
			return err
		}
	}

	return nil
}

// cleanPeriodic runs a cleanup periodically.
func (j *janitor) cleanPeriodic() error {
	// First pass check the detached annotations and update as required.
	if err := j.updateDetachedAnnotations(); err != nil {
		return err
	}

	// Second pass check for expired PVCs.
	if err := j.deleteTimedOutVolumes(); err != nil {
		return err
	}

	// Third pass check for too many PVCs.
	if err := j.deleteOverCapacityVolumes(); err != nil {
		return err
	}

	return nil
}

// run monitors the namespace for resources related to the named cluster.
func (j *janitor) run() {
	log.Info("Janitor starting", "cluster", j.cluster.namespacedName())

	// Sit in a loop forever performing periodic clean up operations every
	// minute.
	// Note we don't do any clean up actions on cluster deletion, the owner
	// references attached to the volumes will do this for us.
	for {
		select {
		case <-j.cluster.ctx.Done():
			return
		case <-time.After(time.Minute):
			if err := j.cleanPeriodic(); err != nil {
				log.Error(err, "Janitor error", "cluster", j.cluster.namespacedName())
			}
		}
	}
}
