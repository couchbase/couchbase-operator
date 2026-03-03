/*
Copyright 2024-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package cluster

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type CNGConfig struct {
	LogLevel string `json:"log-level"`
}

func (c *Cluster) reconcileCloudNativeGatewayConfig() error {
	cng := c.cluster.Spec.Networking.CloudNativeGateway
	if cng == nil {
		return nil
	}

	configMapName := k8sutil.GetCNGConfigMapName(c.cluster)
	current, exists := c.k8s.ConfigMaps.Get(configMapName)

	config := CNGConfig{
		LogLevel: string(cng.LogLevel),
	}

	configData, err := json.MarshalIndent(config, "", "  ")

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
