#!/usr/bin/env bash

BUILD_DIR=${WORKSPACE_DIR}/build

while [[ $# -gt 0 ]]
do
arg="$1"

case $arg in
    -p|--platform)
    PLATFORM="$2"
    shift
    shift
    ;;
    -o|--os)
    OS="$2"
    shift
    shift
    ;;
    -v|--version)
    VERSION="$2"
    shift
    shift
    ;;
    *)    # unknown option
    shift
    ;;
esac
done

case $OS in
    linux)
    NAME=couchbase-autonomous-operator-${PLATFORM}_${VERSION}_linux-x86_64
    ;;
    darwin)
    NAME=couchbase-autonomous-operator-${PLATFORM}_${VERSION}_macos-x86_64
    ;;
    windows)
    NAME=couchbase-autonomous-operator-${PLATFORM}_${VERSION}_windows-amd64
    ;;
    *)
    ;;
esac

mkdir -p ${BUILD_DIR}/${NAME}
cp $WORKSPACE_DIR/docs/user/modules/ROOT/examples/${PLATFORM}/* ${BUILD_DIR}/${NAME}
cp -r $BUILD_DIR/$OS/* ${BUILD_DIR}/${NAME}
tar -C ${BUILD_DIR} -czf ${BUILD_DIR}/${NAME}.tar.gz ${NAME}
rm -rf ${BUILD_DIR}/${NAME}

