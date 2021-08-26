package cluster

import (
	"context"
	"fmt"
	"sort"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Reconcile Autoscaler ensures all server configs have
// associated Autoscaler CR. Applies changes when sizing
// differs.
func (c *Cluster) reconcileAutoscalers() error {
	// Ensure all expected Autoscaler resources are created
	autoscalers, err := c.updateRequestedAutoscalers()
	if err != nil {
		return err
	}

	if len(autoscalers) == 0 || !c.autoscalingReady() {
		// Nothing to do if cluster does not have autoscaling
		// enabled or is not ready to accept requests.
		return nil
	}

	// Ensure that none of the autoscalers are in maintenance
	// mode at this point since autoscaling is ready.
	if err = c.endAutoscalingMaintenanceMode(); err != nil {
		return nil
	}

	for _, autoscalerName := range autoscalers {
		// Fetch cached copy of autoscale resource and get associated config
		autoscaler, found := c.k8s.CouchbaseAutoscalers.Get(autoscalerName)
		if !found {
			return fmt.Errorf("failed to get autoscaler %q: %w", autoscalerName, errors.NewStackTracedError(errors.ErrResourceRequired))
		}

		config := c.cluster.Spec.GetServerConfigByName(autoscaler.Spec.Servers)
		if config == nil {
			return fmt.Errorf("autoscaler references a missing config %q: %w", autoscaler.Spec.Servers, errors.NewStackTracedError(errors.ErrResourceRequired))
		}

		// update status of the autoscaler resource
		autoscaler, err := c.updateAutoscalerStatusSize(autoscaler, *config)
		if err != nil {
			return err
		}

		// Check if autoscaler is requesting size change
		if config.Size != autoscaler.Spec.Size {
			requestedSize := autoscaler.Spec.Size
			currentSize := config.Size

			// Apply requested autoscaler size to CouchbaseCluster
			if err := c.applyAutoscaleSize(config.Name, requestedSize); err != nil {
				return err
			}

			// Since cluster is scaling, check and apply
			// any necessary stabilization procedures
			if c.cluster.Spec.AutoscaleStabilizationPeriod != nil {
				if err := c.applyAutoscaleStabilization(); err != nil {
					return err
				}
			}

			// Scaling Events
			message := fmt.Sprintf("Autoscaling service config %q from %d -> %d", config.Name, currentSize, requestedSize)
			log.Info(message, "cluster", c.namespacedName(), "name", config.Name)

			if currentSize < requestedSize {
				c.raiseEventCached(k8sutil.AutoscaleUpEvent(c.cluster, config.Name, currentSize, requestedSize))
			} else {
				c.raiseEventCached(k8sutil.AutoscaleDownEvent(c.cluster, config.Name, currentSize, requestedSize))
			}

			// stop reconciling autoscalers since size change has occurred
			break
		}
	}

	return nil
}

// Check if cluster is ready to reconcile any autoscaling requests.
func (c *Cluster) autoscalingReady() bool {
	// When scaling condition is not set then initialize as ready
	autoscaleReadyCondition := c.cluster.Status.GetCondition(couchbasev2.ClusterConditionAutoscaleReady)
	if autoscaleReadyCondition == nil {
		c.cluster.Status.SetAutoscalerReadyCondition("cluster is ready for autoscaling")
		return true
	}

	// When scaling condition is False then transition to True,
	// and if it is already True then check if time is needed
	// to stabilize.  The return value is only 'true' when the
	// scaling condition is True and no stabilization is needed.
	switch autoscaleReadyCondition.Status {
	// False condition implies that autoscaling was in maintenance mode.
	case v1.ConditionFalse:
		status, err := c.GetStatus()
		if err != nil {
			message := fmt.Sprintf("failed to get cluster status %v", err)
			log.Error(err, message, "cluster", c.namespacedName())

			break
		} else if !status.Balancing && status.Balanced {
			// If cluster is balanced then condition can be transitioned to true and
			// removed from maintenance mode.
			c.cluster.Status.SetAutoscalerReadyCondition("cluster preparing for autoscaling")
		}
	case v1.ConditionTrue:
		if c.cluster.Spec.AutoscaleStabilizationPeriod == nil {
			return true
		}

		// Check if we need to wait for the stabilization period to complete.
		stabilizationPeriod := c.cluster.Spec.AutoscaleStabilizationPeriod
		stabilizationStartTime, err := time.Parse(time.RFC3339, autoscaleReadyCondition.LastTransitionTime)

		if err != nil {
			message := fmt.Sprintf("Failed to parse transition time from autoscale condition %v", err)
			log.Error(err, message, "cluster", c.namespacedName())

			break
		}

		if stabilizationPeriod != nil {
			stabilizationEndTime := stabilizationStartTime.Add(stabilizationPeriod.Duration)

			timeRemaining := time.Until(stabilizationEndTime)
			if timeRemaining > 0 {
				message := fmt.Sprintf("cluster autoscaling is stabilizing: %s remaining", timeRemaining.String())
				log.Info(message, "cluster", c.namespacedName())

				// ensure that autoscalers remain in maintenance mode as it's possible
				// for some external client to manual change size of CouchbaseAutoscale
				// resource.  When this happens HPA thinks we are live and starts
				// sending scaling recommendations
				err := c.applyAutoscaleMaintenanceMode(c.getAutoscalersToDisable())
				if err != nil {
					log.Error(err, message, "cluster", c.namespacedName())
				}

				break
			}
		}

		return true
	case v1.ConditionUnknown:
		// Should never be in unknown condition, log and clear
		err := fmt.Errorf("autoscaler condition %w", errors.NewStackTracedError(errors.ErrUnknownCondition))
		log.Error(err, "cluster", c.namespacedName())
		c.cluster.Status.ClearCondition(couchbasev2.ClusterConditionAutoscaleReady)
	}

	return false
}

// Apply requested size from CouchbaseAutoscaler resource
// to CouchbaseCluster resource if the two differ.
// When size values differ the cluster is put into
// maintenance mode since scaling will occur as a result.
func (c *Cluster) applyAutoscaleSize(configName string, requestedSize int) error {
	// Fetching most recent version of the cluster spec
	cluster, err := c.k8s.CouchbaseClient.CouchbaseV2().CouchbaseClusters(c.cluster.Namespace).Get(context.Background(), c.cluster.Name, metav1.GetOptions{})
	if err != nil {
		return errors.NewStackTracedError(err)
	}

	for i := range c.cluster.Spec.Servers {
		if cluster.Spec.Servers[i].Name == configName {
			cluster.Spec.Servers[i].Size = requestedSize
		}
	}

	updatedCluster, err := c.k8s.CouchbaseClient.CouchbaseV2().CouchbaseClusters(c.cluster.Namespace).Update(context.Background(), cluster, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update cluster size: %w", errors.NewStackTracedError(err))
	}

	c.cluster = updatedCluster

	return nil
}

// applyAutoscaleStabilization puts all of the autoscalers into maintenance mode
// if a stabilization period is set.
func (c *Cluster) applyAutoscaleStabilization() error {
	autoscaleReadyCondition := c.cluster.Status.GetCondition(couchbasev2.ClusterConditionAutoscaleReady)
	// maintenance mode is only allowed when cluster scaling condition is True,
	// which means the cluster is ready to transition into unready (maintenance)
	if autoscaleReadyCondition != nil && autoscaleReadyCondition.Status == v1.ConditionTrue {
		return c.startAutoscalingMaintenanceMode()
	}

	return nil
}

// Updates Status.Size of the CouchbaseAutoscaler resource to
// match the number of existing Pods within a server config.
func (c *Cluster) updateAutoscalerStatusSize(autoscaler *couchbasev2.CouchbaseAutoscaler, config couchbasev2.ServerConfig) (*couchbasev2.CouchbaseAutoscaler, error) {
	configPods := 0

	for _, pod := range c.getClusterPods() {
		if config.Name == pod.Labels[constants.LabelNodeConf] {
			configPods++
		}
	}

	// update status subresource to actual ready pods from group
	if configPods != autoscaler.Status.Size {
		requestedAutoscaler, err := c.k8s.CouchbaseClient.CouchbaseV2().CouchbaseAutoscalers(c.cluster.Namespace).Get(context.Background(), autoscaler.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}

		requestedAutoscaler.Status.Size = configPods
		requestedAutoscaler, err = k8sutil.UpdateAutoscaler(c.k8s, c.cluster.Namespace, requestedAutoscaler)

		if err != nil {
			return nil, fmt.Errorf("%w: failed to update autoscaler status: %s", errors.NewStackTracedError(err), autoscaler.Name)
		}

		autoscaler = requestedAutoscaler
	}

	return autoscaler, nil
}

// Update existing autoscaler resources to match the set of resources being requested.
func (c *Cluster) updateRequestedAutoscalers() ([]string, error) {
	requestedAutoscalers := []string{}
	actualAutoscalers := []string{}

	for _, config := range c.cluster.Spec.Servers {
		if config.AutoscaleEnabled {
			// Create Autoscaler if it doesn't exist
			autoscalerName := config.AutoscalerName(c.cluster.Name)
			if _, found := c.k8s.CouchbaseAutoscalers.Get(autoscalerName); !found {
				_, err := k8sutil.CreateCouchbaseAutoscaler(c.k8s, c.cluster, config)
				if err != nil {
					return actualAutoscalers, errors.NewStackTracedError(err)
				}

				message := fmt.Sprintf("Autoscaler created for service config %q", config.Name)
				log.Info(message, "cluster", c.namespacedName())
				c.raiseEventCached(k8sutil.AutoscalerCreateEvent(c.cluster, config.Name))
			}

			requestedAutoscalers = append(requestedAutoscalers, config.Name)
		}
	}

	for _, autoscaler := range c.k8s.CouchbaseAutoscalers.List() {
		// delete autoscaler if it is not requested
		configName := autoscaler.Spec.Servers
		if _, found := couchbasev2.HasItem(configName, requestedAutoscalers); !found {
			err := c.k8s.CouchbaseClient.CouchbaseV2().CouchbaseAutoscalers(c.cluster.Namespace).Delete(context.Background(), autoscaler.Name, *metav1.NewDeleteOptions(0))
			if err != nil {
				return actualAutoscalers, err
			}

			message := fmt.Sprintf("Autoscaling disabled for service config %q", configName)
			log.Info(message, "cluster", c.namespacedName())
			c.raiseEventCached(k8sutil.AutoscalerDeleteEvent(c.cluster, configName))

			continue
		}

		// ensure autoscaler is added to status since it is requested for use
		actualAutoscalers = append(actualAutoscalers, autoscaler.Name)
	}

	sort.Strings(actualAutoscalers)
	c.cluster.Status.Autoscalers = actualAutoscalers

	return actualAutoscalers, nil
}

// Autoscaling is put into maintenance mode by setting scale size of the CouchbaseAutoscaler to 0.
// This causes the referencing HorizontalPodAutoscaler to stop adjusting the desired size.
// https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/#implicit-maintenance-mode-deactivation
func (c *Cluster) startAutoscalingMaintenanceMode() error {
	requestedAutoscalers := c.getAutoscalersToDisable()
	if len(requestedAutoscalers) > 0 {
		c.cluster.Status.SetAutoscalerUnreadyCondition("autoscaling is paused")
		return c.applyAutoscaleMaintenanceMode(requestedAutoscalers)
	}

	return nil
}

// Gets list of autoscalers that need to be disabled for autoscaling mode.
func (c *Cluster) getAutoscalersToDisable() []*couchbasev2.CouchbaseAutoscaler {
	autoscalers := []*couchbasev2.CouchbaseAutoscaler{}

	for _, autoscaler := range c.k8s.CouchbaseAutoscalers.List() {
		if autoscaler.Spec.Size != 0 {
			autoscalers = append(autoscalers, autoscaler)
		}
	}

	return autoscalers
}

// Applies size of `0` to autoscalers to indicate maintenance mode.
func (c *Cluster) applyAutoscaleMaintenanceMode(autoscalers []*couchbasev2.CouchbaseAutoscaler) error {
	for _, autoscaler := range autoscalers {
		requestedAutoscaler, err := c.k8s.CouchbaseClient.CouchbaseV2().CouchbaseAutoscalers(c.cluster.Namespace).Get(context.Background(), autoscaler.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		requestedAutoscaler.Spec.Size = 0

		msg := fmt.Sprintf("Autoscaler for service config %q is entering maintenance mode", requestedAutoscaler.Spec.Servers)
		log.Info(msg, "cluster", c.namespacedName())
		_, err = k8sutil.UpdateAutoscaler(c.k8s, c.cluster.Namespace, requestedAutoscaler)

		if err != nil {
			return err
		}
	}

	return nil
}

// End autoscaling maintenance mode checks if an autoscaler was previously in
// maintenance and syncs it with the expected size of the server config.
func (c *Cluster) endAutoscalingMaintenanceMode() error {
	for _, autoscaler := range c.k8s.CouchbaseAutoscalers.List() {
		config := c.cluster.Spec.GetServerConfigByName(autoscaler.Spec.Servers)
		if config == nil {
			return fmt.Errorf("autoscaler references a missing config %q: %w", autoscaler.Spec.Servers, errors.NewStackTracedError(errors.ErrResourceRequired))
		}

		// Size of 0 indicates maintenance mode.
		if autoscaler.Spec.Size == 0 {
			// Restore autoscaler size from the config size
			requestedAutoscaler := autoscaler.DeepCopy()
			requestedAutoscaler.Spec.Size = config.Size
			_, err := k8sutil.UpdateAutoscaler(c.k8s, c.cluster.Namespace, requestedAutoscaler)

			if err != nil {
				return err
			}
		}
	}

	return nil
}
