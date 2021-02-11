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
		Data: map[string][]byte{
			// The formatting of the file is very important so using space & newlines to control rather than multi-line strings
			// Do not use tabs
			// Configure fluent bit to also include the pod name and some helper keys
			"fluent-bit.conf": []byte("[SERVICE]\n" +
				"    flush        	1\n" +
				"    daemon       	Off\n" +
				"    log_level    	warn\n" +
				"    parsers_file 	parsers.conf\n" +
				"# This is required to simplify downstream parsing to filter the different pod logs\n" +
				"[FILTER]\n" +
				"    Name           modify\n" +
				"    Match          *\n" +
				"    Add            pod        ${HOSTNAME}\n" +
				"    Add            logshipper fluentbit-sidecar\n" +
				"@include output.conf\n" +
				"@include input.conf\n"),

			// Default to only reading the audit log in default location
			"input.conf": []byte("[INPUT]\n" +
				"    Name           tail\n" +
				"    Path           ${COUCHBASE_LOGS}/audit.log\n" +
				"    Parser         auditdb_log\n" +
				"    Path_Key       filename\n" +
				"    Tag            couchbase.log.audit\n"),

			// Default to only sending to stdout
			"output.conf": []byte("[OUTPUT]\n" +
				"    name           stdout\n" +
				"    match          couchbase.log.*\n"),

			// Parsers provided for audit log, a general erlang one that copes with multiline messages and
			// the simple_log copes with indexer.log or similar TIME LEVEL MESSAGE log formats. In each case
			// we only 'parse' the time, level and message fields - no further decoding is done.
			"parsers.conf": []byte("[PARSER]\n" +
				"    Name           auditdb_log\n" +
				"    Format       	json\n" +
				"    Time_Key     	timestamp\n" +
				"    Time_Format  	%Y-%m-%dT%H:%M:%S.%L\n\n" +

				"[PARSER]\n" +
				"    Name           simple_log\n" +
				"    Format         regex\n" +
				"    Regex          ^(?<time>\\d+-\\d+-\\d+T\\d+:\\d+:\\d+.\\d+(\\+|-)\\d+:\\d+)\\s+\\[(?<level>\\w+)\\](?<message>.*)$\n" +
				"    Time_Key       time\n" +
				"    Time_Format    %Y-%m-%dT%H:%M:%S.%L%z\n\n" +

				"[PARSER]\n" +
				"    Name           erlang_multiline\n" +
				"    Format         regex\n" +
				"    Regex          ^\\[(?<logger>\\w+):(?<level>\\w+),(?<time>\\d+-\\d+-\\d+T\\d+:\\d+:\\d+.\\d+Z).*](?<message>.*)$\n" +
				"    Time_Key       time\n" +
				"    Time_Format    %Y-%m-%dT%H:%M:%S.%L\n"),
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
