#!/bin/bash

imageType=$1
testRunnerDockerImageName=$2

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
    cp ./Dockerfile.1node ./Dockerfile
fi

if [ "$imageType" == "4node" ]; then
    cp ./Dockerfile.4node ./Dockerfile
fi

if [ "$imageType" == "platform-cert" ]; then
    cp ./Dockerfile.platform-cert ./Dockerfile
fi

if [ "$imageType" == "tpcc" ]; then
    cp ./Dockerfile.tpcc ./Dockerfile
fi

docker build . -t $testRunnerDockerImageName
