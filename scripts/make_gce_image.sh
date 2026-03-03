#!/bin/bash

# Copyright 2023-Present Couchbase, Inc.
#
# Use of this software is governed by the Business Source License included in
# the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
# file, in accordance with the Business Source License, use of this software
# will be governed by the Apache License, Version 2.0, included in the file
# licenses/APL2.txt.

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