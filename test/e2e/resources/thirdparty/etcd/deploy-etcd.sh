#!/bin/sh

sh etcd-create-role.sh
kubectl create -f ./etcd-deployment.yaml

# wait for etcd operator pod to be running
while true
    do
        etcdOperator=$(kubectl --namespace=default get -l name=etcd-operator pods | tail -1 | awk '{print $1}')
        if [ "$etcdOperator" != "" ] ; then
            echo "initializing etcd operator: '$etcdOperator'"
            for i in {1..300}
            do
                podRunning=$(kubectl --namespace=default describe pod $etcdOperator | grep "State:" | grep "Running" | wc -l | xargs )
                if [ $podRunning -eq 1 ] ; then
                    break
                fi
                sleep 1
            done

            if [ $podRunning -ne 1 ] ; then
                exitOnError 1 "etcd operator: '$etcdOperator' failed to deploy after 5mins"
            fi
            unset podRunning
            break
        fi
done

kubectl create -f ./etcd-cluster.yaml

# wait for etcd cluster pods to be running
while true
    do
        echo "initializing etcd cluster"
        for i in {1..300}
        do
            etcdPodsRunning=$(kubectl get pods -l app=etcd | grep "etcd-cluster" | grep "Running" | wc -l | xargs)
            if [ $etcdPodsRunning -eq 3 ] ; then
                break
            fi
            sleep 1
        done

        if [ $etcdPodsRunning -ne 3 ] ; then
                exitOnError 1 "etcd cluster failed to deploy after 5 minutes"
        fi
        unset etcdPodsRunning
        break
done