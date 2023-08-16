#!/bin/bash

set -ex

GOOGLE_APPLICATION_CREDENTIALS=
GCP_PROJECT_ID="couchbase-engineering"

while getopts c:p: flag
do
    case "${flag}" in
        c)  GOOGLE_APPLICATION_CREDENTIALS=${OPTARG};;
        p)  GCP_PROJECT_ID=${OPTARG};;
        *)
    esac
done

if [ -z "$GOOGLE_APPLICATION_CREDENTIALS" ]; then
    echo "Missing Google Application Credentials"
    exit 1
fi

WORKING_DIR="image-builder/images/capi/"

export GCP_PROJECT_ID
export GOOGLE_APPLICATION_CREDENTIALS

(cd ${WORKING_DIR} && exec make deps-gce)

(cd ${WORKING_DIR} && exec make build-gce-ubuntu-2204)

echo