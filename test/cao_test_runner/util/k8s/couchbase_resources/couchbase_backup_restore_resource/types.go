package couchbasebackuprestoreresource

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
	Exclude      []string `json:"exclude"`
	FilterKeys   string   `json:"filterKeys"`
	FilterValues string   `json:"filterValues"`
	Include      []string `json:"include"`
	Map          []struct {
		Source string `json:"source"`
		Target string `json:"target"`
	} `json:"map"`
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

type Spec struct {
	BackoffLimit int         `json:"backoffLimit"`
	Backup       string      `json:"backup"`
	Buckets      interface{} `json:"buckets"`
	Data         Data        `json:"data"`
	End          struct {
		Int int    `json:"int"`
		Str string `json:"str"`
	} `json:"end"`
	ForceUpdates   bool        `json:"forceUpdates"`
	LogRetention   string      `json:"logRetention"`
	ObjectStore    ObjectStore `json:"objectStore"`
	OverwriteUsers bool        `json:"overwriteUsers"`
	Repo           string      `json:"repo"`
	S3Bucket       string      `json:"s3bucket"`
	Services       struct {
		Analytics        bool `json:"analytics"`
		BucketConfig     bool `json:"bucketConfig"`
		BucketQuery      bool `json:"bucketQuery"`
		ClusterAnalytics bool `json:"clusterAnalytics"`
		ClusterQuery     bool `json:"clusterQuery"`
		Data             bool `json:"data"`
		Eventing         bool `json:"eventing"`
		FtAlias          bool `json:"ftAlias"`
		FtIndex          bool `json:"ftIndex"`
		GsiIndex         bool `json:"gsiIndex"`
		Users            bool `json:"users"`
		Views            bool `json:"views"`
	} `json:"services"`
	StagingVolume struct {
		Size             string `json:"size"`
		StorageClassName string `json:"storageClassName"`
	} `json:"stagingVolume"`
	Start struct {
		Int int    `json:"int"`
		Str string `json:"str"`
	} `json:"start"`
	Threads                 int `json:"threads"`
	TtlSecondsAfterFinished int `json:"ttlSecondsAfterFinished"`
}

type Status struct {
	Archive string `json:"archive"`
	Backups []struct {
		Full         string   `json:"full"`
		Incrementals []string `json:"incrementals"`
		Name         string   `json:"name"`
	} `json:"backups"`
	Duration    string `json:"duration"`
	Failed      bool   `json:"failed"`
	Job         string `json:"job"`
	LastFailure string `json:"lastFailure"`
	LastRun     string `json:"lastRun"`
	LastSuccess string `json:"lastSuccess"`
	Output      string `json:"output"`
	Pod         string `json:"pod"`
	Repo        string `json:"repo"`
	Running     bool   `json:"running"`
}

type CouchbaseBackupRestoreResource struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`
	Spec       Spec     `json:"spec"`
	Status     Status   `json:"status"`
}

type CouchbaseBackupRestoreResourceList struct {
	APIVersion                      string                            `json:"apiVersion"`
	CouchbaseBackupRestoreResources []*CouchbaseBackupRestoreResource `json:"items"`
	Kind                            string                            `json:"kind"`
}
