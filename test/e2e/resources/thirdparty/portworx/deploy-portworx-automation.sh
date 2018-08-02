#!/bin/sh

etcdEndpoint=$1
portworxClusterName=$2
kubeConfig=$3

echo "using etcd endpoint: $etcdEndpoint"

pxpath=$(pwd)/resources/thirdparty/portworx

cp $pxpath/portworx-provisioner-template.yaml $pxpath/portworx-provisioner.yaml
sed -ie "s/<<ETCD_ENDPOINT>>/$etcdEndpoint/g" $pxpath/portworx-provisioner.yaml
sed -ie "s/<<PORTWORX_CLUSTER_NAME>>/$portworxClusterName/g" $pxpath/portworx-provisioner.yaml
rm $pxpath/portworx-provisioner.yamle

if [ -n "$kubeConfig" ]; then
    param="--kubeconfig=$kubeConfig"
fi
kubectl $param create -f $pxpath/portworx-provisioner.yaml