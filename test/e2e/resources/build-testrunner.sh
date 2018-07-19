#!/bin/bash

imageType=$1
testRunnerDockerImageName=$2

cd ./testrunner/

if [ "$imageType" == "" ]; then
    echo "Exiting: Number of nodes missing"
    exit 1
fi

if [ "$testRunnerDockerImageName" == "" ]; then
    echo "Exiting: Docker image name missing"
    exit 1
fi

if [ "$imageType" == "1node" ]; then
    cp ./Dockerfile.1node ./Dockerfile
    docker build . -t $testRunnerDockerImageName
fi
