#!/bin/bash

SUPPORT_PKG=cb-k8s-support-`date +%m%d%Y-%T | sed  "s/:/_/g"`
KUBECTL="kubectl $@"
OUTPUT_DIR=/tmp/$SUPPORT_PKG
mkdir -p $OUTPUT_DIR

# cluster debugging and diagnosis
CLUSTER_LOG_DIR=$OUTPUT_DIR/cluster
mkdir $CLUSTER_LOG_DIR
echo "collecting cluster info"
$KUBECTL cluster-info dump > $CLUSTER_LOG_DIR/cluster_info.logs
$KUBECTL describe nodes > $CLUSTER_LOG_DIR/describe_nodes.logs
$KUBECTL top node > $CLUSTER_LOG_DIR/top_nodes.logs   2>&1
$KUBECTL top pod > $CLUSTER_LOG_DIR/top_pods.logs   2>&1

# info about pods, deployments, replicasets, and statefulsets
RESOURCE_LOG_DIR=$OUTPUT_DIR/resources
mkdir $RESOURCE_LOG_DIR
echo "collecting info about cluster objects"
$KUBECTL describe --show-events --all-namespaces po > $RESOURCE_LOG_DIR/pods.logs
$KUBECTL describe --show-events --all-namespaces deployment > $RESOURCE_LOG_DIR/deployment.logs
$KUBECTL describe --show-events --all-namespaces rs > $RESOURCE_LOG_DIR/repliaset.logs
$KUBECTL describe --show-events --all-namespaces statefulset > $RESOURCE_LOG_DIR/statefulsets.logs
$KUBECTL describe --show-events --all-namespaces couchbasecluster > $RESOURCE_LOG_DIR/couchbase.logs

# list of all running resources, 'roles','limits'...
$KUBECTL get --all-namespaces all -o yaml > $RESOURCE_LOG_DIR/all_resources.logs

# all container logs
CONTAINER_LOG_DIR=$OUTPUT_DIR/containers
mkdir $CONTAINER_LOG_DIR
echo "collecting container logs"
for ns in $( $KUBECTL get namespaces -o name | sed 's/.*\///' ); do
  for pod in $( $KUBECTL get -n $ns po -o name | sed 's/.*\///' ); do
      pod_log_path=$CONTAINER_LOG_DIR/$ns/$pod
      mkdir -p $pod_log_path
      # print logs of active container within pod
      $KUBECTL describe -n $ns po/$pod  | grep -B1 "Container ID" | grep ":$" | sed 's/://' | xargs -I '{}' $KUBECTL -n $ns logs $pod --timestamps=true  -c '{}' >>  $pod_log_path/output.log
      # print logs of pervious container within pod
      $KUBECTL describe -n $ns po/$pod  | grep -B1 "Container ID" | grep ":$" | sed 's/://' | xargs -I '{}' $KUBECTL -n $ns logs $pod --timestamps=true  -p -c '{}' >>  $pod_log_path/previous.logs  2>&1
  done
done

# services info
SERVICE_LOG_DIR=$OUTPUT_DIR/services
mkdir $SERVICE_LOG_DIR
echo "collecting service logs"
$KUBECTL describe --all-namespaces --show-events=true  endpoints > $SERVICE_LOG_DIR/endpoints.log
$KUBECTL describe --all-namespaces --show-events=true  svc > $SERVICE_LOG_DIR/services.log
$KUBECTL describe --all-namespaces --show-events=true  ing > $SERVICE_LOG_DIR/ingress.log


tar -czf $SUPPORT_PKG.tgz -C /tmp $SUPPORT_PKG
rm -rf $OUTPUT_DIR
echo "Done! `pwd`/$SUPPORT_PKG.tgz"
