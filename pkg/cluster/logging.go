/*
Copyright 2021-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package cluster

import (
	"context"
	"reflect"
	"strconv"

	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// LoggingConfigurationManagedAnnotation is the annotation added to any managed secrets.
	LoggingConfigurationManagedAnnotation = "logging.couchbase.com/managed"
)

// reconcileLogConfig will create a default ConfigMap for log configuration if one does not exist.
func (c *Cluster) reconcileLogConfig() error {
	fbs := c.cluster.Spec.Logging.Server
	if fbs == nil || !fbs.Enabled {
		return nil
	}

	// We want to default to being managed but this requires an optional boolean so if it is nil then managed.
	managed := true
	if fbs.ManageConfiguration != nil {
		// If it is present we must be explicitly setting it to true or false
		managed = (*fbs.ManageConfiguration)
	}

	// No further checks if not managed by ourselves - assumption is it is valid as manually controlled.
	// Note there is no check on existence so if the configuration is required for the container to start then be aware.
	if !managed {
		return nil
	}

	// Retrieve the current configuration and whether it exists at all
	current, exists := c.k8s.Secrets.Get(fbs.ConfigurationName)

	// Use a ConfigMap as this can then be dynamically changed and picked up by existing sidecars (or new ones).
	// The config map means we are fully configurable as well to handle output to ES, Azure, S3, etc, per customer.
	// Note that FluentBit currently does not support hot reloading: https://github.com/fluent/fluent-bit/issues/365
	requestedConfig := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:   fbs.ConfigurationName,
			Labels: k8sutil.LabelsForClusterResource(c.cluster),
			// Indicate operator managed
			Annotations: map[string]string{
				LoggingConfigurationManagedAnnotation: strconv.FormatBool(true),
			},
		},
		// Whilst the container contains a default configuration we want to mount the whole directory - if you use sub-paths then it will not auto-update.
		// https://github.com/kubernetes/kubernetes/issues/50345
		// We allow for just specifying some config and reusing parser definitions by using two directories in the image: this configuration directory
		// and then common stuff all in /fluent-bit/etc/.
		Data: map[string][]byte{
			// Just include the default configuration now
			k8sutil.LoggingConfigurationFile: []byte("@include /fluent-bit/etc/fluent-bit.conf\n"),

			// Redaction salt if required using default of cluster name
			"redaction.salt": []byte(c.cluster.ObjectMeta.Name),
		},
	}

	// Do not change ownership for existing Secrets as this requires additional permissions (delete).
	// This implies a 1-1 mapping for CouchbaseCluster <--> Secret in the same namespace.
	// Using the same Secret for multiple clusters would cause it to be GC'd when the original cluster that created it is.
	if !exists {
		// Ideally we would append all clusters as they use it but this requires `delete` permission on Secrets which may not be acceptable.
		requestedConfig.ObjectMeta.OwnerReferences = append(requestedConfig.ObjectMeta.OwnerReferences, c.cluster.AsOwner())
	}

	// Add on the common annotations for version
	k8sutil.ApplyBaseAnnotations(requestedConfig)

	// Now, if it exists but is not equal then update otherwise ignore as identical so no need to change
	if exists {
		// Check if identical so we can ignore - we ignore metadata, only care about the actual configuration in Data (B64 encoded)
		if reflect.DeepEqual(current.Data, requestedConfig.Data) {
			return nil
		}

		log.Info("Updating default log config secret", "cluster", c.namespacedName(), "name", fbs.ConfigurationName)

		// This will make any meta-data updates as well.
		if _, err := c.k8s.KubeClient.CoreV1().Secrets(c.cluster.Namespace).Update(context.Background(), requestedConfig, metav1.UpdateOptions{}); err != nil {
			return errors.NewStackTracedError(err)
		}

		return nil
	}

	// If we get here then must not exist
	log.Info("Creating default log config secret", "cluster", c.namespacedName(), "name", fbs.ConfigurationName)

	if _, err := c.k8s.KubeClient.CoreV1().Secrets(c.cluster.Namespace).Create(context.Background(), requestedConfig, metav1.CreateOptions{}); err != nil {
		return errors.NewStackTracedError(err)
	}

	return nil
}
