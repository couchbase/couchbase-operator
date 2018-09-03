#!/bin/sh

kubeConfig=$HOME/.kube/config
namespace=default

while [ $# -ne 0 ]
do
    case "$1" in
    "--kubeConfig")
        kubeConfig=$2
        shift ; shift
        ;;
    "--namespace")
        namespace=$2
        shift ; shift
        ;;
    esac
done

etcdpath=$(pwd)/resources/thirdparty/etcd
param="--kubeconfig=$kubeConfig --namespace=$namespace"

kubectl $param delete etcdcluster example-etcd-cluster
kubectl $param delete -f $etcdpath/etcd-cluster.yaml
kubectl $param delete -f $etcdpath/etcd-deployment.yaml
kubectl $param delete endpoints etcd-operator
kubectl $param delete crd etcdclusters.etcd.database.coreos.com
kubectl $param delete clusterrole etcd-operator
kubectl $param delete clusterrolebinding etcd-operator
