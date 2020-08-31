#!/bin/bash
set -e
TOKEN=$1
PROJECT_ID=$2
length=$(curl -s --header "PRIVATE-TOKEN: $TOKEN" "https://gitlab.com/api/v4/projects/$PROJECT_ID/registry/repositories?tags=true" | jq .'[0].tags|length')

length=$((length-1))

latest=$(curl -s --header "PRIVATE-TOKEN: $TOKEN" "https://gitlab.com/api/v4/projects/$PROJECT_ID/registry/repositories?tags=true" | jq .'[0].tags['$length'].path')

echo "${latest}" | tr -d '"' | cut -d ":" -f2
