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
        ;;
    "platform-cert"|"tpcc")
        srcDockerFile="./Dockerfile.${imageType}"
        ;;
    "*")
        echo "Exiting: Invalid imageType '$imageType'"
        exit 1
esac

cp $srcDockerFile ./Dockerfile
echo "ENTRYPOINT [\"./entrypoint.sh\", \"$numNodes\"]" >> ./Dockerfile
dockerBuildArgs="--build-arg testrunnerBranch=$testrunnerBranch --build-arg numNodes=$numNodes"
docker build . $dockerBuildArgs -t $testRunnerDockerImageName
