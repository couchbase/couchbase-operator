package cluster

import (
	"context"
	"reflect"
	"strconv"

	"github.com/couchbase/couchbase-operator/pkg/client"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	LoggingConfigurationManagedAnnotation = "logging.couchbase.com/managed"
)

// reconcileLogConfig will create a default ConfigMap for log configuration if one does not exist.
func (c *Cluster) reconcileLogConfig(client *client.Client) error {
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
	current, exists := client.Secrets.Get(fbs.ConfigurationName)

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
			// Make the CouchbaseCluster the owner so it gets GC'd together
			OwnerReferences: []metav1.OwnerReference{
				c.cluster.AsOwner(),
			},
		},
		// Whilst the container contains a default configuration we want to mount the whole directory - if you use sub-paths then it will not auto-update.
		// https://github.com/kubernetes/kubernetes/issues/50345
		// We allow for just specifying some config and reusing parser definitions by using two directories in the image: this configuration directory
		// and then common stuff all in /fluent-bit/etc/.
		Data: map[string][]byte{
			// Just include the default configuration now
			"fluent-bit.conf": []byte("@include /fluent-bit/etc/fluent-bit.conf\n"),

			// Redaction salt if required using default of cluster name
			"redaction.salt": []byte(c.cluster.ClusterName),
		},
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

		if _, err := client.KubeClient.CoreV1().Secrets(c.cluster.Namespace).Update(context.Background(), requestedConfig, metav1.UpdateOptions{}); err != nil {
			return errors.NewStackTracedError(err)
		}

		return nil
	}

	// If we get here then must not exist
	log.Info("Creating default log config secret", "cluster", c.namespacedName(), "name", fbs.ConfigurationName)

	if _, err := client.KubeClient.CoreV1().Secrets(c.cluster.Namespace).Create(context.Background(), requestedConfig, metav1.CreateOptions{}); err != nil {
		return errors.NewStackTracedError(err)
	}

	return nil
}
