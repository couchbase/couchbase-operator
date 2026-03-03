# Copyright 2018-Present Couchbase, Inc.
#
# Use of this software is governed by the Business Source License included in
# the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
# file, in accordance with the Business Source License, use of this software
# will be governed by the Apache License, Version 2.0, included in the file
# licenses/APL2.txt.

import sys
from kubernetes import client, config


def main():
    currNameSpace = sys.argv[1]
    config.load_incluster_config()
    v1 = client.CoreV1Api()
    nodeList = v1.list_pod_for_all_namespaces(watch=False)

    for node in nodeList.items:
        if node.metadata.namespace == currNameSpace:
            print("%s %s" % (node.metadata.name, node.status.pod_ip))

if __name__ == '__main__':
    if len(sys.argv) != 2:
        print("Please provide namespace to process")
        sys.exit(1)

    main()
