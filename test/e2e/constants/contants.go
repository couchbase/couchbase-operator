package constants

import (
	util_const "github.com/couchbase/couchbase-operator/pkg/util/constants"
)

// These values can be updated from e2espec/crd.go
var (
	CbServerBaseImage = "couchbase/server"
	CbServerVersion   = "enterprise-5.5.0"

	// Storage class defined in K8S cluster
	StorageClassName = "standard"
)

// Const Ansible setting string
var (
	AnsibleLoginSectionData = map[string]string{
		"ansible_connection":      "ssh",
		"ansible_ssh_user":        "root",
		"ansible_ssh_pass":        "couchbase",
		"ansible_ssh_common_args": "-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null",
	}

	CbAppSelectorMap = map[string]string{
		"app": "couchbase",
	}
)

var (
	// Couchbase cluster
	ClusterNamePrefix          = "test-couchbase-"
	CouchbaseLabel             = util_const.LabelApp + "=" + util_const.App
	CouchbaseOperatorLabel     = util_const.LabelApp + "=couchbase-operator"
	CouchbaseServerClusterKey  = "couchbase_cluster"
	CouchbaseServerPodLabelStr = CouchbaseLabel + "," + CouchbaseServerClusterKey + "="
)

var (
	// Secret name in kube used for testing
	KubeTestSecretName = "basic-test-secret"

	SecretUsernameKey = util_const.AuthSecretUsernameKey
	SecretPasswordKey = util_const.AuthSecretPasswordKey
	CbClusterUsername = "Administrator"
	CbClusterPassword = "password"
)

const (
	// Operator constants
	OperatorRestPort  int32  = 8080
	DefOperatorImgTag string = "couchbase/operator:1.0.0"

	// Couchbase cluster constants
	CbClusterRestPort int32 = 8091
)

const (
	Size1 = 1
	Size2 = 2
	Size3 = 3
	Size4 = 4
	Size5 = 5
)

const (
	WithBucket    = true
	WithoutBucket = false
	AdminExposed  = true
	AdminHidden   = false

	BucketFlushEnabled   = true
	BucketFlushDisabled  = false
	IndexReplicaEnabled  = true
	IndexReplicaDisabled = false
)

const (
	Retries1   = 1
	Retries5   = 5
	Retries10  = 10
	Retries20  = 20
	Retries30  = 30
	Retries60  = 60
	Retries120 = 120
)

const (
	Mem256Mb = 256
	Mem512Mb = 512
	Mem1Gb   = 1024
)
