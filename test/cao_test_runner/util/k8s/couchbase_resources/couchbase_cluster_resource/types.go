package couchbaseclusterresource

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
	AntiAffinity                       bool          `json:"antiAffinity"`
	AutoResourceAllocation             interface{}   `json:"autoResourceAllocation"`
	AutoscaleStabilizationPeriod       string        `json:"autoscaleStabilizationPeriod"`
	Backup                             interface{}   `json:"backup"`
	Buckets                            interface{}   `json:"buckets"`
	Cluster                            interface{}   `json:"cluster"`
	EnableOnlineVolumeExpansion        bool          `json:"enableOnlineVolumeExpansion"`
	EnablePreviewScaling               bool          `json:"enablePreviewScaling"`
	EnvImagePrecedence                 bool          `json:"envImagePrecedence"`
	Hibernate                          bool          `json:"hibernate"`
	HibernationStrategy                string        `json:"hibernationStrategy"`
	Image                              string        `json:"image"`
	Logging                            interface{}   `json:"logging"`
	Migration                          interface{}   `json:"migration"`
	Monitoring                         interface{}   `json:"monitoring"`
	Networking                         interface{}   `json:"networking"`
	OnlineVolumeExpansionTimeoutInMins int           `json:"onlineVolumeExpansionTimeoutInMins"`
	Paused                             bool          `json:"paused"`
	PerServiceClassPDB                 bool          `json:"perServiceClassPDB"`
	Platform                           string        `json:"platform"`
	RecoveryPolicy                     string        `json:"recoveryPolicy"`
	RollingUpgrade                     interface{}   `json:"rollingUpgrade"`
	Security                           interface{}   `json:"security"`
	ServerGroups                       []string      `json:"serverGroups"`
	Servers                            []interface{} `json:"servers"`
	SoftwareUpdateNotifications        bool          `json:"softwareUpdateNotifications"`
	UpgradeProcess                     string        `json:"upgradeProcess"`
	UpgradeStrategy                    string        `json:"upgradeStrategy"`
	VolumeClaimTemplates               []interface{} `json:"volumeClaimTemplates"`
	Xdcr                               interface{}   `json:"xdcr"`
}

type Status struct {
}

type CouchbaseClusterResource struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`
	Spec       Spec     `json:"spec"`
	Status     Status   `json:"status"`
}

type CouchbaseClusterResourceList struct {
	APIVersion                string                      `json:"apiVersion"`
	CouchbaseClusterResources []*CouchbaseClusterResource `json:"items"`
	Kind                      string                      `json:"kind"`
}
