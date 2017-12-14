package e2eutil

import (
	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func GetCouchbaseCluster(crClient versioned.Interface, name, namespace string) (*api.CouchbaseCluster, error) {
	return crClient.Couchbase().CouchbaseClusters(namespace).Get(name, metav1.GetOptions{})
}

// Gets events for a CouchbaseCluster and returns them sorted by time (oldest to newest)
func GetCouchbaseEvents(kubeCli kubernetes.Interface, name, namespace string) (EventList, error) {
	list, err := kubeCli.Core().Events(namespace).List(metav1.ListOptions{FieldSelector: "involvedObject.name=" + name})
	if err != nil {
		return nil, err
	}

	events := EventList{}
	for _, item := range list.Items {
		events.AddEvent(item)
	}

	return events, nil
}
