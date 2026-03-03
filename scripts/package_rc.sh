#!/bin/bash

# Copyright 2023-Present Couchbase, Inc.
#
# Use of this software is governed by the Business Source License included in
# the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
# file, in accordance with the Business Source License, use of this software
# will be governed by the Apache License, Version 2.0, included in the file
# licenses/APL2.txt.

set -eu

#  ********************************************************************************* 
#  * WARNING * WARNING * WARNING * WARNING * WARNING * WARNING * WARNING * WARNING *
#  *********************************************************************************
#  This script expects you to have access to three things:
#      1.  You must have access to the docker repository that you are pulling the 
#          images from. You must perform a docker login to use.
#              `docker login "$REPOSITORY" --password-stdin --username "$USER"`
#          If you are using ghcr, your user is your github user and password is a
#          token generated from https://github.com/settings/tokens.  The token
#          requires read:packages permissions.
#      2.  You must have access to the bucket that you intend for the artifacts to
#          be placed in. It does not need to be public in anyway, as the script will
#          generate pre-signed urls for each artifact and place them in a links.txt
#          file it will put in a folder called "couchbase-autonomous-operator-$TAG"
#      3.  You must have access to latest builds.  This will require you to either
#          be on the corporate intranet, or the Couchbase VPN.  

IMAGES=("operator" "admission-controller" "exporter" "fluent-bit" "operator-certification" "operator-backup" "cloud-native-gateway")
PLATFORMS=("kubernetes" "openshift")
ARCH=("amd64" "arm64")
OS=("macos" "windows" "linux")
FOLDER="couchbase-autonomous-operator-"
DEFAULT_TAG=latest
BACKUP_TAG="latest"
DAC_TAG="$DEFAULT_TAG"
EXPORTER_TAG="$DEFAULT_TAG"
FLUENT_BIT_TAG="$DEFAULT_TAG"
CERT_TAG="$DEFAULT_TAG"
CNG_TAG="$DEFAULT_TAG"
OPERATOR_TAG="$DEFAULT_TAG"
EXPIRES="86400"
HELP=0
GA=0
ARM=0
CONTAINER_ARCH=amd64

while getopts r:o:e:f:b:g:t:x:hna flag
do
    case "${flag}" in
        r) REPOSITORY=${OPTARG};;
        o) BUCKET=${OPTARG};;
        e) EXPORTER_TAG=${OPTARG};;
        f) FLUENT_BIT_TAG=${OPTARG};;
        g) CNG_TAG=${OPTARG};;
        b) BACKUP_TAG=${OPTARG};;
        t) OPERATOR_TAG=${OPTARG};;
        x) EXPIRES=${OPTARG};;
        h) HELP=1;;
        n) GA=1;;
        a) ARM=1;;
        *) exit 1;;
    esac
done

CERT_TAG=${OPERATOR_TAG}
DAC_TAG=${OPERATOR_TAG}

if [[ "$HELP" == "1" ]]; then
echo "
********************************************************************************* 
* WARNING * WARNING * WARNING * WARNING * WARNING * WARNING * WARNING * WARNING *
*********************************************************************************
This script expects you to have access to three things:
    1.  You must have access to the docker repository that you are pulling the 
        images from. You must perform a docker login to use.
            'docker login \"REPOSITORY\" --password-stdin --username \"USER\"'
        If you are using ghcr, your user is your github user and password is a
        token generated from https://github.com/settings/tokens.  The token
        requires read:packages permissions.
    2.  You must have access to the bucket that you intend for the artifacts to
        be placed in. It does not need to be public in anyway, as the script will
        generate pre-signed urls for each artifact and place them in a links.txt
        file it will put in a folder called \"couchbase-autonomous-operator-TAG\"
    3.  You must have access to latest builds.  This will require you to either
        be on the corporate intranet, or the Couchbase VPN. 
********************************************************************************* 
* WARNING * WARNING * WARNING * WARNING * WARNING * WARNING * WARNING * WARNING *
*********************************************************************************
EXAMPLE:
./package_rc.sh  \ # The package to deliver
    -r ghcr.io/cb-vanilla \ # (REQUIRED) container registry
    -o operator-rc \ # (REQUIRED) bucket to place artifacts in
    -t 2.5.0-164 \ # (REQUIRED) Operator/DAC/Cert Container Tag to use (Must be in format major.minor.patch-buildnum)
    -b 1.3.5-101 \ # Operator-Backup Container tag to use - Default is 1.3.5-101 (No latest tag available)
    -e 1.0.10-101 \ # Exporter container tag to use - Default is 'latest'
    -f 1.2.6-100 \ # Fluent Bit container tag to use - Default is 'latest'
    -g 0.1.0-132 \ # Cloud Native Gateway tag to use - Default is 'latest'
    -x 86400 \ # Creates pre-signed URLS for the files for download that expire in N seconds - Default is '86400' (24hr)
    -n # drops the build number from the version.  Use this for 'release' binaries to give to the customer early.

To load the resulting image file simply use 'docker load -i couchbase-autonomous-operator_TAG-images.tar.gz'
"
exit 0
fi

if ! [[ "$OPERATOR_TAG" =~ ^[1,2,3]{1}\.[0-9]{1,2}\.[0-9]{1,2}-[1-9]{1}[0-9]{2}$ ]]; then
    echo "You must specify a tag for operator in the format VERSION-BUILDNUM"
    exit 1
fi

if [[ -z "$REPOSITORY" ]]; then
    echo "You must specify a container registry to pull docker images from"
    exit 1
fi

if [[ -z "$BUCKET" ]]; then
    echo "You must specify a S3 bucket to store artifacts in"
    exit 1
fi

if [[ "$ARM" == "1" ]]; then
    CONTAINER_ARCH=arm64
fi

WORK_DIR=$(mktemp -d)

echo "$WORK_DIR"

if [[ ! "$WORK_DIR" || ! -d "$WORK_DIR" ]]; then
    echo "Could not create temp dir."
    exit 1
fi

# deletes the temp directory
function cleanup {      
  rm -rf "$WORK_DIR"
}

# register the cleanup function to be called on the EXIT signal
trap cleanup EXIT

PULLED_IMAGES=""

for image in "${IMAGES[@]}"; do
    TAG="$DEFAULT_TAG"
    case "$image" in
        operator)
            TAG="$OPERATOR_TAG";;
        admission-controller)
            TAG="$DAC_TAG";;
        exporter)
            TAG="$EXPORTER_TAG";;
        fluent-bit)
            TAG="$FLUENT_BIT_TAG";;
        operator-certification)
            TAG="$CERT_TAG";;
        operator-backup)
            TAG="$BACKUP_TAG";;
        cloud-native-gateway)#
            TAG="$CNG_TAG"
    esac
    echo "Pulling $REPOSITORY/$image:$TAG"
    docker pull --quiet --platform "linux/$CONTAINER_ARCH" "$REPOSITORY/$image:$TAG" &
    PULLED_IMAGES="$PULLED_IMAGES $REPOSITORY/$image:$TAG"
done

wait # waiting on all the docker pull commands to finish
SPLIT_TAG=${OPERATOR_TAG/-/"/"}
VERSION=${OPERATOR_TAG%%-*}
OUTPUT="couchbase-autonomous-operator_${OPERATOR_TAG}-images-$CONTAINER_ARCH.tar.gz"

if [[ "$GA" == "1" ]]; then
    OUTPUT="couchbase-autonomous-operator_${VERSION}-images-$CONTAINER_ARCH.tar.gz"
    FOLDER="${FOLDER}${VERSION}"
else 
    FOLDER="${FOLDER}${OPERATOR_TAG}"
fi

PULLED_IMAGES=$(echo "$PULLED_IMAGES" | xargs)
CMD="docker save ${PULLED_IMAGES} | gzip > \"${WORK_DIR}/${OUTPUT}\""
echo "Saving docker images."
eval "${CMD}" &
echo "Docker images will be saved to $OUTPUT."

MAC_WIN_SUFFIX=".zip"
LINUX_SUFFIX=".tar.gz"

for platform in "${PLATFORMS[@]}"; do
    for os in "${OS[@]}"; do
        SUFFIX="$LINUX_SUFFIX"
        if [[ "$os" == "windows" || "$os" == "macos" ]]; then
            SUFFIX="$MAC_WIN_SUFFIX"
        fi
        for arch in "${ARCH[@]}"; do
            URL="https://latestbuilds.service.couchbase.com/builds/latestbuilds/couchbase-operator/${SPLIT_TAG}/couchbase-autonomous-operator_${OPERATOR_TAG}-${platform}-${os}-${arch}${SUFFIX}"
            echo "Downloading tools - $URL"
            if [[ "$GA" = "1" ]]; then
                curl -s --output "$WORK_DIR/couchbase-autonomous-operator_${VERSION}-${platform}-${os}-${arch}${SUFFIX}" "$URL" &
            else
                curl -O -s --output-dir "$WORK_DIR/" "$URL" &
            fi
        done
    done
done

wait # waiting on all the curl commands to finish

echo "Uploading files to s3 bucket $BUCKET and folder $FOLDER"

aws s3 sync "${WORK_DIR}/" "s3://${BUCKET}/${FOLDER}"
echo "Generating Pre-Signed URLS for download"

links=
for f in "$WORK_DIR"/*; do
    dl=$(aws s3 presign "s3://${BUCKET}/${FOLDER}/${f}" --expires-in "$EXPIRES")
    links="[$f|$dl] $links"
done

touch "$WORK_DIR/links.txt"
links=("$links")
for l in "${links[@]}"; do
    printf "%s\r\n" "$(basename $l)" >> "$WORK_DIR/links.txt" 
done

aws s3 cp "$WORK_DIR/links.txt" "s3://${BUCKET}/${FOLDER}/links.txt"

echo "Download Complete.  Use the following pre-signed url to download a list of links to remainder of artifacts"
aws s3 presign "s3://${BUCKET}/${FOLDER}/links.txt" --expires-in "$EXPIRES"