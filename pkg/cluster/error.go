package cluster

import (
	"errors"
	"fmt"
)

var (
	errCreatedCluster  = errors.New("cluster failed to be created")
	errUnkownCreatePod = errors.New("unkown error occurred creating pod")
)

type (
	ErrSecretMissingUsername string
	ErrSecretMissingPassword string
	ErrCreatingPod           string
	ErrRunningPod            string
)

func (e ErrSecretMissingUsername) Error() string {
	return fmt.Sprintf("secret is missing username key: %s", e)
}

func (e ErrSecretMissingPassword) Error() string {
	return fmt.Sprintf("secret is missing password key: %s", e)
}

func (e ErrCreatingPod) Error() string {
	return fmt.Sprintf("failed to create pod: %s", e)
}

func (e ErrRunningPod) Error() string {
	return fmt.Sprintf("failed to run pod: %s", e)
}
