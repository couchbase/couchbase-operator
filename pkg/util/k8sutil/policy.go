/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package k8sutil

import (
	"context"
	"reflect"
	"strings"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/client"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// ReconcilePDB takes a cluster and creates a PodDisruptionBudget based on cluster size.
func ReconcilePDB(client *client.Client, cluster *couchbasev2.CouchbaseCluster) error {
	if cluster.Spec.PerServiceClassPDB {
		return handlePerServiceClassPDB(client, cluster)
	}

	name := cluster.Name + "-pdb"

	required := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: LabelsForCluster(cluster),
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: SelectorForClusterResource(cluster),
			},
			MinAvailable: &intstr.IntOrString{
				IntVal: int32(cluster.Spec.TotalSize() - 1),
			},
		},
	}
	ApplyBaseAnnotations(required)
	addOwnerRefToObject(required, cluster.AsOwner())

	actualList := client.PodDisruptionBudgets.List()

	// Delete any per-serviceClass pdbs
	for _, pdb := range actualList {
		if !strings.EqualFold(pdb.Name, name) {
			if err := client.KubeClient.PolicyV1().PodDisruptionBudgets(cluster.Namespace).Delete(context.Background(), pdb.Name, metav1.DeleteOptions{}); err != nil {
				return errors.NewStackTracedError(err)
			}
		}
	}

	// Get any existing budgets, creating one if it doesn't exist.
	actual, found := client.PodDisruptionBudgets.Get(name)
	if !found {
		if _, err := client.KubeClient.PolicyV1().PodDisruptionBudgets(cluster.Namespace).Create(context.Background(), required, metav1.CreateOptions{}); err != nil {
			return errors.NewStackTracedError(err)
		}

		return nil
	}

	// If the requested and actual specifications are out of sync, patch the requested
	// version into the existing one and update it.
	if reflect.DeepEqual(required.Spec, actual.Spec) {
		return nil
	}

	// Delete and recreate as the spec is immutable.
	if err := client.KubeClient.PolicyV1().PodDisruptionBudgets(cluster.Namespace).Delete(context.Background(), name, metav1.DeleteOptions{}); err != nil {
		return errors.NewStackTracedError(err)
	}

	if _, err := client.KubeClient.PolicyV1().PodDisruptionBudgets(cluster.Namespace).Create(context.Background(), required, metav1.CreateOptions{}); err != nil {
		return errors.NewStackTracedError(err)
	}

	return nil
}

//nolint:gocognit
func handlePerServiceClassPDB(client *client.Client, cluster *couchbasev2.CouchbaseCluster) error {
	// Delete older pdb definitions in the case of an operator upgrade.
	oldPDB, ok := client.PodDisruptionBudgets.Get(cluster.Name + "-pdb")
	if ok {
		if err := client.KubeClient.PolicyV1().PodDisruptionBudgets(cluster.Namespace).Delete(context.Background(), oldPDB.Name, metav1.DeleteOptions{}); err != nil {
			return errors.NewStackTracedError(err)
		}
	}

	if len(cluster.Spec.Servers) != len(client.PodDisruptionBudgets.List()) {
		for _, pdb := range client.PodDisruptionBudgets.List() {
			found := false

			for _, serverClass := range cluster.Spec.Servers {
				if strings.EqualFold(pdb.Name, cluster.Name+"-"+serverClass.Name+"-pdb") {
					found = true
					break
				}
			}

			if !found {
				if err := client.KubeClient.PolicyV1().PodDisruptionBudgets(cluster.Namespace).Delete(context.Background(), pdb.Name, metav1.DeleteOptions{}); err != nil {
					return errors.NewStackTracedError(err)
				}
			}
		}
	}

	for _, serverClass := range cluster.Spec.Servers {
		name := cluster.Name + "-" + serverClass.Name + "-pdb"
		required := &policyv1.PodDisruptionBudget{
			ObjectMeta: metav1.ObjectMeta{
				Name:   name,
				Labels: LabelsForCluster(cluster),
			},
			Spec: policyv1.PodDisruptionBudgetSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: LabelsForServerClass(cluster, serverClass.Name),
				},
				MinAvailable: &intstr.IntOrString{
					IntVal: int32(serverClass.Size - 1),
				},
			},
		}

		ApplyBaseAnnotations(required)
		addOwnerRefToObject(required, cluster.AsOwner())

		// Get any existing budgets, creating one if it doesn't exist.
		actual, found := client.PodDisruptionBudgets.Get(name)
		if !found {
			if _, err := client.KubeClient.PolicyV1().PodDisruptionBudgets(cluster.Namespace).Create(context.Background(), required, metav1.CreateOptions{}); err != nil {
				return errors.NewStackTracedError(err)
			}

			continue
		}

		// If the requested and actual specifications are out of sync, patch the requested
		// version into the existing one and update it.
		if reflect.DeepEqual(required.Spec, actual.Spec) {
			continue
		}

		// Delete and recreate as the spec is immutable.
		if err := client.KubeClient.PolicyV1().PodDisruptionBudgets(cluster.Namespace).Delete(context.Background(), name, metav1.DeleteOptions{}); err != nil {
			return errors.NewStackTracedError(err)
		}

		if _, err := client.KubeClient.PolicyV1().PodDisruptionBudgets(cluster.Namespace).Create(context.Background(), required, metav1.CreateOptions{}); err != nil {
			return errors.NewStackTracedError(err)
		}
	}

	return nil
}
