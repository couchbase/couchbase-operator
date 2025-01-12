package oldvalidators

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/actions/context"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util"
	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kubectl"
	caopods "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/k8s/cao_pods"
	"github.com/sirupsen/logrus"
)

const (
	crdCheckDuration = 60 * time.Second
	crdCheckInterval = 10 * time.Second
)

var (
	couchbaseCRD = []string{"couchbaseautoscalers.couchbase.com", "couchbasebackuprestores.couchbase.com", "couchbasebackups.couchbase.com",
		"couchbasebuckets.couchbase.com", "couchbaseclusters.couchbase.com", "couchbasecollectiongroups.couchbase.com",
		"couchbasecollections.couchbase.com", "couchbaseephemeralbuckets.couchbase.com", "couchbasegroups.couchbase.com",
		"couchbasememcachedbuckets.couchbase.com", "couchbasemigrationreplications.couchbase.com", "couchbasereplications.couchbase.com",
		"couchbaserolebindings.couchbase.com", "couchbasescopegroups.couchbase.com", "couchbasescopes.couchbase.com", "couchbaseusers.couchbase.com"}

	ErrAllCRDsNotPresent  = errors.New("all couchbase crds not present")
	ErrOperatorNotPresent = errors.New("operator and operator-admission pod not present")
)

type PreFlight struct {
	Name  string `yaml:"name" caoCli:"required"`
	State string `yaml:"state" caoCli:"required"`
}

func (pf *PreFlight) Run(_ *context.Context, testAssets assets.TestAssetGetterSetter) error {
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

	err := util.RetryFunctionTillTimeout(funcCheckCRD, crdCheckDuration, crdCheckInterval)
	if err != nil {
		return fmt.Errorf("retry function: %w", err)
	}

	// CHECK: Operator and Operator Admission pods.
	_, _, err = caopods.GetOperatorAdmissionPodNames("default")
	if err != nil {
		return fmt.Errorf("pre flight check: %w", err)
	}

	logrus.Info("Pre-flight checks successful")

	return nil
}

func (pf *PreFlight) GetState() string {
	return pf.State
}
