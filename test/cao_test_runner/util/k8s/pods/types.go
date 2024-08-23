package pods

import (
	"time"
)

type Metadata struct {
	Annotations       map[string]string   `json:"annotations"`       // pods: all
	CreationTimestamp time.Time           `json:"creationTimestamp"` // pods: all
	Labels            map[string]string   `json:"labels"`            // pods: all
	Name              string              `json:"name"`              // pods: all
	Namespace         string              `json:"namespace"`         // pods: all
	OwnerReferences   []MetadataOwnerRefs `json:"ownerReferences"`   // pods: all
	ResourceVersion   string              `json:"resourceVersion"`   // pods: all
	UID               string              `json:"uid"`               // pods: all
}

type MetadataOwnerRefs struct {
	APIVersion         string `json:"apiVersion"`
	BlockOwnerDeletion bool   `json:"blockOwnerDeletion"`
	Controller         bool   `json:"controller"`
	Kind               string `json:"kind"`
	Name               string `json:"name"`
	UID                string `json:"uid"`
}

type SpecContainers struct {
	Image           string `json:"image"`
	ImagePullPolicy string `json:"imagePullPolicy"`
	Name            string `json:"name"`
	Ports           []struct {
		ContainerPort int    `json:"containerPort"`
		Name          string `json:"name"`
		Protocol      string `json:"protocol"`
	} `json:"ports"`
	VolumeMounts []struct {
		MountPath string `json:"mountPath"`
		Name      string `json:"name"`
		SubPath   string `json:"subPath,omitempty"`
		ReadOnly  bool   `json:"readOnly,omitempty"`
	} `json:"volumeMounts"`
}

type SpecTolerations struct {
	Effect            string `json:"effect"`
	Key               string `json:"key"`
	Operator          string `json:"operator"`
	TolerationSeconds int    `json:"tolerationSeconds"`
}

type SpecVolumes struct {
	Name                  string `json:"name"`
	PersistentVolumeClaim struct {
		ClaimName string `json:"claimName"`
	} `json:"persistentVolumeClaim,omitempty"`
}

type ContainerStatuses struct {
	ContainerID  string                       `json:"containerID"`
	Image        string                       `json:"image"`
	ImageID      string                       `json:"imageID"`
	Name         string                       `json:"name"`
	Ready        bool                         `json:"ready"`
	RestartCount int                          `json:"restartCount"`
	Started      bool                         `json:"started"`
	State        map[string]map[string]string `json:"state"`
}

type StatusConditions struct {
	LastTransitionTime time.Time `json:"lastTransitionTime"`
	Status             string    `json:"status"`
	Type               string    `json:"type"`
}

type Spec struct {
	Containers         []SpecContainers  `json:"containers"`         // pods: all
	DNSPolicy          string            `json:"dnsPolicy"`          // pods: all
	EnableServiceLinks bool              `json:"enableServiceLinks"` // pods: all
	Hostname           string            `json:"hostname"`           // pods: cb-server
	NodeName           string            `json:"nodeName"`           // pods: all
	NodeSelector       map[string]string `json:"nodeSelector"`       // pods: cb-server
	PreemptionPolicy   string            `json:"preemptionPolicy"`   // pods: all
	Priority           int               `json:"priority"`           // pods: all
	RestartPolicy      string            `json:"restartPolicy"`      // pods: all
	SchedulerName      string            `json:"schedulerName"`      // pods: all
	ServiceAccount     string            `json:"serviceAccount"`     // pods: all
	ServiceAccountName string            `json:"serviceAccountName"` // pods: all
	Subdomain          string            `json:"subdomain"`          // pods: cb-server
	Tolerations        []SpecTolerations `json:"tolerations"`        // pods: all
	Volumes            []SpecVolumes     `json:"volumes"`            // pods: whichever pod claims volume
}

type Status struct {
	Conditions        []StatusConditions  `json:"conditions"`
	ContainerStatuses []ContainerStatuses `json:"containerStatuses"`
	HostIP            string              `json:"hostIP"`
	Phase             string              `json:"phase"`
	PodIP             string              `json:"podIP"`
	PodIPs            []map[string]string `json:"podIPs"`
	QosClass          string              `json:"qosClass"`
	StartTime         time.Time           `json:"startTime"`
}

type Pod struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`
	Spec       Spec     `json:"spec"`
	Status     Status   `json:"status"`
}

type PodList struct {
	APIVersion string `json:"apiVersion"`
	Pods       []Pod  `json:"items"`
	Kind       string `json:"kind"`
}
