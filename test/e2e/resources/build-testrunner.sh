#!/bin/bash

# Copyright 2018-Present Couchbase, Inc.
#
# Use of this software is governed by the Business Source License included in
# the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
# file, in accordance with the Business Source License, use of this software
# will be governed by the Apache License, Version 2.0, included in the file
# licenses/APL2.txt.

imageType=$1
testRunnerDockerImageName=$2
numNodes=$3
testrunnerBranch=master

cd ./testrunner/

if [ "$testRunnerDockerImageName" == "" ]; then
    echo "Exiting: Docker image name missing"
    exit 1
fi

case "$imageType" in
    "1node"|"4node")
        srcDockerFile="./Dockerfile.nNode"
        entryPointString="ENTRYPOINT [\"./entrypoint.sh\", \"$numNodes\"]"
        ;;
    "platform-cert")
        srcDockerFile="./Dockerfile.${imageType}"
        entryPointString="ENTRYPOINT [\"./entrypoint.sh\", \"$numNodes\"]"
        ;;
    "tpcc")
        srcDockerFile="./Dockerfile.${imageType}"
        entryPointString="ENTRYPOINT [\"./entrypoint.sh\", \"$numNodes\", \"5.5.1-3511\", \"3600\"]"
        ;;
    "*")
        echo "Exiting: Invalid imageType '$imageType'"
        exit 1
esac

cp $srcDockerFile ./Dockerfile
echo "$entryPointString" >> ./Dockerfile
dockerBuildArgs="--build-arg testrunnerBranch=$testrunnerBranch --build-arg numNodes=$numNodes"
docker build . $dockerBuildArgs -t $testRunnerDockerImageName
