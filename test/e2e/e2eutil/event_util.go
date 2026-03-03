/*
Copyright 2017-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

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

func (e EventList) Len() int {
	return len(e)
}

func (e EventList) Less(a, b int) bool {
	return e[a].FirstTimestamp.Before(&e[b].FirstTimestamp)
}

func (e EventList) Swap(a, b int) {
	t := e[a]
	e[a] = e[b]
	e[b] = t
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

func NewMemberCreationFailedEvent(cl *couchbasev2.CouchbaseCluster, memberID int) *v1.Event {
	name := couchbaseutil.CreateMemberName(cl.Name, memberID)
	return k8sutil.MemberCreationFailedEvent(name, cl)
}

func NewMemberAddEvent(cl *couchbasev2.CouchbaseCluster, memberID int) *v1.Event {
	name := couchbaseutil.CreateMemberName(cl.Name, memberID)
	return k8sutil.MemberAddEvent(name, cl)
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

func TLSUpdatedEvent(cl *couchbasev2.CouchbaseCluster) *v1.Event {
	return k8sutil.TLSUpdatedEvent(cl)
}

func TLSInvalidEvent(cl *couchbasev2.CouchbaseCluster) *v1.Event {
	return k8sutil.TLSInvalidEvent(cl)
}

func ReplicationRemovedEvent(c *couchbasev2.CouchbaseCluster, remote, source, destination string) *v1.Event {
	name := fmt.Sprintf("%s/%s/%s", remote, source, destination)
	return k8sutil.ReplicationRemovedEvent(c, name)
}

func BackupStartedEvent(c *couchbasev2.CouchbaseCluster, backup string) *v1.Event {
	return k8sutil.BackupStartEvent(backup, c)
}

func BackupCompletedEvent(c *couchbasev2.CouchbaseCluster, backup string) *v1.Event {
	return k8sutil.BackupCompleteEvent(backup, c)
}

func BackupFailedEvent(c *couchbasev2.CouchbaseCluster, backup string) *v1.Event {
	return k8sutil.BackupFailEvent(backup, c)
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
func ClusterCreateSequenceWithExposedFeatures(size int, features ...string) eventschema.Validatable {
	services := map[string]interface{}{}
	for _, feature := range features {
		switch feature {
		case couchbasev2.FeatureAdmin:
			services["admin"] = nil
		case couchbasev2.FeatureXDCR:
			services["admin"] = nil
			services["index"] = nil
			services["data"] = nil
		case couchbasev2.FeatureClient:
			services["admin"] = nil
			services["index"] = nil
			services["query"] = nil
			services["search"] = nil
			services["analytics"] = nil
			services["eventing"] = nil
			services["data"] = nil
		}
	}

	schema := eventschema.Sequence{
		Validators: []eventschema.Validatable{
			eventschema.Repeat{
				Times:     size,
				Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
			},
			eventschema.Repeat{
				Times:     len(services),
				Validator: eventschema.Event{Reason: k8sutil.EventReasonNodeServiceCreated},
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
			eventschema.Event{Reason: k8sutil.EventReasonClusterSettingsEdited},
			eventschema.Repeat{
				Times:     size - 1,
				Validator: eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
			},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
		},
	}
}

// ClusterCreateSequenceWithN2N is a common function for generating cluster
// creation events, when N2N is enabled.
func ClusterCreateSequenceWithN2N(size int, encryptionType couchbasev2.NodeToNodeEncryptionType) eventschema.Validatable {
	schema := eventschema.Sequence{
		Validators: []eventschema.Validatable{
			eventschema.Event{Reason: k8sutil.EventReasonNewMemberAdded},
			eventschema.Event{Reason: k8sutil.EventReasonSecuritySettingsUpdated},
		},
	}

	// Control Plane Only is the default, anything else will change the mode.
	if encryptionType != couchbasev2.NodeToNodeControlPlaneOnly {
		schema.Validators = append(schema.Validators, eventschema.Event{Reason: k8sutil.EventReasonSecuritySettingsUpdated})
	}

	if size > 1 {
		schema.Validators = append(schema.Validators, ClusterScaleUpSequence(size-1))
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

// ServerCrashRecoverySequence is generated when you kill NS server.
func ServerCrashRecoverySequence() eventschema.Validatable {
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
		events.Validators = append(events.Validators, eventschema.Event{Reason: k8sutil.EventReasonMemberRecovered})
		victims -= 1
	}
	events.Validators = append(events.Validators, eventschema.Sequence{
		Validators: []eventschema.Validatable{
			eventschema.Repeat{
				Times:     victims,
				Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown},
			},
			eventschema.Repeat{
				Times:     victims,
				Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberRecovered},
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

// PodDownFailedWithPVCRecoverySequence is a common sequence when stateful pods are
// failed over but Couchbase before Operator recovery.
func PodDownFailedWithPVCRecoverySequence(victims int) eventschema.Validatable {
	return eventschema.Sequence{
		Validators: []eventschema.Validatable{
			eventschema.Repeat{
				Times:     victims,
				Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberDown},
			},
			eventschema.Repeat{
				Times:     victims,
				Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberFailedOver},
			},
			eventschema.Repeat{
				Times:     victims,
				Validator: eventschema.Event{Reason: k8sutil.EventReasonMemberRecovered},
			},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceStarted},
			eventschema.Event{Reason: k8sutil.EventReasonRebalanceCompleted},
		},
	}
}
