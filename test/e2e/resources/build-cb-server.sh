#!/bin/bash

# Copyright 2018-Present Couchbase, Inc.
#
# Use of this software is governed by the Business Source License included in
# the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
# file, in accordance with the Business Source License, use of this software
# will be governed by the Apache License, Version 2.0, included in the file
# licenses/APL2.txt.

VERSION=${1:-5.5.0}
BUILD=${2:-2958}
FLAVOR=${3:-vulcan}
IMAGENAME=${4:-couchbase_${VERSION}-${BUILD}.centos7}

docker build -t base.centos7 ./cbserver/base

if [ "$BUILD" = "ga" ]; then
	docker build \
	--build-arg VERSION=${VERSION} \
	--build-arg BUILD_NO=${BUILD} \
	--build-arg FLAVOR=${FLAVOR} \
	--build-arg BUILD_PKG=couchbase-server-enterprise-$VERSION-centos7.x86_64.rpm \
	--build-arg BASE_URL=http://172.23.120.24/builds/releases/$VERSION/$BUILD_PKG \
	-t couchbase_${VERSION}-${BUILD}.centos7 ./cbserver/server
else
	docker build \
	--build-arg VERSION=${VERSION} \
	--build-arg BUILD_NO=${BUILD} \
	--build-arg FLAVOR=${FLAVOR} \
	-t ${IMAGENAME} ./cbserver/server
fi
