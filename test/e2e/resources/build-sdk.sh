#!/bin/bash

# Copyright 2018-Present Couchbase, Inc.
#
# Use of this software is governed by the Business Source License included in
# the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
# file, in accordance with the Business Source License, use of this software
# will be governed by the Apache License, Version 2.0, included in the file
# licenses/APL2.txt.

# requires insecure registry: /etc/docker/daemon.json
#{
#    "insecure-registries" : [ "dockerhub.build.couchbase.com" ]
#}
#sudo systemctl daemon-reload
#sudo systemctl restart docker


imageType=$1
testRunnerDockerImageName=$2

docker rmi -f $(docker images -q dockerhub.build.couchbase.com/sdkd-java-client)
docker rmi -f $(docker images -q dockerhub.build.couchbase.com/sdkdc)

cd ./sdk/

if [ "$imageType" == "" ]; then
    echo "Exiting: image type missing"
    exit 1
fi

if [ "$testRunnerDockerImageName" == "" ]; then
    echo "Exiting: Docker image name missing"
    exit 1
fi

git clone ssh://git@github.com/couchbaselabs/sdkqe-resource.git

if [ "$imageType" == "sanity" ]; then
    cd sdkqe-resource/dockerfiles/sdkqe/situational/sdkd-java-client
    sh build.sh
    docker tag $(docker images -q dockerhub.build.couchbase.com/sdkd-java-client) $testRunnerDockerImageName
fi
