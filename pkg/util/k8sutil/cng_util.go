/*
Copyright 2024-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package k8sutil

import (
	"path"
	"strings"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	v1 "k8s.io/api/core/v1"
)

const (
	CngConfigMapPrefix  = "couchbase-cloud-native-gateway-config-"
	CngConfigVolumeName = "couchbase-cloud-native-gateway-config-volume"
	CngConfigMountPath  = "/etc/couchbase/"
	CngConfigFilename   = "cloud-native-gateway.json"
)

func createCNGVolume(c *couchbasev2.CouchbaseCluster) v1.Volume {
	// ensure group users can execute script as we may be subject to fsGroup
	var chmod int32 = 0555

	return v1.Volume{
		Name: CngConfigVolumeName,
		VolumeSource: v1.VolumeSource{
			ConfigMap: &v1.ConfigMapVolumeSource{
				LocalObjectReference: v1.LocalObjectReference{Name: GetCNGConfigMapName(c)},
				DefaultMode:          &chmod,
			},
		},
	}
}

func GetCNGConfigMapName(c *couchbasev2.CouchbaseCluster) string {
	return strings.Join([]string{CngConfigMapPrefix, c.Name}, "")
}

func getCNGConfigFilePath() string {
	return path.Join(CngConfigMountPath, CngConfigFilename)
}
