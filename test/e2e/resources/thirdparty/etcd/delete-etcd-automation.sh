#!/bin/sh

kubeConfig=$1

etcdpath=$(pwd)/resources/thirdparty/etcd
param="--kubeconfig=$kubeConfig"

kubectl $param delete -f $etcdpath/etcd-deployment.yaml
kubectl $param delete endpoints etcd-operator
kubectl $param delete crd etcdclusters.etcd.database.coreos.com
kubectl $param delete clusterrole etcd-operator
kubectl $param delete clusterrolebinding etcd-operator