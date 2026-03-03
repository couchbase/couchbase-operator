/*
Copyright 2021-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package cluster

import (
	"fmt"

	"github.com/couchbase/couchbase-operator/pkg/cluster/persistence"
	"github.com/couchbase/couchbase-operator/pkg/errors"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
)

// reconcileAdminPassword compares the source-of-truth in the perisistent cache
// with what's in the secret, rotating both the cluster and cache.  While not
// atomic, you'd have to be very unlucky for the former to happen and the operator
// bomb out before the latter!
func (c *Cluster) reconcileAdminPassword() error {
	secret, found := c.k8s.Secrets.Get(c.cluster.Spec.Security.AdminSecret)
	if !found {
		return fmt.Errorf("%w: unable to get admin secret %s", errors.NewStackTracedError(errors.ErrResourceRequired), c.cluster.Spec.Security.AdminSecret)
	}

	passwordRaw, ok := secret.Data[constants.AuthSecretPasswordKey]
	if !ok {
		return fmt.Errorf("%w: secret missing password", errors.NewStackTracedError(errors.ErrResourceAttributeRequired))
	}

	password := string(passwordRaw)

	if password == c.password {
		return nil
	}

	if !k8sutil.PasswordCompliesWithCouchbasePasswordPolicy(c.cluster.Spec.Security.PasswordPolicy, password) {
		log.Info("Unable to rotate admin password as it does not comply with the current password policy", "cluster", c.namespacedName())
		return nil
	}

	log.Info("Rotating admin password", "cluster", c.namespacedName())

	if err := couchbaseutil.ChangePassword(password).On(c.api, c.readyMembers()); err != nil {
		return err
	}

	if err := c.state.Upsert(persistence.Password, password); err != nil {
		return err
	}

	// Update all the places the password is cached.
	c.password = password

	c.api.SetPassword(password)

	c.raiseEvent(k8sutil.EventReasonAdminPasswordChangedEvent(c.cluster))

	return nil
}
