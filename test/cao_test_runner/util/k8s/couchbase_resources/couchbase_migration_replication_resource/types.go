package couchbasemigrationreplicationresource

import "time"

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

type MigrationMapping struct {
	Mappings []struct {
		Filter         string `json:"filter"`
		TargetKeyspace struct {
			Collection string `json:"collection"`
			Scope      string `json:"scope"`
		} `json:"targetKeyspace"`
	} `json:"mappings"`
}

type Spec struct {
	Bucket           string `json:"bucket"`
	CompressionType  string `json:"compressionType"`
	FilterExpression string `json:"filterExpression"`
	Paused           bool   `json:"paused"`
	RemoteBucket     string `json:"remoteBucket"`
}

type Status struct {
}

type CouchbaseMigrationReplicationResource struct {
	APIVersion       string           `json:"apiVersion"`
	Kind             string           `json:"kind"`
	Metadata         Metadata         `json:"metadata"`
	MigrationMapping MigrationMapping `json:"migrationMapping"`
	Spec             Spec             `json:"spec"`
	Status           Status           `json:"status"`
}

type CouchbaseMigrationReplicationResourceList struct {
	APIVersion                             string                                   `json:"apiVersion"`
	CouchbaseMigrationReplicationResources []*CouchbaseMigrationReplicationResource `json:"items"`
	Kind                                   string                                   `json:"kind"`
}
