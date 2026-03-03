#!/bin/bash

# Copyright 2018-Present Couchbase, Inc.
#
# Use of this software is governed by the Business Source License included in
# the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
# file, in accordance with the Business Source License, use of this software
# will be governed by the Apache License, Version 2.0, included in the file
# licenses/APL2.txt.

UNAME=$1
UPASS=$2
set -e

repoName="couchbase"
targetImage=$3
reqVersion=$4

if [[ -z $targetImage ]]; then
    echo "Exiting: Specify image name to get tag"
    exit 1
fi

TOKEN=$(curl -s -H "Content-Type: application/json" -X POST -d '{"username": "'${UNAME}'", "password": "'${UPASS}'"}' https://hub.docker.com/v2/users/login/ | jq -r .token)
REPO_LIST=$(curl -s -H "Authorization: JWT ${TOKEN}" https://hub.docker.com/v2/repositories/${repoName}/?page_size=100 | jq -r '.results|.[]|.name')
for imgName in ${REPO_LIST}
do
    if [ "$imgName" == "$targetImage" ]; then
        IMAGE_TAGS=$(curl -s -H "Authorization: JWT ${TOKEN}" https://hub.docker.com/v2/repositories/${repoName}/${imgName}/tags/?page_size=100 | jq -r '.results|.[]|.name')
        for imgTag in ${IMAGE_TAGS}
        do
            if [ -z $reqVersion ]; then
                FULL_IMAGE_LIST="${FULL_IMAGE_LIST} ${repoName}/${imgName}:${imgTag}"
            elif [[ "$imgTag" =~ ^"$reqVersion-"* ]]; then
                FULL_IMAGE_LIST="${FULL_IMAGE_LIST} ${repoName}/${imgName}:${imgTag}"
            fi
        done
    fi
done

arr=($FULL_IMAGE_LIST)
if [[ -z ${arr[0]} ]]; then
    exit 1
else
    echo ${arr[0]}
    exit 0
fi