#!/bin/bash

SUPPORT_PKG=cb-k8s-support
OUTPUT_DIR=/tmp/$SUPPORT_PKG
mkdir -p $OUTPUT_DIR

# cluster debugging and diagnosis
echo "collecting cluster info"
kubectl cluster-info dump > $OUTPUT_DIR/cluster_info.logs
kubectl describe nodes > $OUTPUT_DIR/describe_nodes.logs
kubectl top node > $OUTPUT_DIR/top_nodes.logs   2>&1
kubectl top pod > $OUTPUT_DIR/top_pods.logs   2>&1

# info about pods, deployments, replicasets, and statefulsets
echo "collecting info about cluster objects"
kubectl describe --show-events --all-namespaces po > $OUTPUT_DIR/describe_pods.logs
kubectl describe --show-events --all-namespaces deployment > $OUTPUT_DIR/describe_deployment.logs
kubectl describe --show-events --all-namespaces rs > $OUTPUT_DIR/describe_rs.logs
kubectl describe --show-events --all-namespaces statefulset > $OUTPUT_DIR/describe_statefulsets.logs
kubectl describe --show-events --all-namespaces couchbasecluster > $OUTPUT_DIR/describe_couchbase.logs

# all container logs
CONTAINER_LOG_DIR=$OUTPUT_DIR/containers
mkdir $CONTAINER_LOG_DIR
echo "collecting container logs"
for ns in $( kubectl get namespaces -o name | sed 's/.*\///' ); do
  for pod in $( kubectl get -n $ns po -o name | sed 's/.*\///' ); do
      pod_log_path=$CONTAINER_LOG_DIR/$ns/$pod
      mkdir -p $pod_log_path
      # print logs of active container within pod
      kubectl describe -n $ns po/$pod  | grep -B1 "Container ID" | grep ":$" | sed 's/://' | xargs -I '{}' kubectl -n $ns logs $pod --timestamps=true  -c '{}' >>  $pod_log_path/output.log
      # print logs of pervious container within pod
      kubectl describe -n $ns po/$pod  | grep -B1 "Container ID" | grep ":$" | sed 's/://' | xargs -I '{}' kubectl -n $ns logs $pod --timestamps=true  -p -c '{}' >>  $pod_log_path/previous.logs  2>&1
  done
done

tar -czf $SUPPORT_PKG.tgz -C /tmp $SUPPORT_PKG
rm -rf $OUTPUT_DIR
echo "Done! `pwd`/$SUPPORT_PKG.tgz"
