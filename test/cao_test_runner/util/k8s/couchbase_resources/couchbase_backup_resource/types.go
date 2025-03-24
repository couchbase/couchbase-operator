package couchbasebackupresource

import (
	"time"
)

type Metadata struct {
	Annotations       map[string]string   `json:"annotations"`
	CreationTimestamp time.Time           `json:"creationTimestamp"`
	Labels            map[string]string   `json:"labels"`
	Name              string              `json:"name"`
	Namespace         string              `json:"namespace"`
	OwnerReferences   []MetadataOwnerRefs `json:"ownerReferences"`
	ResourceVersion   string              `json:"resourceVersion"`
	UID               string              `json:"uid"`
}

type MetadataOwnerRefs struct {
	APIVersion         string `json:"apiVersion"`
	BlockOwnerDeletion bool   `json:"blockOwnerDeletion"`
	Controller         bool   `json:"controller"`
	Kind               string `json:"kind"`
	Name               string `json:"name"`
	UID                string `json:"uid"`
}

type Data struct {
	Exclude []string `json:"exclude"`
	Include []string `json:"include"`
}

type ObjectStore struct {
	Endpoint struct {
		Secret         string `json:"secret"`
		URL            string `json:"url"`
		UseVirtualPath bool   `json:"useVirtualPath"`
	} `json:"endpoint"`
	Secret string `json:"secret"`
	URI    string `json:"uri"`
	UseIAM bool   `json:"useIAM"`
}

type Services struct {
	Analytics        bool `json:"analytics"`
	BucketConfig     bool `json:"bucketConfig"`
	BucketQuery      bool `json:"bucketQuery"`
	ClusterAnalytics bool `json:"clusterAnalytics"`
	ClusterQuery     bool `json:"clusterQuery"`
	Data             bool `json:"data"`
	Eventing         bool `json:"eventing"`
	FtsAliases       bool `json:"ftsAliases"`
	FtsIndex         bool `json:"ftsIndexes"`
	GsiIndex         bool `json:"gsiIndexes"`
	Users            bool `json:"users"`
	Views            bool `json:"views"`
}

type Spec struct {
	AutoScaling struct {
		IncrementPercent int    `json:"incrementPercent"`
		Limit            string `json:"limit"`
		ThresholdPercent int    `json:"thresholdPercent"`
	} `json:"autoScaling"`
	BackoffLimit           int    `json:"backoffLimit"`
	BackupRetention        string `json:"backupRetention"`
	Data                   Data   `json:"data"`
	DefaultRecoveryMethod  string `json:"defaultRecoveryMethod"`
	EphemeralVolume        bool   `json:"ephemeralVolume"`
	FailedJobsHistoryLimit int    `json:"failedJobsHistoryLimit"`
	Full                   struct {
		Schedule string `json:"schedule"`
	} `json:"full"`
	Incremental struct {
		Schedule string `json:"schedule"`
	} `json:"incremental"`
	LogRetention               string      `json:"logRetention"`
	ObjectStore                ObjectStore `json:"objectStore"`
	S3Bucket                   string      `json:"s3bucket"`
	Services                   Services    `json:"services"`
	Size                       string      `json:"size"`
	StorageClassName           string      `json:"storageClassName"`
	Strategy                   string      `json:"strategy"`
	SuccessfulJobsHistoryLimit int         `json:"successfulJobsHistoryLimit"`
	Threads                    int         `json:"threads"`
	TtlSecondsAfterFinished    int         `json:"ttlSecondsAfterFinished"`
}

type Status struct {
}

type CouchbaseBackupResource struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`
	Spec       Spec     `json:"spec"`
	Status     Status   `json:"status"`
}

type CouchbaseBackupResourceList struct {
	APIVersion               string                     `json:"apiVersion"`
	CouchbaseBackupResources []*CouchbaseBackupResource `json:"items"`
	Kind                     string                     `json:"kind"`
}
