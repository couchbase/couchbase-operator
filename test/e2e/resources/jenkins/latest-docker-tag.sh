#!/bin/bash
UNAME=$1
UPASS=$2
set -e
TOKEN=$(curl -s -H "Content-Type: application/json" -X POST -d '{"username": "'${UNAME}'", "password": "'${UPASS}'"}' https://hub.docker.com/v2/users/login/ | jq -r .token)
REPO_LIST=$(curl -s -H "Authorization: JWT ${TOKEN}" https://hub.docker.com/v2/repositories/couchbase/?page_size=100 | jq -r '.results|.[]|.name')
IMAGE_TAGS=$(curl -s -H "Authorization: JWT ${TOKEN}" https://hub.docker.com/v2/repositories/couchbase/couchbase-operator-internal/tags/?page_size=100 | jq -r '.results|.[]|.name')
for j in ${IMAGE_TAGS}
do
  FULL_IMAGE_LIST="${FULL_IMAGE_LIST} couchbase/couchbase-operator-internal:${j}"
done
arr=($FULL_IMAGE_LIST)
echo ${arr[0]}