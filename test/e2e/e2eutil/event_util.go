package e2eutil

import (
	"fmt"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
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

func EventExistsInEventList(event *v1.Event, eventList EventList) bool {
	for _, temEvent := range eventList {
		if EqualEvent(event, &temEvent) {
			return true
		}
	}
	return false
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

func (e *EventList) AppendEventList(eventsList EventList) {
	for _, event := range eventsList {
		*e = append(*e, event)
	}
}

func (e *EventList) AddMemberCreationFailedEvent(cl *api.CouchbaseCluster, memberId int) {
	event := NewMemberCreationFailedEvent(cl, memberId)
	*e = append(*e, *event)
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

func (e *EventList) NodeServiceCreateEvent(cl *api.CouchbaseCluster, serviceName api.Service) {
	*e = append(*e, *k8sutil.NodeServiceCreateEvent(serviceName, cl))
}

func (e *EventList) AddClusterSettingsEditedEvent(cl *api.CouchbaseCluster, settingName string) {
	*e = append(*e, *k8sutil.ClusterSettingsEditedEvent(settingName, cl))
}

func (e *EventList) AddMemberRecoveredEvent(cl *api.CouchbaseCluster, memberId int) {
	name := couchbaseutil.CreateMemberName(cl.Name, memberId)
	*e = append(*e, *k8sutil.MemberRecoveredEvent(name, cl))
}

func (e *EventList) AddNodeServiceCreateEvent(cl *api.CouchbaseCluster, serviceName api.Service) {
	*e = append(*e, *k8sutil.NodeServiceCreateEvent(serviceName, cl))
}

func (e *EventList) AddMemberVolumeUnhealthyEvent(cl *api.CouchbaseCluster, memberId int, reason string) {
	name := couchbaseutil.CreateMemberName(cl.Name, memberId)
	*e = append(*e, *k8sutil.MemberVolumeUnhealthyEvent(name, reason, cl))
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

func NewMemberCreationFailedEvent(cl *api.CouchbaseCluster, memberId int) *v1.Event {
	name := couchbaseutil.CreateMemberName(cl.Name, memberId)
	return k8sutil.MemberCreationFailedEvent(name, cl)
}

func NewMemberAddEvent(cl *api.CouchbaseCluster, memberId int) *v1.Event {
	name := couchbaseutil.CreateMemberName(cl.Name, memberId)
	return k8sutil.MemberAddEvent(name, cl)
}

func NewMemberRemoveEvent(cl *api.CouchbaseCluster, memberId int) *v1.Event {
	name := couchbaseutil.CreateMemberName(cl.Name, memberId)
	return k8sutil.MemberRemoveEvent(name, cl)
}

func FailedAddNodeEvent(cl *api.CouchbaseCluster, memberId int) *v1.Event {
	name := couchbaseutil.CreateMemberName(cl.Name, memberId)
	return k8sutil.FailedAddNodeEvent(name, cl)
}

func NewMemberDownEvent(cl *api.CouchbaseCluster, memberId int) *v1.Event {
	name := couchbaseutil.CreateMemberName(cl.Name, memberId)
	return k8sutil.MemberDownEvent(name, cl)
}

func NewMemberFailedOverEvent(cl *api.CouchbaseCluster, memberId int) *v1.Event {
	name := couchbaseutil.CreateMemberName(cl.Name, memberId)
	return k8sutil.MemberFailedOverEvent(name, cl)
}

func RebalanceStartedEvent(cl *api.CouchbaseCluster) *v1.Event {
	return k8sutil.RebalanceStartedEvent(cl)
}

func RebalanceCompletedEvent(cl *api.CouchbaseCluster) *v1.Event {
	return k8sutil.RebalanceCompletedEvent(cl)
}

func RebalanceIncompleteEvent(cl *api.CouchbaseCluster) *v1.Event {
	return k8sutil.RebalanceIncompleteEvent(cl)
}

func MemberRecoveredEvent(cl *api.CouchbaseCluster, memberId int) *v1.Event {
	name := couchbaseutil.CreateMemberName(cl.Name, memberId)
	return k8sutil.MemberRecoveredEvent(name, cl)
}

// New Event schema code
type EventSequence eventschema.Sequence

func EventValidator() *EventSequence {
	return &EventSequence{
		Validators: []eventschema.Validatable{},
	}
}

func (eventsequence *EventSequence) AddClusterEvent(cbCluster *api.CouchbaseCluster, eventType string) {
	var eventToAppend v1.Event
	switch eventType {
	case "AdminConsoleServiceCreate":
		eventToAppend = *k8sutil.AdminConsoleSvcCreateEvent(cbCluster.Name+"-ui", cbCluster)
	case "RebalanceStarted":
		eventToAppend = *k8sutil.RebalanceStartedEvent(cbCluster)
	case "RebalanceCompleted":
		eventToAppend = *k8sutil.RebalanceCompletedEvent(cbCluster)
	case "RebalanceIncomplete":
		eventToAppend = *k8sutil.RebalanceIncompleteEvent(cbCluster)
	}
	eventsequence.Validators = append(eventsequence.Validators, eventschema.CreateEventFrom(eventToAppend))
}

func (eventsequence *EventSequence) AddClusterNodeServiceEvent(cbCluster *api.CouchbaseCluster, eventType string, serviceName api.Service) {
	var eventToAppend v1.Event
	switch eventType {
	case "Create":
		eventToAppend = *k8sutil.NodeServiceCreateEvent(serviceName, cbCluster)
	}
	eventsequence.Validators = append(eventsequence.Validators, eventschema.CreateEventFrom(eventToAppend))
}

func (eventsequence *EventSequence) AddClusterPodEvent(cbCluster *api.CouchbaseCluster, eventType string, cbMemberIdList ...int) {
	switch eventType {
	case "AddNewMember":
		for _, cbMemberId := range cbMemberIdList {
			eventToAppend := NewMemberAddEvent(cbCluster, cbMemberId)
			eventsequence.Validators = append(eventsequence.Validators, eventschema.CreateEventFrom(*eventToAppend))
		}
	case "MemberDown":
		for _, cbMemberId := range cbMemberIdList {
			eventToAppend := NewMemberDownEvent(cbCluster, cbMemberId)
			eventsequence.Validators = append(eventsequence.Validators, eventschema.CreateEventFrom(*eventToAppend))
		}
	case "MemberRemoved":
		for _, cbMemberId := range cbMemberIdList {
			eventToAppend := NewMemberRemoveEvent(cbCluster, cbMemberId)
			eventsequence.Validators = append(eventsequence.Validators, eventschema.CreateEventFrom(*eventToAppend))
		}
	case "MemberRecovered":
		for _, cbMemberId := range cbMemberIdList {
			eventToAppend := MemberRecoveredEvent(cbCluster, cbMemberId)
			eventsequence.Validators = append(eventsequence.Validators, eventschema.CreateEventFrom(*eventToAppend))
		}
	case "CreationFailed":
		for _, cbMemberId := range cbMemberIdList {
			eventToAppend := NewMemberCreationFailedEvent(cbCluster, cbMemberId)
			eventsequence.Validators = append(eventsequence.Validators, eventschema.CreateEventFrom(*eventToAppend))
		}
	case "FailedOver":
		for _, cbMemberId := range cbMemberIdList {
			eventToAppend := NewMemberFailedOverEvent(cbCluster, cbMemberId)
			eventsequence.Validators = append(eventsequence.Validators, eventschema.CreateEventFrom(*eventToAppend))
		}
	case "FailedAddNode":
		for _, cbMemberId := range cbMemberIdList {
			eventToAppend := FailedAddNodeEvent(cbCluster, cbMemberId)
			eventsequence.Validators = append(eventsequence.Validators, eventschema.CreateEventFrom(*eventToAppend))
		}
	}
}

func (eventsequence *EventSequence) AddClusterBucketEvent(cbCluster *api.CouchbaseCluster, eventType string, bucketNameList ...string) {
	switch eventType {
	case "Create":
		for _, bucketName := range bucketNameList {
			eventToAppend := *k8sutil.BucketCreateEvent(bucketName, cbCluster)
			eventsequence.Validators = append(eventsequence.Validators, eventschema.CreateEventFrom(eventToAppend))
		}
	case "Delete":
		for _, bucketName := range bucketNameList {
			eventToAppend := *k8sutil.BucketDeleteEvent(bucketName, cbCluster)
			eventsequence.Validators = append(eventsequence.Validators, eventschema.CreateEventFrom(eventToAppend))
		}
	case "Edit":
		for _, bucketName := range bucketNameList {
			eventToAppend := *k8sutil.BucketEditEvent(bucketName, cbCluster)
			eventsequence.Validators = append(eventsequence.Validators, eventschema.CreateEventFrom(eventToAppend))
		}
	}
}

func (eventsequence *EventSequence) AddParallelEvents(eventList EventList) {
	// Generate event set from the given event list
	eventSet := eventschema.Set{
		Validators: []eventschema.Validatable{},
	}
	for _, event := range eventList {
		eventSet.Validators = append(eventSet.Validators, eventschema.CreateEventFrom(event))
	}

	// Append the generated event set in to event sequence
	eventsequence.Validators = append(eventsequence.Validators, eventSet)
}

func (eventsequence *EventSequence) AddClusterSettingsEditedEvent(cbCluster *api.CouchbaseCluster, settingName string) {
	eventToAppend := *k8sutil.ClusterSettingsEditedEvent(settingName, cbCluster)
	eventsequence.Validators = append(eventsequence.Validators, eventschema.CreateEventFrom(eventToAppend))
}

func (eventsequence *EventSequence) AddMemberVolumeUnhealthyEvent(cbCluster *api.CouchbaseCluster, memberId int, reason string) {
	cbMemberName := couchbaseutil.CreateMemberName(cbCluster.Name, memberId)
	eventToAppend := *k8sutil.MemberVolumeUnhealthyEvent(cbMemberName, reason, cbCluster)
	eventsequence.Validators = append(eventsequence.Validators, eventschema.CreateEventFrom(eventToAppend))
}
