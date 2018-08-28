#!/bin/bash

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
