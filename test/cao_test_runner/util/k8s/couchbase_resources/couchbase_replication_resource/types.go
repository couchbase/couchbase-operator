package couchbaserolebindingresource

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

type Spec struct {
	Bucket           string `json:"bucket"`
	Paused           bool   `json:"paused"`
	CompressionType  string `json:"compressionType"`
	FilterExpression string `json:"filterExpression"`
	RemoteBucket     string `json:"remoteBucket"`
}

type Status struct {
}

type CouchbaseReplicationResource struct {
	APIVersion      string `json:"apiVersion"`
	ExplicitMapping struct {
		AllowRules []struct {
			SourceKeyspace struct {
				Collection string `json:"collection"`
				Scope      string `json:"scope"`
			} `json:"sourceKeyspace"`
			TargetKeyspace struct {
				Collection string `json:"collection"`
				Scope      string `json:"scope"`
			} `json:"targetKeyspace"`
		} `json:"allowRules"`
		DenyRules []struct {
			SourceKeyspace struct {
				Collection string `json:"collection"`
				Scope      string `json:"scope"`
			} `json:"sourceKeyspace"`
		} `json:"denyRules"`
	} `json:"explicitMapping"`
	Kind     string   `json:"kind"`
	Metadata Metadata `json:"metadata"`
	Spec     Spec     `json:"spec"`
	Status   Status   `json:"status"`
}

type CouchbaseReplicationResourceList struct {
	APIVersion                    string                          `json:"apiVersion"`
	CouchbaseReplicationResources []*CouchbaseReplicationResource `json:"items"`
	Kind                          string                          `json:"kind"`
}
