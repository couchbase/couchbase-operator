#!/bin/bash

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
