#!/bin/bash

# Copyright 2023-Present Couchbase, Inc.
#
# Use of this software is governed by the Business Source License included in
# the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
# file, in accordance with the Business Source License, use of this software
# will be governed by the Apache License, Version 2.0, included in the file
# licenses/APL2.txt.

set -eu

IMAGES=("operator" "admission-controller" "exporter" "fluent-bit" "operator-certification" "operator-backup" "cloud-native-gateway")

DEFAULT_TAG=latest
BACKUP_TAG="1.3.5-101"
DAC_TAG="$DEFAULT_TAG"
EXPORTER_TAG="$DEFAULT_TAG"
FLUENT_BIT_TAG="$DEFAULT_TAG"
CERT_TAG="$DEFAULT_TAG"
CNG_TAG="$DEFAULT_TAG"
OPERATOR_TAG="$DEFAULT_TAG"
HELP=0

while getopts r:s:o:u:d:e:f:c:b:g:t:h flag
do
    case "${flag}" in
        r) REPOSITORY=${OPTARG};;
        s) SECRET=${OPTARG};;
        u) USER=${OPTARG};;
        o) OUTPUT=${OPTARG};;
        d) DAC_TAG=${OPTARG};;
        e) EXPORTER_TAG=${OPTARG};;
        f) FLUENT_BIT_TAG=${OPTARG};;
        c) CERT_TAG=${OPTARG};;
        g) CNG_TAG=${OPTARG};;
        b) BACKUP_TAG=${OPTARG};;
        t) OPERATOR_TAG=${OPTARG};;
        h) HELP=1;;
        *) exit 1;;
    esac
done

if [[ "$HELP" == "1" ]]; then
echo "Retrieves docker images from private repository and packages them into a tar file with each side-car image
Defaults to :latest tag, but appropriate tags can be specified for each image
EXAMPLE:
./package_rc.sh  \ # The package to deliver
    -r ghcr.io/cb-vanilla \ #container registry
    -s <PullSecret> \ # pull secret can be created for ghcr.io here: https://github.com/settings/tokens
    -u <UserName> \ # Github username or docker username
    -o couchbase-operator-rc \ #output prefix.  Will be appended with '_YYYY-MM-DD.tar.gz'
    -d 2.5.0-164 \ # DAC Container Tag to use
    -c 2.5.0-164 \ # Certification Container Tag to use
    -t 2.5.0-164 \ # Operator Container Tag to use
    -b 1.3.5-101 \ # Operator-Backup Container tag to use
    -e 1.0.10-101 \ # Exporter container tag to use
    -f 1.2.6-100 \ # Fluent Bit container tag to use
    -g 0.1.0-132 # Cloud Native Gateway tag to use

To load the resulting file simply use 'docker load -i <generated>.tar.gz
"
exit 0
fi

echo "$SECRET" | docker login "$REPOSITORY" --password-stdin --username "$USER"

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
    docker pull "$REPOSITORY/$image:$TAG"
    #echo "Saving image tarball $REPOSITORY/$image:$TAG"
    PULLED_IMAGES="$PULLED_IMAGES $REPOSITORY/$image:$TAG"
done

TODAYS_DATE=$(date '+%Y-%m-%d')
OUTPUT="${OUTPUT}_${TODAYS_DATE}.tar.gz"
PULLED_IMAGES=$(echo "$PULLED_IMAGES" | xargs)
CMD="docker save ${PULLED_IMAGES} | gzip > ${OUTPUT}"

eval "${CMD}"