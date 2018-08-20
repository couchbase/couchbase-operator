#!/bin/bash

imageType=$1
testRunnerDockerImageName=$2
numNodes=$3
testrunnerBranch=master

cd ./testrunner/

if [ "$imageType" == "" ]; then
    echo "Exiting: image type missing"
    exit 1
fi

if [ "$testRunnerDockerImageName" == "" ]; then
    echo "Exiting: Docker image name missing"
    exit 1
fi

if [ "$imageType" == "1node" ]; then
    cp ./Dockerfile.nNode ./Dockerfile
fi

if [ "$imageType" == "4node" ]; then
    cp ./Dockerfile.nNode ./Dockerfile
fi

if [ "$imageType" == "platform-cert" ]; then
    cp ./Dockerfile.platform-cert ./Dockerfile
fi

if [ "$imageType" == "tpcc" ]; then
    cp ./Dockerfile.tpcc ./Dockerfile
fi

dockerBuildArgs="--build-arg testrunnerBranch=$testrunnerBranch --build-arg numNodes=$numNodes"
docker build . $dockerBuildArgs -t $testRunnerDockerImageName
