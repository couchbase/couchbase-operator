package e2eutil

import (
	"fmt"

	api "github.com/couchbaselabs/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbaselabs/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbaselabs/couchbase-operator/pkg/util/k8sutil"

	"k8s.io/api/core/v1"
)

type EventList []v1.Event

// An event is considered equal if the type, reason, and message for each event
// in the list are the same.
func (e EventList) Compare(other EventList) bool {
	if len(e) != len(other) {
		return false
	}

	for i, c := range e {
		if c.Type != other[i].Type || c.Reason != other[i].Reason || c.Message != other[i].Message {
			return false
		}
	}

	return true
}

func (e *EventList) AddEvent(add v1.Event) {
	for i, c := range *e {
		if c.FirstTimestamp.After(add.FirstTimestamp.Time) {
			*e = append((*e)[:i], append([]v1.Event{add}, (*e)[i:]...)...)
			return
		}
	}

	*e = append(*e, add)
}

func (e *EventList) AddMemberAddEvent(cl *api.CouchbaseCluster, memberId int) {
	name := couchbaseutil.CreateMemberName(cl.Name, memberId)
	event := k8sutil.MemberAddEvent(name, cl)
	*e = append(*e, *event)
}

func (e *EventList) AddMemberRemoveEvent(cl *api.CouchbaseCluster, memberId int) {
	name := couchbaseutil.CreateMemberName(cl.Name, memberId)
	event := k8sutil.MemberRemoveEvent(name, cl)
	*e = append(*e, *event)
}

func (e *EventList) AddRebalanceEvent(cl *api.CouchbaseCluster) {
	*e = append(*e, *k8sutil.RebalanceEvent(cl))
}

func (e *EventList) AddFailedAddNodeEvent(cl *api.CouchbaseCluster, memberId int) {
	name := couchbaseutil.CreateMemberName(cl.Name, memberId)
	event := k8sutil.FailedAddNodeEvent(name, cl)
	*e = append(*e, *event)
}

func (e *EventList) AddBucketCreateEvent(cl *api.CouchbaseCluster, name string) {
	*e = append(*e, *k8sutil.BucketCreateEvent(name, cl))
}

func (e *EventList) AddBucketDeleteEvent(cl *api.CouchbaseCluster, name string) {
	*e = append(*e, *k8sutil.BucketDeleteEvent(name, cl))
}

func (e *EventList) AddBucketEditEvent(cl *api.CouchbaseCluster, name string) {
	*e = append(*e, *k8sutil.BucketEditEvent(name, cl))
}

func (e EventList) String() string {
	s := ""
	for _, c := range e {
		s += fmt.Sprintf("Type: %s | Reason: %s | Message: %s\n", c.Type, c.Reason, c.Message)
	}

	return s
}

func EventListCompareFailedString(expected, actual EventList) string {
	return fmt.Sprintf("Expected events to be:\n%s\nbut got:\n%s", expected, actual)
}
