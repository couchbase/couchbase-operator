package k8sutil

import (
	"fmt"
	"os"
	"time"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	EventReasonMemberCreationFailed  = "MemberCreationFailed"
	EventReasonNewMemberAdded        = "NewMemberAdded"
	EventReasonMemberRemoved         = "MemberRemoved"
	EventReasonMemberDown            = "MemberDown"
	EventReasonMemberRecovered       = "MemberRecovered"
	EventReasonMemberFailedOver      = "MemberFailedOver"
	EventReasonRebalanceStarted      = "RebalanceStarted"
	EventReasonRebalanceIncomplete   = "RebalanceIncomplete"
	EventReasonRebalanceCompleted    = "RebalanceCompleted"
	EventReasonFailedAddNode         = "FailedAddNode"
	EventReasonFailedAddBackNode     = "FailedAddBackNode"
	EventReasonBucketCreated         = "BucketCreated"
	EventReasonBucketDeleted         = "BucketDeleted"
	EventReasonBucketEdited          = "BucketEdited"
	EventReasonServiceCreated        = "ServiceCreated"
	EventReasonServiceDeleted        = "ServiceDeleted"
	EventReasonNodeServiceCreated    = "NodeServiceCreated"
	EventReasonNodeServiceDeleted    = "NodeServiceDeleted"
	EventReasonUpgradeStarted        = "UpgradeStarted"
	EventReasonUpgradeFinished       = "UpgradeFinished"
	EventReasonRollbackStarted       = "RollbackStarted"
	EventReasonRollbackFinished      = "RollbackFinished"
	EventReasonClusterSettingsEdited = "ClusterSettingsEdited"
	EventReasonVolumeUnhealthy       = "VolumeUnhealthy"
)

func MemberCreationFailedEvent(memberName string, cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeWarning
	event.Reason = EventReasonMemberCreationFailed
	event.Message = fmt.Sprintf("New member %s creation failed", memberName)
	return event
}

func MemberAddEvent(memberName string, cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonNewMemberAdded
	event.Message = fmt.Sprintf("New member %s added to cluster", memberName)
	return event
}

func MemberRemoveEvent(memberName string, cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonMemberRemoved
	event.Message = fmt.Sprintf("Existing member %s removed from the cluster", memberName)
	return event
}

func MemberDownEvent(memberName string, cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeWarning
	event.Reason = EventReasonMemberDown
	event.Message = fmt.Sprintf("Existing member %s down", memberName)
	return event
}

func MemberRecoveredEvent(memberName string, cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonMemberRecovered
	event.Message = fmt.Sprintf("Existing member %s recovered", memberName)
	return event
}

func MemberFailedOverEvent(memberName string, cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeWarning
	event.Reason = EventReasonMemberFailedOver
	event.Message = fmt.Sprintf("Existing member %s failed over", memberName)
	return event
}

func RebalanceStartedEvent(cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonRebalanceStarted
	event.Message = fmt.Sprintf("A rebalance has been started to balance data across the cluster")
	return event
}

func RebalanceIncompleteEvent(cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonRebalanceIncomplete
	event.Message = fmt.Sprintf("A rebalance is incomplete")
	return event
}

func RebalanceCompletedEvent(cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonRebalanceCompleted
	event.Message = fmt.Sprintf("A rebalance has completed")
	return event
}

func FailedAddNodeEvent(memberName string, cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonFailedAddNode
	event.Message = fmt.Sprintf("Removed existing member %s because it failed before it could be added to the cluster", memberName)
	return event
}

func FailedAddBackNodeEvent(memberName string, cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonFailedAddBackNode
	event.Message = fmt.Sprintf("Removed existing member %s because it could not be added back to the cluster", memberName)
	return event
}

func BucketCreateEvent(bucketName string, cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonBucketCreated
	event.Message = fmt.Sprintf("A new bucket `%s` was created", bucketName)
	return event
}

func BucketDeleteEvent(bucketName string, cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonBucketDeleted
	event.Message = fmt.Sprintf("Bucket `%s` was deleted", bucketName)
	return event
}

func BucketEditEvent(bucketName string, cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonBucketEdited
	event.Message = fmt.Sprintf("Bucket `%s` was edited", bucketName)
	return event
}

func AdminConsoleSvcCreateEvent(svcName string, cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonServiceCreated
	event.Message = fmt.Sprintf("Service for admin console `%s` was created", svcName)
	return event
}

func AdminConsoleSvcDeleteEvent(svcName string, cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonServiceDeleted
	event.Message = fmt.Sprintf("Service for admin console `%s` was deleted", svcName)
	return event
}

func NodeServiceCreateEvent(service api.Service, cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonNodeServiceCreated
	event.Message = fmt.Sprintf("Node service for %s was created", service.String())
	return event
}

func NodeServiceDeleteEvent(service api.Service, cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonNodeServiceDeleted
	event.Message = fmt.Sprintf("Node service for %s was deleted", service.String())
	return event
}

func UpgradeStartedEvent(sourceVersion, targetVersion string, cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonUpgradeStarted
	event.Message = fmt.Sprintf("Started upgrade from %s to %s", sourceVersion, targetVersion)
	return event
}

func UpgradeFinishedEvent(sourceVersion, targetVersion string, cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonUpgradeFinished
	event.Message = fmt.Sprintf("Finished upgrade from %s to %s", sourceVersion, targetVersion)
	return event
}

func RollbackStartedEvent(sourceVersion, targetVersion string, cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonRollbackStarted
	event.Message = fmt.Sprintf("Started rollback from %s to %s", sourceVersion, targetVersion)
	return event
}

func RollbackFinishedEvent(sourceVersion, targetVersion string, cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonRollbackFinished
	event.Message = fmt.Sprintf("Finished rollback from %s to %s", sourceVersion, targetVersion)
	return event
}

func ClusterSettingsEditedEvent(settingName string, cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonClusterSettingsEdited
	event.Message = fmt.Sprintf("Setting for `%s` was edited", settingName)
	return event
}

func MemberVolumeUnhealthyEvent(memberName string, reason string, cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonVolumeUnhealthy
	event.Message = fmt.Sprintf("Member %s volumes are unhealthy.  Failover is recommended: %s", memberName, reason)
	return event
}

func newClusterEvent(cl *api.CouchbaseCluster) *v1.Event {
	t := time.Now()
	return &v1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: cl.Name + "-",
			Namespace:    cl.Namespace,
		},
		InvolvedObject: v1.ObjectReference{
			APIVersion:      api.SchemeGroupVersion.String(),
			Kind:            api.CRDResourceKind,
			Name:            cl.Name,
			Namespace:       cl.Namespace,
			UID:             cl.UID,
			ResourceVersion: cl.ResourceVersion,
		},
		Source: v1.EventSource{
			Component: os.Getenv(constants.EnvOperatorPodName),
		},
		// Each cluster event is unique so it should not be collapsed with other events
		FirstTimestamp: metav1.Time{Time: t},
		LastTimestamp:  metav1.Time{Time: t},
		Count:          int32(1),
	}
}
