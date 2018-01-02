package e2eutil

// error handling for util methods
import (
	"fmt"
)

func NewErrVerifyEditBucket(bucket string) error {
	return fmt.Errorf("failed during edit bucket verification: %s", bucket)
}

func NewErrGetClusterBucket(bucket string) error {
	return fmt.Errorf("failed to get bucket from cluster %s", bucket)
}

func NewErrGetBucketSpec(bucket string) error {
	return fmt.Errorf("failed to get spec for bucket %s", bucket)
}

func NewErrServerConfigNotFound(configName string) error {
	return fmt.Errorf("failed to find server config in spec %s", configName)
}
