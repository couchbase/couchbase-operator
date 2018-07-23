#!/bin/bash

# requires insecure registry: /etc/docker/daemon.json
#{
#    "insecure-registries" : [ "sdk-s435.sc.couchbase.com" ]
#}
#sudo systemctl daemon-reload
#sudo systemctl restart docker


imageType=$1
testRunnerDockerImageName=$2

docker rmi -f $(docker images -q sdk-s435.sc.couchbase.com/sdkd-java-client)
docker rmi -f $(docker images -q sdk-s435.sc.couchbase.com/sdkdc)

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
    docker tag $(docker images -q sdk-s435.sc.couchbase.com/sdkd-java-client) $testRunnerDockerImageName
fi
