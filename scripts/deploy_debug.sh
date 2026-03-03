#!/usr/bin/env bash

# Copyright 2021-Present Couchbase, Inc.
#
# Use of this software is governed by the Business Source License included in
# the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
# file, in accordance with the Business Source License, use of this software
# will be governed by the Apache License, Version 2.0, included in the file
# licenses/APL2.txt.

CLUSTER_NAME=${CLUSTER_NAME:-couchbase-debug}

API_SERVER_ADDRESS=${API_SERVER_ADDRESS-127.0.0.1}
API_SERVER_PORT=9090
# This allows the container tags to be explicitly set.
DOCKER_USER=couchbase
DOCKER_TAG=v1

# Delete the old cluster
kind delete cluster --name="${CLUSTER_NAME}"

# Set up KIND cluster with 3 worker nodes
CLUSTER_CONFIG=$(mktemp)

kind create cluster --name="${CLUSTER_NAME}" --config - <<EOF
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
networking:
  apiServerAddress: ${API_SERVER_ADDRESS}
  apiServerPort: ${API_SERVER_PORT}
EOF

scripts_dir=$( cd "$(dirname "${BASH_SOURCE[0]}")" ; pwd -P )

make binaries
# creates a docker container that runs operator via dlv
DOCKER_BUILDKIT=1 docker build -f ${scripts_dir}/../Dockerfile.debug -t ${DOCKER_USER}/couchbase-operator-debug:${DOCKER_TAG} .
DOCKER_BUILDKIT=1 docker build -f${scripts_dir}/../Dockerfile.admission -t ${DOCKER_USER}/couchbase-operator-admission:${DOCKER_TAG} .

# primes kind cluster and launches debug version of operator. requires port-forwarding 30123

kind --name="${CLUSTER_NAME}" load docker-image ${DOCKER_USER}/couchbase-operator-admission:${DOCKER_TAG}
kind --name="${CLUSTER_NAME}" load docker-image ${DOCKER_USER}/couchbase-operator-debug:${DOCKER_TAG}


# create crd
kubectl create -f ${scripts_dir}/../example/crd.yaml
# creates debug operator and admission
${scripts_dir}/../build/bin/cao create admission --image=${DOCKER_USER}/couchbase-operator-admission:${DOCKER_TAG}
${scripts_dir}/../build/bin/cao create operator --image=${DOCKER_USER}/couchbase-operator-debug:${DOCKER_TAG} --debug=true
