#!/bin/sh

etcdOperatorRunning=$(kubectl --namespace=default describe deployment -l name=etcd-operator | grep -w "Available" | grep "True" | wc -l | xargs)
etcdEndpointPresent=$(kubectl --namespace=default get services | grep "example-etcd-cluster-client" | wc -l | xargs)
etcdPodsRunning=$(kubectl get pods -l app=etcd | grep "etcd-cluster" | grep "Running" | wc -l | xargs)

etcdDeployed=true

if [ "$etcdOperatorRunning" -le 0 ];then
  echo "etcd operator not running running"
  etcdDeployed=false
fi

if [ "$etcdEndpointPresent" -le 0 ];then
  echo "etcd endpoints not present"
  etcdDeployed=false
fi

if [ "$etcdPodsRunning" -le 0 ];then
  echo "etcd cluster not running"
  etcdDeployed=false
fi

if [ "$etcdDeployed" = false ] ; then
    echo 'deploying etcd cluster'
    sh ../etcd/delete-etcd.sh
    sh ../etcd/deploy-etcd.sh
    etcdDeployed=true
fi

if [ "$etcdDeployed" = true ] ; then
    echo 'etcd cluster successfully deployed'
fi

echo 'deploying portworx'

portworxClusterName="portworx-cluster-$(uuidgen)"
etcdEndpoint=$(kubectl --namespace=default get services | grep "example-etcd-cluster-client" | tail -1 | awk '{print $3}')

sed -ie "s/<<ETCD_ENDPOINT>>/$etcdEndpoint/g" ./portworx-provisioner.yaml
sed -ie "s/<<PORTWORX_CLUSTER_NAME>>/$portworxClusterName/g" ./portworx-provisioner.yaml
rm ./portworx-provisioner.yamle
kubectl create -f ./portworx-provisioner.yaml