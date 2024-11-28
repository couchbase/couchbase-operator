package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type CNGConfig struct {
	LogLevel          string `json:"log-level"`
	DapiPort          int    `json:"dapi-port,omitempty"`
	DapiProxyServices string `json:"dapi-proxy-services,omitempty"`
}

func (c *Cluster) reconcileCloudNativeGatewayConfig() error {
	cng := c.cluster.Spec.Networking.CloudNativeGateway
	if cng == nil {
		return nil
	}

	configMapName := k8sutil.GetCNGConfigMapName(c.cluster)
	current, exists := c.k8s.ConfigMaps.Get(configMapName)

	newConfig := CNGConfig{
		LogLevel: string(cng.LogLevel),
	}

	if cng.DataAPI != nil && cng.DataAPI.Enabled {
		newConfig.DapiPort = k8sutil.CNGDAPIServicePort
		if cng.DataAPI.ProxyServices != nil {
			newConfig.DapiProxyServices = strings.Join(cng.DataAPI.ProxyServices.StringSlice(), ",")
		}
	}

	configData, err := json.MarshalIndent(newConfig, "", "  ")

	if err != nil {
		return err
	}

	requestedConfig := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:            configMapName,
			Labels:          k8sutil.LabelsForClusterResource(c.cluster),
			OwnerReferences: []metav1.OwnerReference{c.cluster.AsOwner()},
		},
		Data: map[string]string{
			k8sutil.CngConfigFilename: string(configData),
		},
	}

	k8sutil.ApplyBaseAnnotations(requestedConfig)

	if exists {
		if reflect.DeepEqual(current.Data, requestedConfig.Data) {
			return nil
		}

		log.Info("Updating Cloud Native Gateway ConfigMap", "cluster", c.namespacedName(), "name", configMapName)

		_, err := c.k8s.KubeClient.CoreV1().ConfigMaps(c.cluster.Namespace).Update(context.Background(), requestedConfig, metav1.UpdateOptions{})

		if err != nil {
			return errors.NewStackTracedError(err)
		}

		var currentConfig CNGConfig

		err = json.Unmarshal([]byte(current.Data[k8sutil.CngConfigFilename]), &currentConfig)

		if err != nil {
			return fmt.Errorf("unable to check whether Cloud Native Gateway requires a restart, refer to the container logs: %w", err)
		}

		// If the dapi-proxy-services list has changed or the dapi has been disabled, we need to restart the pods to force the CNG container to restart.
		// CNG will be updated in the future to be dynamic, in which case this can be removed.
		if currentConfig.DapiPort != newConfig.DapiPort || !couchbaseutil.DoStringSlicesContainEqualValues(newConfig.DapiProxyServices, currentConfig.DapiProxyServices, ",") {
			log.Info("Adding reschedule annotation to force pod restart due to CNG config changes", "cluster", c.namespacedName())

			for name := range c.members {
				pod, exists := c.k8s.Pods.Get(name)

				if !exists {
					continue
				}

				pod.Annotations[constants.AnnotationReschedule] = "true"
			}
		}

		return nil
	}

	// If we reach here then the CM doesn't exist
	log.Info("Creating Cloud Native Gateway ConfigMap", "cluster", c.namespacedName(), "name", configMapName)

	_, err = c.k8s.KubeClient.CoreV1().ConfigMaps(c.cluster.Namespace).Create(context.Background(), requestedConfig, metav1.CreateOptions{})

	if err != nil {
		return errors.NewStackTracedError(err)
	}

	return nil
}
