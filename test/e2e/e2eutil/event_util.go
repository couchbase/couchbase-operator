package e2eutil

import (
	"fmt"
	"testing"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/eventschema"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"

	v1 "k8s.io/api/core/v1"
)

type EventList []v1.Event

var SwapRebalanceSequence = eventschema.Sequence{
	Validators: []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
		eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved},
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
	},
}

func (e EventList) Len() int {
	return len(e)
}

func (e EventList) Less(a, b int) bool {
	return e[a].FirstTimestamp.Before(&e[b].FirstTimestamp)
}

func (e EventList) Swap(a, b int) {
	e[a], e[b] = e[b], e[a]
}

func EventExistsInEventList(event *v1.Event, eventList EventList) bool {
	for i := range eventList {
		e := eventList[i]

		if EqualEvent(event, &e) {
			return true
		}
	}

	return false
}

func EqualEvent(e1, e2 *v1.Event) bool {
	return (e1.Type == e2.Type && e1.Reason == e2.Reason && e1.Message == e2.Message)
}

func LooseEqualEvent(e1, e2 *v1.Event) bool {
	return (e1.Type == e2.Type && e1.Reason == e2.Reason)
}

func NewMemberCreationFailedEvent(cl *couchbasev2.CouchbaseCluster, memberID int) *v1.Event {
	name := couchbaseutil.CreateMemberName(cl.Name, memberID)
	return k8sutil.MemberCreationFailedEvent(name, cl)
}

func NewMemberAddEvent(cl *couchbasev2.CouchbaseCluster, memberID int) *v1.Event {
	name := couchbaseutil.CreateMemberName(cl.Name, memberID)
	return k8sutil.MemberAddEvent(name, cl)
}

func NewUpgradeFinishedEvent(cl *couchbasev2.CouchbaseCluster) *v1.Event {
	return k8sutil.UpgradeFinishedEvent(cl)
}

func NewMemberRemoveEvent(cl *couchbasev2.CouchbaseCluster, memberID int) *v1.Event {
	name := couchbaseutil.CreateMemberName(cl.Name, memberID)
	return k8sutil.MemberRemoveEvent(name, cl)
}

func FailedAddNodeEvent(cl *couchbasev2.CouchbaseCluster, memberID int) *v1.Event {
	name := couchbaseutil.CreateMemberName(cl.Name, memberID)
	return k8sutil.FailedAddNodeEvent(name, cl)
}

func NewMemberDownEvent(cl *couchbasev2.CouchbaseCluster, memberID int) *v1.Event {
	name := couchbaseutil.CreateMemberName(cl.Name, memberID)
	return k8sutil.MemberDownEvent(name, cl)
}

func NewMemberFailedOverEvent(cl *couchbasev2.CouchbaseCluster, memberID int) *v1.Event {
	name := couchbaseutil.CreateMemberName(cl.Name, memberID)
	return k8sutil.MemberFailedOverEvent(name, cl)
}

func ServicesMismatchEvent(cl *couchbasev2.CouchbaseCluster) *v1.Event {
	return k8sutil.EventReasonServicesMismatchEvent(cl)
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

func MemberRecoveredEvent(cl *couchbasev2.CouchbaseCluster, memberID int) *v1.Event {
	name := couchbaseutil.CreateMemberName(cl.Name, memberID)
	return k8sutil.MemberRecoveredEvent(name, cl)
}

func TLSUpdatedEvent(cl *couchbasev2.CouchbaseCluster, name string) *v1.Event {
	return k8sutil.TLSUpdatedEvent(cl, name)
}

func TLSInvalidEvent(cl *couchbasev2.CouchbaseCluster) *v1.Event {
	return k8sutil.TLSInvalidEvent(cl)
}

func ReplicationAddedEvent(c *couchbasev2.CouchbaseCluster, remoteClusterName, srcBucketName, dstBucketName string) *v1.Event {
	remoteClusterName = applyXDCRRemoteClusterName(remoteClusterName)
	name := fmt.Sprintf("%s/%s/%s", remoteClusterName, srcBucketName, dstBucketName)

	return k8sutil.ReplicationAddedEvent(c, name)
}

func ReplicationRemovedEvent(c *couchbasev2.CouchbaseCluster, remoteClusterName, srcBucketName, dstBucketName string) *v1.Event {
	remoteClusterName = applyXDCRRemoteClusterName(remoteClusterName)
	name := fmt.Sprintf("%s/%s/%s", remoteClusterName, srcBucketName, dstBucketName)

	return k8sutil.ReplicationRemovedEvent(c, name)
}

func BucketEditedEvent(c *couchbasev2.CouchbaseCluster, bucket string) *v1.Event {
	return k8sutil.BucketEditEvent(bucket, c)
}

func BackupStartedEvent(c *couchbasev2.CouchbaseCluster, backup string) *v1.Event {
	return k8sutil.BackupStartEvent(backup, c)
}

func BackupCompletedEvent(c *couchbasev2.CouchbaseCluster, backup string) *v1.Event {
	return k8sutil.BackupCompleteEvent(backup, c)
}

func BackupMergeCompletedEvent(c *couchbasev2.CouchbaseCluster, backup string) *v1.Event {
	return k8sutil.BackupMergeCompletedEvent(backup, c)
}

func BackupFailedEvent(c *couchbasev2.CouchbaseCluster, backup string) *v1.Event {
	return k8sutil.BackupFailEvent(backup, c)
}

func BackupUpdatedEvent(c *couchbasev2.CouchbaseCluster, backup string) *v1.Event {
	return k8sutil.BackupUpdateEvent(backup, c)
}

func AutoscaleUpEvent(cl *couchbasev2.CouchbaseCluster, configName string, from int, to int) *v1.Event {
	return k8sutil.AutoscaleUpEvent(cl, configName, from, to)
}

func AutoscaleDownEvent(cl *couchbasev2.CouchbaseCluster, configName string, from int, to int) *v1.Event {
	return k8sutil.AutoscaleDownEvent(cl, configName, from, to)
}

func ReconcileFailedEvent(cl *couchbasev2.CouchbaseCluster) *v1.Event {
	return k8sutil.ReconcileFailedEvent(cl, fmt.Errorf("dummy error"))
}

func NewVolumeExpandStartedEvent(volumeName string, from string, to string, cl *couchbasev2.CouchbaseCluster) *v1.Event {
	return k8sutil.ExpandVolumeStartedEvent(volumeName, from, to, cl)
}

func NewMemberAddedEvent(cl *couchbasev2.CouchbaseCluster, memberID int) *v1.Event {
	name := couchbaseutil.CreateMemberName(cl.Name, memberID)
	return k8sutil.MemberAddEvent(name, cl)
}

func NewMemberRemovedEvent(cl *couchbasev2.CouchbaseCluster, memberID int) *v1.Event {
	name := couchbaseutil.CreateMemberName(cl.Name, memberID)
	return k8sutil.MemberRemoveEvent(name, cl)
}

func UserEditedEvent(cl *couchbasev2.CouchbaseCluster, user *couchbasev2.CouchbaseUser) *v1.Event {
	return k8sutil.UserEditEvent(user.Name, cl)
}

func ClusterSettingsEditedEvent(cl *couchbasev2.CouchbaseCluster, settingName string) *v1.Event {
	return k8sutil.ClusterSettingsEditedEvent(settingName, cl)
}

func NewEncryptionKeyCreatedEvent(cl *couchbasev2.CouchbaseCluster, keyName string) *v1.Event {
	return k8sutil.EncryptionKeyCreatedEvent(cl, keyName)
}

func NewEncryptionKeyUpdatedEvent(cl *couchbasev2.CouchbaseCluster, keyName string) *v1.Event {
	return k8sutil.EncryptionKeyUpdatedEvent(cl, keyName)
}

// VolumeExpansionSuccessSequence combines the successful series of events associated with expanding persistent volumes.
func VolumeExpansionSuccessSequence() eventschema.Validatable {
	return eventschema.Sequence{
		Validators: []eventschema.Validatable{
			eventschema.Event{Reason: k8sutil.EventReasonExpandVolumeStarted},
			eventschema.Event{Reason: k8sutil.EventReasonExpandVolumeSucceeded},
		},
	}
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

// ClusterCreateSequenceWithExposedFeatures is a common function for generating cluster
// creation events, with specific featuresets.
func ClusterCreateSequenceWithExposedFeatures(size int, _ ...couchbasev2.ExposedFeature) eventschema.Validatable {
	schema := eventschema.Sequence{
		Validators: []eventschema.Validatable{
			eventschema.Repeat{
				Times:     size,
				Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
			},
		},
	}

	if size > 1 {
		schema.Validators = append(schema.Validators,
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
		)
	}

	return schema
}

// ClusterCreateSequenceWithMutualTLS is a common function for generating cluster
// creation events, when mTLS is enabled.
func ClusterCreateSequenceWithMutualTLS(size int) eventschema.Validatable {
	if size == 1 {
		return eventschema.Sequence{
			Validators: []eventschema.Validatable{
				eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
				eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
			},
		}
	}

	return eventschema.Sequence{
		Validators: []eventschema.Validatable{
			eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
			eventschema.Repeat{
				Times:     size - 1,
				Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
			},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
			eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
		},
	}
}

// ClusterCreateSequenceWithN2N is a common function for generating cluster
// creation events, when N2N is enabled.
func ClusterCreateSequenceWithN2N(size int, encryptionType couchbasev2.NodeToNodeEncryptionType) eventschema.Validatable {
	schema := eventschema.Sequence{
		Validators: []eventschema.Validatable{
			eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
		},
	}

	if size > 1 {
		schema.Validators = append(schema.Validators, ClusterScaleUpSequence(size-1))
	}

	schema.Validators = append(schema.Validators, eventschema.Event{Reason: k8sutil.EventReasonSecuritySettingsUpdated})

	// Control Plane Only is the default, anything else will change the mode.
	if encryptionType != couchbasev2.NodeToNodeControlPlaneOnly {
		schema.Validators = append(schema.Validators, eventschema.Event{Reason: k8sutil.EventReasonSecuritySettingsUpdated})
	}

	return schema
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

func ClusterScaleUpSequenceWithMemberNames(names []string) eventschema.Validatable {
	var validators []eventschema.Validatable

	for _, name := range names {
		validators = append(validators, eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded, FuzzyMessage: name})
	}

	validators = append(validators, eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted})
	validators = append(validators, eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted})

	return eventschema.Sequence{
		Validators: validators,
	}
}

// ClusterScaleDownSequence is a common function for generating cluster scaling down events.
func ClusterScaleDownSequence(size int) eventschema.Validatable {
	return eventschema.Sequence{
		Validators: []eventschema.Validatable{
			eventschema.Optional{
				Validator: eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
			},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			eventschema.Repeat{
				Times:     size,
				Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved},
			},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
		},
	}
}

func ClusterScaleDownSequenceWithMemberNames(names []string) eventschema.Validatable {
	validators := []eventschema.Validatable{
		eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
	}

	for _, name := range names {
		validators = append(validators, eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved, FuzzyMessage: name})
	}

	validators = append(validators, eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted})

	return eventschema.Sequence{
		Validators: validators,
	}
}

// PodDownFailoverRecoverySequence is a common function for generating down/failover/recovery events.
// Observing the pod go down is optional, and not of any real consequence.  It's entirely possible
// that the pod is failed while the Operator is inactive for whatever reason.
func PodDownFailoverRecoverySequence() eventschema.Validatable {
	return eventschema.Sequence{
		Validators: []eventschema.Validatable{
			eventschema.Optional{
				Validator: eventschema.RepeatAtLeast{Times: 1, Validator: eventschema.Event{Reason: k8sutil.EventReasonReconcileFailed}},
			},
			eventschema.Optional{
				Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown},
			},
			eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver},
			eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
		},
	}
}

// PodRecoverySequenceAfterKilled checks the sequence in which pods goes down without removing
// the volumes and how they recover, followed by the rebalalnce which takes place.
func PodRecoverySequenceAfterKilled() eventschema.Validatable {
	return eventschema.Sequence{
		Validators: []eventschema.Validatable{
			eventschema.Optional{
				Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown},
			},
			eventschema.Optional{
				Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver},
			},
			eventschema.Optional{
				Validator: eventschema.RepeatAtLeast{Times: 1, Validator: eventschema.Event{Reason: k8sutil.EventReasonReconcileFailed}},
			},
			eventschema.Event{Reason: k8sutil.EventReasonMemberRecovered},
			eventschema.Optional{
				Validator: eventschema.RepeatAtLeast{Times: 1, Validator: eventschema.Event{Reason: k8sutil.EventReasonReconcileFailed}},
			},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			eventschema.Optional{
				Validator: eventschema.Event{Reason: k8sutil.EventReasonRebalanceIncomplete},
			},
			eventschema.Optional{
				Validator: eventschema.RepeatAtLeast{Times: 1, Validator: eventschema.Event{Reason: k8sutil.EventReasonReconcileFailed}},
			},
			eventschema.Optional{
				Validator: eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			},
			eventschema.Optional{
				Validator: eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
			},
		},
	}
}

// KubernetesUpgradeSequenceEphemeral is the expected even sequence for a Kubernets upgrade with
// an epehemeral cluster.  Note the down even is optional as the Operator may get evicted at
// the same time as a pod and miss the event as Couchbase has already failed over the pod.
func KubernetesUpgradeSequenceEphemeral(clusterSize int) eventschema.Validatable {
	return eventschema.RepeatAtLeast{
		Times: clusterSize,
		Validator: eventschema.Sequence{
			Validators: []eventschema.Validatable{
				eventschema.Optional{
					Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown},
				},
				eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver},
				eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
				eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
				eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved},
				eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
			},
		},
	}
}

// ServerCrashRecoverySequence is generated when you kill NS server. Some older server versions may wait for a rebalance during recovery.
func ServerCrashRecoverySequence(waitForRebalance bool) eventschema.Validatable {
	if !waitForRebalance {
		return eventschema.Sequence{
			Validators: []eventschema.Validatable{
				// The server instance may come back before being registered as down,
				// especially if the operator is dead that time.
				eventschema.Optional{
					Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown},
				},
			},
		}
	}

	return eventschema.Sequence{
		Validators: []eventschema.Validatable{
			// The server instance may come back before being registered as down,
			// especially if the operator is dead that time.
			eventschema.Optional{
				Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown},
			},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
		},
	}
}

// PodDownWithPVCRecoverySequence is a common sequence when some pods are nuked with PVC
// storage attached and they can delta-recover.
func PodDownWithPVCRecoverySequence(clusterSize, victims int) eventschema.Validatable {
	events := eventschema.Sequence{}

	if victims == clusterSize {
		events.Validators = append(events.Validators, eventschema.Optional{Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberRecovered}})
		victims--
	}

	events.Validators = append(events.Validators, eventschema.Sequence{
		Validators: []eventschema.Validatable{
			eventschema.Optional{
				Validator: eventschema.RepeatAtLeast{Times: 1, Validator: eventschema.Event{Reason: k8sutil.EventReasonReconcileFailed}},
			},
			eventschema.Repeat{
				Times:     victims,
				Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown},
			},
			eventschema.Repeat{
				Times:     victims,
				Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberRecovered},
			},
			eventschema.Optional{
				Validator: eventschema.Sequence{
					Validators: []eventschema.Validatable{
						eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
						eventschema.Event{Reason: k8sutil.EventReasonRebalanceIncomplete},
						eventschema.Optional{
							Validator: eventschema.Event{Reason: k8sutil.EventReasonReconcileFailed},
						},
					},
				},
			},
			// This may or may not happen on 6.5.0... :/
			eventschema.Optional{
				Validator: eventschema.Sequence{
					Validators: []eventschema.Validatable{
						eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
						eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
					},
				},
			},
		},
	})

	return events
}

// PodFailedOverWithPVCRecoverySequence is a common sequence when some pods are nuked with PVC
// storage attached and they can delta-recover after a failover event occurs.
func PodFailedOverWithPVCRecoverySequence(victims int) eventschema.Validatable {
	return eventschema.Sequence{
		Validators: []eventschema.Validatable{
			eventschema.Optional{Validator: eventschema.RepeatAtLeast{Times: 1, Validator: eventschema.Event{Reason: k8sutil.EventReasonReconcileFailed}}},
			// Depending on either server version, auto-failover timeout and where the reconciliation loop
			// is at when pods are failed over, we may or may not see member down events for the victims
			eventschema.Optional{
				Validator: eventschema.Repeat{
					Times:     victims,
					Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown},
				},
			},
			eventschema.Repeat{
				Times:     victims,
				Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver},
			},
			eventschema.Repeat{
				Times: victims,
				Validator: eventschema.Sequence{
					Validators: []eventschema.Validatable{
						eventschema.Event{Reason: k8sutil.EventReasonMemberRecovered},
						eventschema.Event{Reason: k8sutil.EventReasonReconcileFailed},
					},
				},
			},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
		},
	}
}

// PodDownWithPVCRecoverySequenceWithEphemeral is shat to expect when the platform
// goes down with some ephemeral pods in the cluster.  The persistent pods will
// be recovered first, then the operator needs to failover the ephemeral pods and
// recreate them, they are gone as it is.
func PodDownWithPVCRecoverySequenceWithEphemeral(t *testing.T, clusterSize, persistentVictims, ephemeralVictims int, serverImage string) eventschema.Validatable {
	tag, err := k8sutil.CouchbaseVersion(serverImage)
	if err != nil {
		Die(t, err)
	}

	version, err := couchbaseutil.NewVersion(tag)
	if err != nil {
		Die(t, err)
	}

	// In CBS7 a forced failover ejects the member due to metadata inconsistencies,
	// so it behaves differently.
	if version.GreaterEqualString("7.0.0") {
		return eventschema.Sequence{
			Validators: []eventschema.Validatable{
				eventschema.Event{Reason: k8sutil.EventReasonMemberRecovered},
				eventschema.Repeat{
					Times:     clusterSize - 1,
					Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown},
				},
				eventschema.Repeat{
					Times:     persistentVictims - 1,
					Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberRecovered},
				},
				eventschema.Repeat{
					Times:     ephemeralVictims,
					Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver},
				},
				eventschema.Repeat{
					Times:     ephemeralVictims,
					Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
				},
				eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
				eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
			},
		}
	}

	return eventschema.Sequence{
		Validators: []eventschema.Validatable{
			eventschema.Event{Reason: k8sutil.EventReasonMemberRecovered},
			eventschema.Repeat{
				Times:     clusterSize - 1,
				Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown},
			},
			eventschema.Repeat{
				Times:     persistentVictims - 1,
				Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberRecovered},
			},
			eventschema.Repeat{
				Times:     ephemeralVictims,
				Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver},
			},
			eventschema.Repeat{
				Times:     ephemeralVictims,
				Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
			},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			eventschema.Repeat{
				Times:     ephemeralVictims,
				Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved},
			},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
		},
	}
}

// PodDownFailedWithPVCRecoverySequence is a common sequence when stateful pods are
// failed over but Couchbase before Operator recovery.
func PodDownFailedWithPVCRecoverySequence(victims int) eventschema.Validatable {
	return eventschema.Sequence{
		Validators: []eventschema.Validatable{
			eventschema.Optional{
				Validator: eventschema.RepeatAtLeast{Times: 1, Validator: eventschema.Event{Reason: k8sutil.EventReasonReconcileFailed}},
			},
			eventschema.Optional{
				Validator: eventschema.Repeat{
					Times:     victims,
					Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown},
				},
			},
			eventschema.Optional{
				Validator: eventschema.Repeat{
					Times:     victims,
					Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver},
				},
			},
			eventschema.Optional{
				Validator: eventschema.Repeat{
					Times:     victims,
					Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberRecovered},
				},
			},
			eventschema.Optional{
				Validator: eventschema.Event{Reason: k8sutil.EventReasonReconcileFailed},
			},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
		},
	}
}

func PodDownEphemeralWithForcedFailover(t *testing.T, victims int, serverImage string) eventschema.Validatable {
	tag, err := k8sutil.CouchbaseVersion(serverImage)
	if err != nil {
		Die(t, err)
	}

	version, err := couchbaseutil.NewVersion(tag)
	if err != nil {
		Die(t, err)
	}

	// In CBS7 a forced failover ejects the member due to metadata inconsistencies,
	// so it behaves differently.
	if version.GreaterEqualString("7.0.0") {
		return eventschema.Sequence{
			Validators: []eventschema.Validatable{
				eventschema.Repeat{Times: victims, Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown}},
				eventschema.Repeat{Times: victims, Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded}},
				eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
				eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
			},
		}
	}

	return eventschema.Sequence{
		Validators: []eventschema.Validatable{
			eventschema.Repeat{Times: victims, Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown}},
			eventschema.Repeat{Times: victims, Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver}},
			eventschema.Repeat{Times: victims, Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded}},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			eventschema.Repeat{Times: victims, Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved}},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
		},
	}
}

func MultiNodeSwapRebalanceSequence(nodes int) eventschema.Validatable {
	return eventschema.Sequence{
		Validators: []eventschema.Validatable{
			eventschema.Repeat{
				Times:     nodes,
				Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
			},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			eventschema.Repeat{
				Times:     nodes,
				Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberRemoved},
			},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
		},
	}
}
