package e2eutil

import (
	"fmt"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
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
		if !EqualEvent(&c, &other[i]) {
			return false
		}
	}

	return true
}

func EqualEvent(e1, e2 *v1.Event) bool {
	return (e1.Type == e2.Type && e1.Reason == e2.Reason && e1.Message == e2.Message)
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
	event := NewMemberAddEvent(cl, memberId)
	*e = append(*e, *event)
}

func (e *EventList) AddMemberRemoveEvent(cl *api.CouchbaseCluster, memberId int) {
	event := NewMemberRemoveEvent(cl, memberId)
	*e = append(*e, *event)
}

func (e *EventList) AddMemberDownEvent(cl *api.CouchbaseCluster, memberId int) {
	event := NewMemberDownEvent(cl, memberId)
	*e = append(*e, *event)
}

func (e *EventList) AddMemberFailedOverEvent(cl *api.CouchbaseCluster, memberId int) {
	event := NewMemberFailedOverEvent(cl, memberId)
	*e = append(*e, *event)
}

func (e *EventList) AddRebalanceStartedEvent(cl *api.CouchbaseCluster) {
	*e = append(*e, *k8sutil.RebalanceStartedEvent(cl))
}

func (e *EventList) AddRebalanceIncompleteEvent(cl *api.CouchbaseCluster) {
	*e = append(*e, *k8sutil.RebalanceIncompleteEvent(cl))
}

func (e *EventList) AddRebalanceCompletedEvent(cl *api.CouchbaseCluster) {
	*e = append(*e, *k8sutil.RebalanceCompletedEvent(cl))
}

func (e *EventList) AddFailedAddNodeEvent(cl *api.CouchbaseCluster, memberId int) {
	name := couchbaseutil.CreateMemberName(cl.Name, memberId)
	event := k8sutil.FailedAddNodeEvent(name, cl)
	*e = append(*e, *event)
}

func (e *EventList) AddBucketCreateEvent(cl *api.CouchbaseCluster, bucketName string) {
	*e = append(*e, *k8sutil.BucketCreateEvent(bucketName, cl))
}

func (e *EventList) AddBucketDeleteEvent(cl *api.CouchbaseCluster, bucketName string) {
	*e = append(*e, *k8sutil.BucketDeleteEvent(bucketName, cl))
}

func (e *EventList) AddBucketEditEvent(cl *api.CouchbaseCluster, bucketName string) {
	*e = append(*e, *k8sutil.BucketEditEvent(bucketName, cl))
}

func (e *EventList) AddAdminConsoleSvcCreateEvent(cl *api.CouchbaseCluster) {
	*e = append(*e, *k8sutil.AdminConsoleSvcCreateEvent(cl.Name+"-ui", cl))
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

func NewMemberAddEvent(cl *api.CouchbaseCluster, memberId int) *v1.Event {
	name := couchbaseutil.CreateMemberName(cl.Name, memberId)
	return k8sutil.MemberAddEvent(name, cl)
}

func NewMemberRemoveEvent(cl *api.CouchbaseCluster, memberId int) *v1.Event {
	name := couchbaseutil.CreateMemberName(cl.Name, memberId)
	return k8sutil.MemberRemoveEvent(name, cl)
}

func NewMemberDownEvent(cl *api.CouchbaseCluster, memberId int) *v1.Event {
	name := couchbaseutil.CreateMemberName(cl.Name, memberId)
	return k8sutil.MemberDownEvent(name, cl)
}

func NewMemberFailedOverEvent(cl *api.CouchbaseCluster, memberId int) *v1.Event {
	name := couchbaseutil.CreateMemberName(cl.Name, memberId)
	return k8sutil.MemberFailedOverEvent(name, cl)
}
