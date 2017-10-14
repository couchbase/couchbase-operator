package cluster

import (
	"errors"
	"fmt"
)

var (
	errCreatedCluster = errors.New("cluster failed to be created")
)

type (
	ErrSecretMissingUsername string
	ErrSecretMissingPassword string
)

func (e ErrSecretMissingUsername) Error() string {
	return fmt.Sprintf("secret is missing username key: %s", e)
}

func (e ErrSecretMissingPassword) Error() string {
	return fmt.Sprintf("secret is missing password key: %s", e)
}
