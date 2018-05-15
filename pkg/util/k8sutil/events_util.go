package k8sutil

import (
	"fmt"
	"os"
	"time"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func MemberCreationFailedEvent(memberName string, cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeWarning
	event.Reason = "MemberCreationFailed"
	event.Message = fmt.Sprintf("New member %s creation failed", memberName)
	return event
}

func MemberAddEvent(memberName string, cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = "NewMemberAdded"
	event.Message = fmt.Sprintf("New member %s added to cluster", memberName)
	return event
}

func MemberRemoveEvent(memberName string, cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = "MemberRemoved"
	event.Message = fmt.Sprintf("Existing member %s removed from the cluster", memberName)
	return event
}

func MemberDownEvent(memberName string, cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeWarning
	event.Reason = "MemberDown"
	event.Message = fmt.Sprintf("Existing member %s down", memberName)
	return event
}

func MemberFailedOverEvent(memberName string, cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeWarning
	event.Reason = "MemberFailedOver"
	event.Message = fmt.Sprintf("Existing member %s failed over", memberName)
	return event
}

func RebalanceStartedEvent(cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = "RebalanceStarted"
	event.Message = fmt.Sprintf("A rebalance has been started to balance data across the cluster")
	return event
}

func RebalanceIncompleteEvent(cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = "RebalanceIncomplete"
	event.Message = fmt.Sprintf("A rebalance is incomplete")
	return event
}

func RebalanceCompletedEvent(cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = "RebalanceCompleted"
	event.Message = fmt.Sprintf("A rebalance has completed")
	return event
}

func FailedAddNodeEvent(memberName string, cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = "FailedAddNode"
	event.Message = fmt.Sprintf("Removed existing member %s because it failed before it could be added to the cluster", memberName)
	return event
}

func BucketCreateEvent(bucketName string, cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = "BucketCreated"
	event.Message = fmt.Sprintf("A new bucket `%s` was created", bucketName)
	return event
}

func BucketDeleteEvent(bucketName string, cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = "BucketDeleted"
	event.Message = fmt.Sprintf("Bucket `%s` was deleted", bucketName)
	return event
}

func BucketEditEvent(bucketName string, cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = "BucketEdited"
	event.Message = fmt.Sprintf("Bucket `%s` was edited", bucketName)
	return event
}

func AdminConsoleSvcCreateEvent(svcName string, cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = "ServiceCreated"
	event.Message = fmt.Sprintf("Service for admin console `%s` was created", svcName)
	return event
}

func AdminConsoleSvcDeleteEvent(svcName string, cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = "ServiceDeleted"
	event.Message = fmt.Sprintf("Service for admin console `%s` was deleted", svcName)
	return event
}

func NodeServiceCreateEvent(service string, cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = "NodeServiceCreated"
	event.Message = fmt.Sprintf("Node service for %s was created", service)
	return event
}

func NodeServiceDeleteEvent(service string, cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = "NodeServiceDeleted"
	event.Message = fmt.Sprintf("Node service for %s was deleted", service)
	return event
}

func UpgradeStartedEvent(sourceVersion, targetVersion string, cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = "UpgradeStarted"
	event.Message = fmt.Sprintf("Started upgrade from '%s' to '%s'", sourceVersion, targetVersion)
	return event
}

func UpgradeFinishedEvent(sourceVersion, targetVersion string, cl *api.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = "UpgradeFinished"
	event.Message = fmt.Sprintf("Finished upgrade from '%s' to '%s'", sourceVersion, targetVersion)
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
