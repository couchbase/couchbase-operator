package k8sutil

import (
	"context"
	"fmt"
	"os"
	"sort"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// Member lifecycle.
	EventReasonMemberCreationFailed  = "MemberCreationFailed"
	EventReasonNewMemberAdded        = "NewMemberAdded"
	EventReasonFailedAddNode         = "FailedAddNode"
	EventReasonFailedAddBackNode     = "FailedAddBackNode"
	EventReasonMemberRemoved         = "MemberRemoved"
	EventReasonMemberDown            = "MemberDown"
	EventReasonMemberRecovered       = "MemberRecovered"
	EventReasonMemberFailedOver      = "MemberFailedOver"
	EventReasonRebalanceStarted      = "RebalanceStarted"
	EventReasonRebalanceIncomplete   = "RebalanceIncomplete"
	EventReasonRebalanceCompleted    = "RebalanceCompleted"
	EventReasonExpandVolumeStarted   = "ExpandVolumeStarted"
	EventReasonExpandVolumeFallback  = "ExpandVolumeFallback"
	EventReasonExpandVolumeSucceeded = "ExpandVolumeSucceeded"
	EventReasonReconcileFailed       = "ReconciliationFailed"

	// Bucket lifecycle.
	EventReasonBucketCreated = "BucketCreated"
	EventReasonBucketDeleted = "BucketDeleted"
	EventReasonBucketEdited  = "BucketEdited"

	// RBAC lifecycle.
	EventReasonUserCreated  = "UserCreated"
	EventReasonUserDeleted  = "UserDeleted"
	EventReasonUserEdited   = "UserEdited"
	EventReasonGroupCreated = "GroupCreated"
	EventReasonGroupDeleted = "GroupDeleted"
	EventReasonGroupEdited  = "GroupEdited"

	// Service lifecycle.
	EventReasonServiceCreated = "ServiceCreated"
	EventReasonServiceDeleted = "ServiceDeleted"

	// Upgrade lifecycle.
	EventReasonUpgradeStarted  = "UpgradeStarted"
	EventReasonUpgradeFinished = "UpgradeFinished"

	// Cluster lifecycle.
	EventReasonClusterSettingsEdited = "ClusterSettingsEdited"

	// TLS lifecycle.
	EventReasonTLSUpdated       = "TLSUpdated"
	EventReasonTLSInvalid       = "TLSInvalid"
	EventReasonClientTLSUpdated = "ClientTLSUpdated"
	EventReasonClientTLSInvalid = "ClientTLSInvalid"

	// XDCR lifecycle.
	EventReasonRemoteClusterAdded   = "RemoteClusterAdded"
	EventReasonRemoteClusterUpdated = "RemoteClusterUpdated"
	EventReasonRemoteClusterRemoved = "RemoteClusterRemoved"
	EventReasonReplicationAdded     = "ReplicationAdded"
	EventReasonReplicationRemoved   = "ReplicationRemoved"

	// Backup lifecycle.
	EventReasonBackupCreated = "BackupCreated"
	EventReasonBackupUpdated = "BackupUpdated"
	EventReasonBackupDeleted = "BackupDeleted"
	// These are raised by the jobs themselves and are not part of the
	// CouchbaseCluster documentation.
	EventReasonBackupStarted        = "BackupStarted"
	EventReasonBackupCompleted      = "BackupCompleted"
	EventReasonBackupFailed         = "BackupFailed"
	EventReasonBackupRestoreCreated = "BackupRestoreCreated"
	EventReasonBackupRestoreDeleted = "BackupRestoreDeleted"
	EventReasonBackupMergeStarted   = "BackupMergeStarted"
	EventReasonBackupMergeCompleted = "BackupMergeCompleted"
	EventReasonBackupMergeFailed    = "BackupMergeFailed"

	// Security lifecycle.
	EventReasonSecuritySettingsUpdated = "SecuritySettingsUpdated"
	EventReasonAdminPasswordChanged    = "AdminPasswordChanged"

	// Autoscaler lifecycle.
	EventAutoscalerCreated = "EventAutoscalerCreated"
	EventAutoscalerDeleted = "EventAutoscalerDeleted"
	EventAutoscaleUp       = "EventAutoscaleUp"
	EventAutoscaleDown     = "EventAutoscaleDown"

	// Network lifecycle.
	EventNetworkSettingsModified = "NetworkSettingsModified"

	EventReasonTLSInvalidMessage = "Failed to validate TLS certificate chain"

	// Scopes and collections.
	// Note, this is kept artificially vague as flooding the system with thousands
	// of events won't win us any fans.
	EventScopesAndCollectionsUpdated = "EventScopesAndCollectionsUpdated"

	// Hibernation.
	EventReasonHibernationStarted = "HibernationStarted"
	EventReasonHibernationEnded   = "HibernationEnded"
)

func EventReasonAdminPasswordChangedEvent(cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonAdminPasswordChanged
	event.Message = "The cluster admin password was changed"

	return event
}

func MemberCreationFailedEvent(memberName string, cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeWarning
	event.Reason = EventReasonMemberCreationFailed
	event.Message = fmt.Sprintf("New member %s creation failed", memberName)

	return event
}

func MemberAddEvent(memberName string, cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonNewMemberAdded
	event.Message = fmt.Sprintf("New member %s added to cluster", memberName)

	return event
}

func MemberRemoveEvent(memberName string, cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonMemberRemoved
	event.Message = fmt.Sprintf("Existing member %s removed from the cluster", memberName)

	return event
}

func MemberDownEvent(memberName string, cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeWarning
	event.Reason = EventReasonMemberDown
	event.Message = fmt.Sprintf("Existing member %s down", memberName)

	return event
}

func MemberRecoveredEvent(memberName string, cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonMemberRecovered
	event.Message = fmt.Sprintf("Existing member %s recovered", memberName)

	return event
}

func MemberFailedOverEvent(memberName string, cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeWarning
	event.Reason = EventReasonMemberFailedOver
	event.Message = fmt.Sprintf("Existing member %s failed over", memberName)

	return event
}

func RebalanceStartedEvent(cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonRebalanceStarted
	event.Message = "A rebalance has been started to balance data across the cluster"

	return event
}

func RebalanceIncompleteEvent(cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonRebalanceIncomplete
	event.Message = "A rebalance is incomplete"

	return event
}

func RebalanceCompletedEvent(cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonRebalanceCompleted
	event.Message = "A rebalance has completed"

	return event
}

func ExpandVolumeStartedEvent(volumeName string, fromSize string, toSize string, cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonExpandVolumeStarted
	event.Message = fmt.Sprintf("Expanding Volume %s from %s to %s", volumeName, fromSize, toSize)

	return event
}

func ExpandVolumeSucceededEvent(volumeName string, cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonExpandVolumeSucceeded
	event.Message = fmt.Sprintf("Successfully expanded volume %s", volumeName)

	return event
}

func ExpandVolumeFallbackEvent(volumeName string, cl *couchbasev2.CouchbaseCluster, errorString string) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonExpandVolumeFallback
	event.Message = fmt.Sprintf("Volume %s cannot be expanded in-place, reason: %s; falling back to rolling upgrade", volumeName, errorString)

	return event
}

func FailedAddNodeEvent(memberName string, cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonFailedAddNode
	event.Message = fmt.Sprintf("Removed existing member %s because it failed before it could be added to the cluster", memberName)

	return event
}

func FailedAddBackNodeEvent(memberName string, cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonFailedAddBackNode
	event.Message = fmt.Sprintf("Removed existing member %s because it could not be added back to the cluster", memberName)

	return event
}

// no existing backup, PVC and Cronjob(s) successfully created.
func BackupCreateEvent(backup string, cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonBackupCreated
	event.Message = fmt.Sprintf("A new backup `%s` was created", backup)

	return event
}

// backup was either edited or a PVC and/or cronjob(s) recreated if absent.
func BackupUpdateEvent(backup string, cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonBackupUpdated
	event.Message = fmt.Sprintf("Backup `%s` was edited", backup)

	return event
}

// backup and its cronjob(s) deleted, PVC should remain and jobs and their pods may still remain.
func BackupDeleteEvent(backup string, cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonBackupDeleted
	event.Message = fmt.Sprintf("Backup `%s` was deleted", backup)

	return event
}

func BackupStartEvent(backup string, cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonBackupStarted
	event.Message = fmt.Sprintf("Backup `%s` started", backup)

	return event
}

func BackupCompleteEvent(backup string, cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonBackupCompleted
	event.Message = fmt.Sprintf("Backup `%s` completed", backup)

	return event
}

func BackupFailEvent(backup string, cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonBackupFailed
	event.Message = fmt.Sprintf("Backup `%s` failed", backup)

	return event
}

func BackupMergeCompletedEvent(backup string, cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonBackupMergeCompleted
	event.Message = fmt.Sprintf("Backup merge `%s` completed", backup)

	return event
}

func BackupRestoreCreateEvent(restore string, cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonBackupRestoreCreated
	event.Message = fmt.Sprintf("A new restore `%s` was created", restore)

	return event
}

func BackupRestoreDeleteEvent(restore string, cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonBackupRestoreDeleted
	event.Message = fmt.Sprintf("A new restore `%s` was deleted", restore)

	return event
}

func BucketCreateEvent(bucketName string, cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonBucketCreated
	event.Message = fmt.Sprintf("A new bucket `%s` was created", bucketName)

	return event
}

func BucketDeleteEvent(bucketName string, cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonBucketDeleted
	event.Message = fmt.Sprintf("Bucket `%s` was deleted", bucketName)

	return event
}

func BucketEditEvent(bucketName string, cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonBucketEdited
	event.Message = fmt.Sprintf("Bucket `%s` was edited", bucketName)

	return event
}

func UserCreateEvent(userName string, cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonUserCreated
	event.Message = fmt.Sprintf("A new user `%s` was created", userName)

	return event
}

func UserDeleteEvent(userName string, cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonUserDeleted
	event.Message = fmt.Sprintf("User `%s` was deleted", userName)

	return event
}

func UserEditEvent(userName string, cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonUserEdited
	event.Message = fmt.Sprintf("User `%s` was edited", userName)

	return event
}

func GroupCreateEvent(groupName string, cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonGroupCreated
	event.Message = fmt.Sprintf("A new group `%s` was created", groupName)

	return event
}

func GroupDeleteEvent(groupName string, cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonGroupDeleted
	event.Message = fmt.Sprintf("Group `%s` was deleted", groupName)

	return event
}

func GroupEditEvent(groupName string, cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonGroupEdited
	event.Message = fmt.Sprintf("Group `%s` was edited", groupName)

	return event
}

func AdminConsoleSvcCreateEvent(svcName string, cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonServiceCreated
	event.Message = fmt.Sprintf("Service for admin console `%s` was created", svcName)

	return event
}

func AdminConsoleSvcDeleteEvent(svcName string, cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonServiceDeleted
	event.Message = fmt.Sprintf("Service for admin console `%s` was deleted", svcName)

	return event
}

func UpgradeStartedEvent(cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonUpgradeStarted
	event.Message = "Started upgrade"

	return event
}

func UpgradeFinishedEvent(cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonUpgradeFinished
	event.Message = "Finished upgrade"

	return event
}

func ClusterSettingsEditedEvent(settingName string, cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonClusterSettingsEdited
	event.Message = fmt.Sprintf("Setting for `%s` was edited", settingName)

	return event
}

func TLSUpdatedEvent(cl *couchbasev2.CouchbaseCluster, name string) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonTLSUpdated
	event.Message = fmt.Sprintf("TLS certificate was updated for member %s", name)

	return event
}

func TLSInvalidEvent(cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonTLSInvalid
	event.Message = EventReasonTLSInvalidMessage

	return event
}

func ScopesAndCollectionsUpdated(cl *couchbasev2.CouchbaseCluster, bucket string) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventScopesAndCollectionsUpdated
	event.Message = fmt.Sprintf("Scopes and collections updated for bucket %s", bucket)

	return event
}

// ClientTLSUpdateReason is a symbolic constant message to aid debugging.
type ClientTLSUpdateReason string

const (
	// ClientTLSUpdateReasonCreateCA is for when TLS is dynamically enabled.
	ClientTLSUpdateReasonCreateCA ClientTLSUpdateReason = "CA certificate was added"

	// ClientTLSUpdateReasonUpdateCA is for when a CA is rotated.
	ClientTLSUpdateReasonUpdateCA ClientTLSUpdateReason = "CA certificate was updated"

	// ClientTLSUpdateReasonDeleteCA is for when TLS is dynamically disabled.
	ClientTLSUpdateReasonDeleteCA ClientTLSUpdateReason = "CA certificate was removed"

	// ClientTLSUpdateReasonCreateClientAuth is for when mTLS is dynamically enabled.
	ClientTLSUpdateReasonCreateClientAuth ClientTLSUpdateReason = "Client certificate was added"

	// ClientTLSUpdateReasonUpdateClientAuth is for when client certificates are rotated.
	ClientTLSUpdateReasonUpdateClientAuth ClientTLSUpdateReason = "Client certificate was updated"

	// ClientTLSUpdateReasonDeleteClientAuth is for when mTLS is dynamically disabled.
	ClientTLSUpdateReasonDeleteClientAuth ClientTLSUpdateReason = "Client certificate was removed"
)

func ClientTLSUpdatedEvent(cl *couchbasev2.CouchbaseCluster, why ClientTLSUpdateReason) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonClientTLSUpdated
	event.Message = string(why)

	return event
}

func ClientTLSInvalidEvent(cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonClientTLSInvalid
	event.Message = EventReasonTLSInvalidMessage

	return event
}

func RemoteClusterAddedEvent(cl *couchbasev2.CouchbaseCluster, name string) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonRemoteClusterAdded
	event.Message = fmt.Sprintf("XDCR remote cluster %s added", name)

	return event
}

func RemoteClusterUpdatedEvent(cl *couchbasev2.CouchbaseCluster, name string) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonRemoteClusterUpdated
	event.Message = fmt.Sprintf("XDCR remote cluster %s updated", name)

	return event
}

func RemoteClusterRemovedEvent(cl *couchbasev2.CouchbaseCluster, name string) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonRemoteClusterRemoved
	event.Message = fmt.Sprintf("XDCR remote cluster %s removed", name)

	return event
}

func ReplicationAddedEvent(cl *couchbasev2.CouchbaseCluster, name string) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonReplicationAdded
	event.Message = fmt.Sprintf("XDCR replication %s added", name)

	return event
}

func ReplicationRemovedEvent(cl *couchbasev2.CouchbaseCluster, name string) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonReplicationRemoved
	event.Message = fmt.Sprintf("XDCR replication %s removed", name)

	return event
}

func AutoscalerCreateEvent(cl *couchbasev2.CouchbaseCluster, name string) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventAutoscalerCreated
	event.Message = fmt.Sprintf("Autoscaler for config `%s` added", name)

	return event
}

func AutoscalerDeleteEvent(cl *couchbasev2.CouchbaseCluster, name string) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventAutoscalerDeleted
	event.Message = fmt.Sprintf("Autoscaler for config `%s` removed", name)

	return event
}

func AutoscaleUpEvent(cl *couchbasev2.CouchbaseCluster, name string, from int, to int) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventAutoscaleUp
	event.Message = fmt.Sprintf("Autoscaling up config `%s` from %d to %d", name, from, to)

	return event
}

func AutoscaleDownEvent(cl *couchbasev2.CouchbaseCluster, name string, from int, to int) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventAutoscaleDown
	event.Message = fmt.Sprintf("Autoscaling down config `%s` from %d to %d", name, from, to)

	return event
}

func ReconcileFailedEvent(cl *couchbasev2.CouchbaseCluster, err error) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonReconcileFailed
	event.Message = err.Error()

	return event
}

const (
	SecuritySettingUpdated                          = "Security settings modified"
	SecuritySettingUpdatedN2NEncryptionModified     = "Node-to-Node encryption modified"
	SecuritySettingUpdatedN2NEncryptionModeModified = "Node-to-Node encryption mode modified"
)

func SecuritySettingsUpdatedEvent(cl *couchbasev2.CouchbaseCluster, message string) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonSecuritySettingsUpdated
	event.Message = message

	return event
}

func NetworkSettingsModifiedEvent(cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventNetworkSettingsModified
	event.Message = "Network settings were updated"

	return event
}

func HibernationStartedEvent(cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonHibernationStarted
	event.Message = "Cluster entered hibernation"

	return event
}

func HibernationEndedEvent(cl *couchbasev2.CouchbaseCluster) *v1.Event {
	event := newClusterEvent(cl)
	event.Type = v1.EventTypeNormal
	event.Reason = EventReasonHibernationEnded
	event.Message = "Cluster left hibernation"

	return event
}

func newClusterEvent(cl *couchbasev2.CouchbaseCluster) *v1.Event {
	t := time.Now()

	return &v1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: cl.Name + "-",
			Namespace:    cl.Namespace,
		},
		InvolvedObject: v1.ObjectReference{
			APIVersion:      couchbasev2.SchemeGroupVersion.String(),
			Kind:            couchbasev2.ClusterCRDResourceKind,
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

// GetEventsForResource returns a time ordered list of events for a specific resource.
func GetEventsForResource(client kubernetes.Interface, namespace, kind, name string) ([]v1.Event, error) {
	events, err := client.CoreV1().Events(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, errors.NewStackTracedError(err)
	}

	// Also build up data structures necessary for sorting
	filteredEvents := []v1.Event{}

	for _, event := range events.Items {
		if event.InvolvedObject.Kind == kind && event.InvolvedObject.Name == name {
			filteredEvents = append(filteredEvents, event)
		}
	}

	// Sort the timestamps
	sorter := eventSorter{events: filteredEvents}
	sort.Sort(sorter)

	return sorter.events, nil
}

// eventSorter is a type able to sort events by time.
type eventSorter struct {
	events []v1.Event
}

// Len returns the length of the events list to be sorted.
func (s eventSorter) Len() int {
	return len(s.events)
}

// Swap does an in-place swap of elements in a list.
func (s eventSorter) Swap(i, j int) {
	s.events[i], s.events[j] = s.events[j], s.events[i]
}

// Less does numeric comparisons of event time stamps.
func (s eventSorter) Less(i, j int) bool {
	return s.events[i].LastTimestamp.String() < s.events[j].LastTimestamp.String()
}
