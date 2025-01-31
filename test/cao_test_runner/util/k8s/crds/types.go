package crds

import (
	"time"
)

type Metadata struct {
	Annotations       map[string]interface{} `json:"annotations"`
	CreationTimestamp time.Time              `json:"creationTimestamp"`
	Labels            map[string]string      `json:"labels"`
	Name              string                 `json:"name"`
	ResourceVersion   string                 `json:"resourceVersion"`
	UID               string                 `json:"uid"`
	Generation        int                    `json:"generation"`
}

type Version struct {
	AdditionalPrinterColumns interface{} `json:"additionalPrinterColumns"`
	Name                     string      `json:"name"`
	Schema                   interface{} `json:"schema"`
	Served                   bool        `json:"served"`
	Storage                  bool        `json:"storage"`
}

type Spec struct {
	Conversion map[string]string      `json:"conversion"`
	Group      string                 `json:"group"`
	Names      map[string]interface{} `json:"names"`
	Scope      string                 `json:"scope"`
	Version    []Version              `json:"versions"`
}

type Status struct {
	AcceptedNames  map[string]interface{} `json:"acceptedNames"`
	Conditions     interface{}            `json:"conditions"`
	StoredVersions []string               `json:"storedVersions"`
}

type CRD struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`
	Spec       Spec     `json:"spec"`
	Status     Status   `json:"status"`
}

type CRDList struct {
	APIVersion string `json:"apiVersion"`
	CRDs       []*CRD `json:"items"`
	Kind       string `json:"kind"`
}
