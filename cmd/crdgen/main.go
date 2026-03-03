/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil/v2"

	"github.com/ghodss/yaml"
)

var (
	crds []interface{}
)

// buffer post processes raw output strings and buffers them
func buffer(crd interface{}) {
	crds = append(crds, crd)
}

// dump formats the CRDs as YAML and echos to standard out
func dump() error {
	var yamls []string

	for _, crd := range crds {
		data, err := yaml.Marshal(crd)
		if err != nil {
			return err
		}
		// Hack: the status attribute is formatted, but the API rejects this so
		// we need a way to rid ourselves of it.
		parts := strings.Split(string(data), "\nstatus:\n")
		yamls = append(yamls, parts[0])
	}

	fmt.Println(strings.Join(yamls, "\n---\n"))
	return nil
}

func main() {
	buffer(v2.GetCouchbaseBucketCRD())
	buffer(v2.GetCouchbaseEphemeralBucketCRD())
	buffer(v2.GetCouchbaseMemcachedBucketCRD())
	buffer(v2.GetCouchbaseReplicationCRD())
	buffer(v2.GetUserCRD())
	buffer(v2.GetGroupCRD())
	buffer(v2.GetRoleBindingCRD())
	buffer(v2.GetCouchbaseClusterCRD())
	buffer(v2.GetCouchbaseBackupCRD())
	buffer(v2.GetCouchbaseBackupRestoreCRD())
	if err := dump(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
