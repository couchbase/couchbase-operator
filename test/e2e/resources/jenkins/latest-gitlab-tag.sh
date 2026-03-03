#!/bin/bash

# Copyright 2020-Present Couchbase, Inc.
#
# Use of this software is governed by the Business Source License included in
# the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
# file, in accordance with the Business Source License, use of this software
# will be governed by the Apache License, Version 2.0, included in the file
# licenses/APL2.txt.

set -e
TOKEN=$1
PROJECT_ID=$2
length=$(curl -s --header "PRIVATE-TOKEN: $TOKEN" "https://gitlab.com/api/v4/projects/$PROJECT_ID/registry/repositories?tags=true" | jq .'[0].tags|length')

length=$((length-1))

latest=$(curl -s --header "PRIVATE-TOKEN: $TOKEN" "https://gitlab.com/api/v4/projects/$PROJECT_ID/registry/repositories?tags=true" | jq .'[0].tags['$length'].path')

echo "${latest}" | tr -d '"' | cut -d ":" -f2
