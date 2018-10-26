// clustercapabilities collects information daynamically from the current cluster
package clustercapabilities

import (
	"fmt"
	"testing"

	"github.com/couchbase/couchbase-operator/test/e2e/constants"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ZoneList is a list of availability zones.
type ZoneList []string

// zoneSet is a set of unique zones.
type zoneSet map[string]interface{}

// add adds a zone to the set.
func (z zoneSet) add(zone string) {
	z[zone] = nil
}

// toZoneList turns a set into a list.
func (z zoneSet) toZoneList() ZoneList {
	l := ZoneList{}
	for zone, _ := range z {
		l = append(l, zone)
	}
	return l
}

// Capabilities is a description of a Kubernetes cluster.
type Capabilities struct {
	// ZonesSet tells us whether MasterZones and AvailabilityZones are valid
	ZonesSet bool
	// MasterZones is a list of zones containing master nodes
	MasterZones ZoneList
	// AvailabilityZones is a list of all availability zones
	AvailabilityZones ZoneList
}

// NewCapabilities returns a new populated Capabilities struct.
func NewCapabilities(client kubernetes.Interface) (*Capabilities, error) {
	// List all nodes in the cluster (requires admin privileges on OpenShift)
	nodes, err := client.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	// Check there are some nodes to look at
	if len(nodes.Items) == 0 {
		return nil, fmt.Errorf("cluster returned no nodes")
	}

	// Is the first node labelled as having a failure domain?
	if _, ok := nodes.Items[0].Labels[constants.FailureDomainZoneLabel]; !ok {
		return &Capabilities{}, nil
	}

	// Collect availability zones and zones containing masters
	masterZones := zoneSet{}
	availabilityZones := zoneSet{}
	for _, node := range nodes.Items {
		// All nodes must have a zone
		zone, ok := node.Labels[constants.FailureDomainZoneLabel]
		if !ok {
			return nil, fmt.Errorf("node %s missing label %s", node.Name, constants.FailureDomainZoneLabel)
		}
		availabilityZones.add(zone)

		// Check if this node is a master
		if _, ok := node.Labels[constants.NodeRoleMasterLabel]; ok {
			masterZones.add(zone)
		}
	}

	// Populate the cluster object
	cluster := &Capabilities{
		ZonesSet:          true,
		MasterZones:       masterZones.toZoneList(),
		AvailabilityZones: availabilityZones.toZoneList(),
	}
	return cluster, nil
}

// MustNewCapabilities returns a new Capabilities or dies on error
func MustNewCapabilities(t *testing.T, client kubernetes.Interface) *Capabilities {
	cluster, err := NewCapabilities(client)
	if err != nil {
		t.Fatal(err)
	}
	return cluster
}
