package destroykubernetes

import (
	"errors"
	"fmt"
	"strings"

	"context"

	kind "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils/kind"
)

var (
	ErrClusterDoesntExists = errors.New("cluster doesn't exists")
)

type DeleteKindCluster struct {
	ClusterName string
}

func contains(array []string, str string) bool {
	for _, item := range array {
		if item == str {
			return true
		}
	}

	return false
}

func (dkc *DeleteKindCluster) DeleteCluster(ctx context.Context) error {
	if err := dkc.ValidateParams(ctx); err != nil {
		return err
	}

	if err := kind.DeleteCluster("", dkc.ClusterName).ExecWithoutOutputCapture(); err != nil {
		return fmt.Errorf("unable to delete cluster %s in kind environment: %w", dkc.ClusterName, err)
	}

	return nil
}

func (dkc *DeleteKindCluster) ValidateParams(_ context.Context) error {
	out, _, err := kind.GetClusters().Exec(true, false)
	if err != nil {
		return fmt.Errorf("cannot fetch clusters in kind environment: %w", err)
	}

	allClusters := strings.Split(out, "\n")

	if !contains(allClusters, dkc.ClusterName) {
		return fmt.Errorf("cluster %s doesn't exist: %w", dkc.ClusterName, ErrClusterDoesntExists)
	}

	return nil
}
