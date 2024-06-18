package validations

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/kubectl"
	"github.com/sirupsen/logrus"
)

const (
	crdTotalDuration = 15
	crdPollInterval  = 5
)

var (
	couchbaseCRD = []string{"couchbaseautoscalers.couchbase.com", "couchbasebackuprestores.couchbase.com", "couchbasebackups.couchbase.com",
		"couchbasebuckets.couchbase.com", "couchbaseclusters.couchbase.com", "couchbasecollectiongroups.couchbase.com",
		"couchbasecollections.couchbase.com", "couchbaseephemeralbuckets.couchbase.com", "couchbasegroups.couchbase.com",
		"couchbasememcachedbuckets.couchbase.com", "couchbasemigrationreplications.couchbase.com", "couchbasereplications.couchbase.com",
		"couchbaserolebindings.couchbase.com", "couchbasescopegroups.couchbase.com", "couchbasescopes.couchbase.com", "couchbaseusers.couchbase.com"}

	ErrAllCRDsNotPresent = errors.New("all couchbase crds not present")
)

type PreFlight struct {
	State string `yaml:"state"`
}

func (c *PreFlight) Run(_ *context.Context) error {
	logrus.Info("Pre-flight checks started")

	// CHECK: CRDs
	funcCheckCRD := func() error {
		stdout, err := kubectl.Get("crds").Output()
		if err != nil {
			return fmt.Errorf("get couchbase crds information: %w", err)
		}

		checkCRD := 0

		for _, crd := range couchbaseCRD {
			if strings.Contains(stdout, crd) {
				checkCRD++
			}
		}

		if checkCRD == len(couchbaseCRD) {
			return nil
		}

		return fmt.Errorf("check couchbase crds: %w", ErrAllCRDsNotPresent)
	}

	err := util.RetryFunctionTillTimeout(funcCheckCRD, time.Duration(crdTotalDuration)*time.Second, time.Duration(crdPollInterval)*time.Second)
	if err != nil {
		return fmt.Errorf("retry function: %w", err)
	}

	// CHECK: Check if the Operator and Operator Admission present or not.

	logrus.Info("Pre-flight checks successful")

	return nil
}

func (c *PreFlight) GetState() string {
	return c.State
}
