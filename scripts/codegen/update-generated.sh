#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

DOCKER_REPO_ROOT="/go/src/github.com/couchbaselabs/couchbase-operator"
IMAGE=${IMAGE:-"mikewied/k8s-code-gen"}

docker run --rm \
  -v "$PWD":"$DOCKER_REPO_ROOT" \
  -w "$DOCKER_REPO_ROOT" \
  "$IMAGE" \
  "./scripts/codegen/codegen.sh" \
  "all" \
  "github.com/couchbaselabs/couchbase-operator/pkg/generated" \
  "github.com/couchbaselabs/couchbase-operator/pkg/apis" \
  "couchbase:v1beta1" \
  --go-header-file "./scripts/codegen/boilerplate.go.txt" \
  $@
