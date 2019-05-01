package e2eutil

import (
	"fmt"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
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
	*e = append(*e, eventsList...)
}

func (e *EventList) AddMemberCreationFailedEvent(cl *couchbasev2.CouchbaseCluster, memberId int) {
	event := NewMemberCreationFailedEvent(cl, memberId)
	*e = append(*e, *event)
}

func (e *EventList) AddMemberAddEvent(cl *couchbasev2.CouchbaseCluster, memberId int) {
	event := NewMemberAddEvent(cl, memberId)
	*e = append(*e, *event)
}

func (e *EventList) AddMemberRemoveEvent(cl *couchbasev2.CouchbaseCluster, memberId int) {
	event := NewMemberRemoveEvent(cl, memberId)
	*e = append(*e, *event)
}

func (e *EventList) AddMemberDownEvent(cl *couchbasev2.CouchbaseCluster, memberId int) {
	event := NewMemberDownEvent(cl, memberId)
	*e = append(*e, *event)
}

func (e *EventList) AddMemberFailedOverEvent(cl *couchbasev2.CouchbaseCluster, memberId int) {
	event := NewMemberFailedOverEvent(cl, memberId)
	*e = append(*e, *event)
}

func (e *EventList) AddRebalanceStartedEvent(cl *couchbasev2.CouchbaseCluster) {
	*e = append(*e, *k8sutil.RebalanceStartedEvent(cl))
}

func (e *EventList) AddRebalanceIncompleteEvent(cl *couchbasev2.CouchbaseCluster) {
	*e = append(*e, *k8sutil.RebalanceIncompleteEvent(cl))
}

func (e *EventList) AddRebalanceCompletedEvent(cl *couchbasev2.CouchbaseCluster) {
	*e = append(*e, *k8sutil.RebalanceCompletedEvent(cl))
}

func (e *EventList) AddFailedAddNodeEvent(cl *couchbasev2.CouchbaseCluster, memberId int) {
	name := couchbaseutil.CreateMemberName(cl.Name, memberId)
	event := k8sutil.FailedAddNodeEvent(name, cl)
	*e = append(*e, *event)
}

func (e *EventList) AddBucketCreateEvent(cl *couchbasev2.CouchbaseCluster, bucketName string) {
	*e = append(*e, *k8sutil.BucketCreateEvent(bucketName, cl))
}

func (e *EventList) AddBucketDeleteEvent(cl *couchbasev2.CouchbaseCluster, bucketName string) {
	*e = append(*e, *k8sutil.BucketDeleteEvent(bucketName, cl))
}

func (e *EventList) AddBucketEditEvent(cl *couchbasev2.CouchbaseCluster, bucketName string) {
	*e = append(*e, *k8sutil.BucketEditEvent(bucketName, cl))
}

func (e *EventList) AddAdminConsoleSvcCreateEvent(cl *couchbasev2.CouchbaseCluster) {
	*e = append(*e, *k8sutil.AdminConsoleSvcCreateEvent(cl.Name+"-ui", cl))
}

func (e *EventList) NodeServiceCreateEvent(cl *couchbasev2.CouchbaseCluster, serviceName couchbasev2.Service) {
	*e = append(*e, *k8sutil.NodeServiceCreateEvent(serviceName, cl))
}

func (e *EventList) AddClusterSettingsEditedEvent(cl *couchbasev2.CouchbaseCluster, settingName string) {
	*e = append(*e, *k8sutil.ClusterSettingsEditedEvent(settingName, cl))
}

func (e *EventList) AddMemberRecoveredEvent(cl *couchbasev2.CouchbaseCluster, memberId int) {
	name := couchbaseutil.CreateMemberName(cl.Name, memberId)
	*e = append(*e, *k8sutil.MemberRecoveredEvent(name, cl))
}

func (e *EventList) AddNodeServiceCreateEvent(cl *couchbasev2.CouchbaseCluster, serviceName couchbasev2.Service) {
	*e = append(*e, *k8sutil.NodeServiceCreateEvent(serviceName, cl))
}

func (e *EventList) AddMemberVolumeUnhealthyEvent(cl *couchbasev2.CouchbaseCluster, memberId int, reason string) {
	name := couchbaseutil.CreateMemberName(cl.Name, memberId)
	*e = append(*e, *k8sutil.MemberVolumeUnhealthyEvent(name, reason, cl))
}

func (e *EventList) AddTLSInvalidEvent(cl *couchbasev2.CouchbaseCluster) {
	*e = append(*e, *TLSInvalidEvent(cl))
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

func NewMemberCreationFailedEvent(cl *couchbasev2.CouchbaseCluster, memberId int) *v1.Event {
	name := couchbaseutil.CreateMemberName(cl.Name, memberId)
	return k8sutil.MemberCreationFailedEvent(name, cl)
}

func NewMemberAddEvent(cl *couchbasev2.CouchbaseCluster, memberId int) *v1.Event {
	name := couchbaseutil.CreateMemberName(cl.Name, memberId)
	return k8sutil.MemberAddEvent(name, cl)
}

func NewMemberRemoveEvent(cl *couchbasev2.CouchbaseCluster, memberId int) *v1.Event {
	name := couchbaseutil.CreateMemberName(cl.Name, memberId)
	return k8sutil.MemberRemoveEvent(name, cl)
}

func FailedAddNodeEvent(cl *couchbasev2.CouchbaseCluster, memberId int) *v1.Event {
	name := couchbaseutil.CreateMemberName(cl.Name, memberId)
	return k8sutil.FailedAddNodeEvent(name, cl)
}

func NewMemberDownEvent(cl *couchbasev2.CouchbaseCluster, memberId int) *v1.Event {
	name := couchbaseutil.CreateMemberName(cl.Name, memberId)
	return k8sutil.MemberDownEvent(name, cl)
}

func NewMemberFailedOverEvent(cl *couchbasev2.CouchbaseCluster, memberId int) *v1.Event {
	name := couchbaseutil.CreateMemberName(cl.Name, memberId)
	return k8sutil.MemberFailedOverEvent(name, cl)
}

func RebalanceStartedEvent(cl *couchbasev2.CouchbaseCluster) *v1.Event {
	return k8sutil.RebalanceStartedEvent(cl)
}

func RebalanceCompletedEvent(cl *couchbasev2.CouchbaseCluster) *v1.Event {
	return k8sutil.RebalanceCompletedEvent(cl)
}

func RebalanceIncompleteEvent(cl *couchbasev2.CouchbaseCluster) *v1.Event {
	return k8sutil.RebalanceIncompleteEvent(cl)
}

func MemberRecoveredEvent(cl *couchbasev2.CouchbaseCluster, memberId int) *v1.Event {
	name := couchbaseutil.CreateMemberName(cl.Name, memberId)
	return k8sutil.MemberRecoveredEvent(name, cl)
}

func TLSUpdatedEvent(cl *couchbasev2.CouchbaseCluster) *v1.Event {
	return k8sutil.TLSUpdatedEvent(cl)
}

func TLSInvalidEvent(cl *couchbasev2.CouchbaseCluster) *v1.Event {
	return k8sutil.TLSInvalidEvent(cl)
}

func TLSUpdateFailedEvent(cl *couchbasev2.CouchbaseCluster) *v1.Event {
	return k8sutil.TLSUpdateFailedEvent(cl)
}

// New Event schema code
type EventValidator []eventschema.Validatable

func createEventFrom(event v1.Event) eventschema.Event {
	return eventschema.Event{
		Reason:  event.Reason,
		Message: event.Message,
	}
}

func (eventsequence *EventValidator) AddClusterEvent(cbCluster *couchbasev2.CouchbaseCluster, eventType string) {
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
	*eventsequence = append(*eventsequence, createEventFrom(eventToAppend))
}

func (eventsequence *EventValidator) AddClusterNodeServiceEvent(cbCluster *couchbasev2.CouchbaseCluster, eventType string, serviceList ...couchbasev2.Service) {
	switch eventType {
	case "Create":
		for _, service := range serviceList {
			eventToAppend := *k8sutil.NodeServiceCreateEvent(service, cbCluster)
			*eventsequence = append(*eventsequence, createEventFrom(eventToAppend))
		}
	}
}

func (eventsequence *EventValidator) AddClusterPodEvent(cbCluster *couchbasev2.CouchbaseCluster, eventType string, cbMemberIdList ...int) {
	switch eventType {
	case "AddNewMember":
		for _, cbMemberId := range cbMemberIdList {
			eventToAppend := NewMemberAddEvent(cbCluster, cbMemberId)
			*eventsequence = append(*eventsequence, createEventFrom(*eventToAppend))
		}
	case "MemberDown":
		for _, cbMemberId := range cbMemberIdList {
			eventToAppend := NewMemberDownEvent(cbCluster, cbMemberId)
			*eventsequence = append(*eventsequence, createEventFrom(*eventToAppend))
		}
	case "MemberRemoved":
		for _, cbMemberId := range cbMemberIdList {
			eventToAppend := NewMemberRemoveEvent(cbCluster, cbMemberId)
			*eventsequence = append(*eventsequence, createEventFrom(*eventToAppend))
		}
	case "MemberRecovered":
		for _, cbMemberId := range cbMemberIdList {
			eventToAppend := MemberRecoveredEvent(cbCluster, cbMemberId)
			*eventsequence = append(*eventsequence, createEventFrom(*eventToAppend))
		}
	case "CreationFailed":
		for _, cbMemberId := range cbMemberIdList {
			eventToAppend := NewMemberCreationFailedEvent(cbCluster, cbMemberId)
			*eventsequence = append(*eventsequence, createEventFrom(*eventToAppend))
		}
	case "FailedOver":
		for _, cbMemberId := range cbMemberIdList {
			eventToAppend := NewMemberFailedOverEvent(cbCluster, cbMemberId)
			*eventsequence = append(*eventsequence, createEventFrom(*eventToAppend))
		}
	case "FailedAddNode":
		for _, cbMemberId := range cbMemberIdList {
			eventToAppend := FailedAddNodeEvent(cbCluster, cbMemberId)
			*eventsequence = append(*eventsequence, createEventFrom(*eventToAppend))
		}
	}
}

func (eventsequence *EventValidator) AddOptionalClusterPodEvent(cbCluster *couchbasev2.CouchbaseCluster, eventType string, cbMemberId int) {
	switch eventType {
	case "MemberDown":
		eventToAppend := NewMemberDownEvent(cbCluster, cbMemberId)
		*eventsequence = append(*eventsequence, &eventschema.Optional{Validator: createEventFrom(*eventToAppend)})
	}
}

func (eventsequence *EventValidator) AddClusterBucketEvent(cbCluster *couchbasev2.CouchbaseCluster, eventType string, bucketNameList ...string) {
	switch eventType {
	case "Create":
		for _, bucketName := range bucketNameList {
			eventToAppend := *k8sutil.BucketCreateEvent(bucketName, cbCluster)
			*eventsequence = append(*eventsequence, createEventFrom(eventToAppend))
		}
	case "Delete":
		for _, bucketName := range bucketNameList {
			eventToAppend := *k8sutil.BucketDeleteEvent(bucketName, cbCluster)
			*eventsequence = append(*eventsequence, createEventFrom(eventToAppend))
		}
	case "Edit":
		for _, bucketName := range bucketNameList {
			eventToAppend := *k8sutil.BucketEditEvent(bucketName, cbCluster)
			*eventsequence = append(*eventsequence, createEventFrom(eventToAppend))
		}
	}
}

func (eventsequence *EventValidator) AddParallelEvents(eventList EventValidator) {
	// Generate event set from the given event list
	eventSet := eventschema.Set{
		Validators: eventList,
	}

	// Append the generated event set in to event sequence
	*eventsequence = append(*eventsequence, eventSet)
}

func (eventsequence *EventValidator) AddAnyOfEvents(eventList EventValidator) {
	anyOfEvents := eventschema.AnyOf{
		Validators: eventList,
	}

	// Append the generated event set in to event sequence
	*eventsequence = append(*eventsequence, anyOfEvents)
}

func (eventsequence *EventValidator) AddClusterSettingsEditedEvent(cbCluster *couchbasev2.CouchbaseCluster, settingName string) {
	eventToAppend := *k8sutil.ClusterSettingsEditedEvent(settingName, cbCluster)
	*eventsequence = append(*eventsequence, createEventFrom(eventToAppend))
}

func (eventsequence *EventValidator) AddMemberVolumeUnhealthyEvent(cbCluster *couchbasev2.CouchbaseCluster, memberId int, reason string) {
	cbMemberName := couchbaseutil.CreateMemberName(cbCluster.Name, memberId)
	eventToAppend := *k8sutil.MemberVolumeUnhealthyEvent(cbMemberName, reason, cbCluster)
	*eventsequence = append(*eventsequence, createEventFrom(eventToAppend))
}

// ClusterCreateSequence is a common function for generating cluster creation events.
func ClusterCreateSequence(size int) eventschema.Validatable {
	if size == 1 {
		return eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded}
	}

	return eventschema.Sequence{
		Validators: []eventschema.Validatable{
			eventschema.Repeat{
				Times:     size,
				Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
			},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
		},
	}
}

// ClusterScaleUpSequence is a common function for generating cluster scaling up events.
func ClusterScaleUpSequence(size int) eventschema.Validatable {
	return eventschema.Sequence{
		Validators: []eventschema.Validatable{
			eventschema.Repeat{
				Times:     size,
				Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
			},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
		},
	}
}

// ClusterScaleDownSequence is a common function for generating cluster scaling down events.
func ClusterScaleDownSequence(size int) eventschema.Validatable {
	return eventschema.Sequence{
		Validators: []eventschema.Validatable{
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			eventschema.Repeat{
				Times:     size,
				Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved},
			},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
		},
	}
}

// PodDownFailoverRecoverySequence is a common function for generating down/failover/recovery events.
func PodDownFailoverRecoverySequence() eventschema.Validatable {
	return eventschema.Sequence{
		Validators: []eventschema.Validatable{
			eventschema.Event{Reason: k8sutil.EventReasonMemberDown},
			eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver},
			eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
		},
	}
}
