#!/bin/sh

function exitOnError() {
    if [ $1 -ne 0 ] ; then
        echo "Exiting: $2"
        exit $1
    fi
}

function showFileContent() {
    echo "-File contents of $1-"
    cat $1
    echo "-End of $1 file-"
}

#set variables
targetCluster="kubernetes"
KUBENAMESPACE="default"
testRunnerBranch="vulcan"
dockerHub="ashwin2002"
numNodes=4
cloudClusterNodeIpList=$1
cloudClusterMasterNodeIp=$2
operatorVersion=$3

#show variables
echo "Using cloud space from '$targetCluster', namespace '$KUBENAMESPACE'"
echo "Cloud node IPs '$cloudClusterNodeIpList'"
echo "Couchbase-operator version '$operatorVersion'"
echo "Using testrunner branch '$testRunnerBranch' for testing '$numOfNodes' node cluster"
echo "Using docker hub account '$dockerHub'"

deploymentFile="../../../example/deployment.yaml"
secretFile="../../../example/secret.yaml"
roleBindingFile="./testrunner/default-cluster-role-binding.yaml"
cbClusterFile="./testrunner/tpcc/cb-cluster-4node.yaml"
testRunnerYamlFileName="./testrunner/tpcc/tpcc.yaml"
clusterName=$(grep "name:" $cbClusterFile | head -1 | xargs | cut -d' ' -f 2)

cbOperatorDockerImageName="couchbase/couchbase-operator-internal:$operatorVersion"
cbServerDockerImageName="couchbase/server:5.5.0-test"
testRunnerDockerImageName="${dockerHub}/testrunner-cloud:tpcc"

# Build required images #
#sh ./build-cb-server.sh "5.5.0" "2958" "vulcan" "${cbServerDockerImageName}"
#exitOnError $? "Unable to build cb server docker file"
sh ./build-testrunner.sh "tpcc" "${testRunnerDockerImageName} ${numNodes}"
exitOnError $? "Unable to build testrunner tpcc docker file"

#ship the docker images to the nodes

cbServerImageName=$(echo $cbServerDockerImageName | cut -d':' -f 1)
cbServerTagName=$(echo $cbServerDockerImageName | cut -d':' -f 2)
cbServerTarFileName="cbServerDockerImage.tar"
rm -f $cbServerTarFileName
echo "Creating docker image tar file '$cbServerTarFileName'"
docker save -o $cbServerTarFileName $cbServerDockerImageName
exitOnError $? "Unable to create tar of cb server docker image"

testrunnerImageName=$(echo $testRunnerDockerImageName | cut -d':' -f 1)
testrunnerTagName=$(echo $testRunnerDockerImageName | cut -d':' -f 2)
testrunnerTarFileName="testrunnerDockerImage.tar"
rm -f $testrunnerTarFileName
echo "Creating docker image tar file '$testrunnerTarFileName'"
docker save -o $testrunnerTarFileName $testRunnerDockerImageName
exitOnError $? "Unable to create tar of testrunner tpcc docker image"

for nodeIp in $cloudClusterNodeIpList
do
    echo "Deleting previous cb server docker image on: '$nodeIp'"
    sshpass -p "couchbase" ssh -o StrictHostKeyChecking=no -t root@$nodeIp docker images | grep $cbServerImageName | grep $cbServerTagName | awk '{print $3}' | xargs docker rmi -f
    echo "Deleting previous testrunner docker image on: '$nodeIp'"
    sshpass -p "couchbase" ssh -o StrictHostKeyChecking=no -t root@$nodeIp docker images | grep $testrunnerImageName | grep $testrunnerTagName | awk '{print $3}' | xargs docker rmi -f

    echo "Copying '$cbServerTarFileName' to '$nodeIp:/root/' path"
    sshpass -p "couchbase" scp $cbServerTarFileName root@$nodeIp:/root/
    echo "Copying '$testrunnerTarFileName' to '$nodeIp:/root/' path"
    sshpass -p "couchbase" scp $testrunnerTarFileName root@$nodeIp:/root/

    echo "Loading docker image from tar: '$cbServerTarFileName'"
    sshpass -p "couchbase" ssh -o StrictHostKeyChecking=no -t root@$nodeIp docker load -i /root/$cbServerTarFileName
    echo "Deleting docker image tar: '$cbServerTarFileName'"
    sshpass -p "couchbase" ssh -o StrictHostKeyChecking=no -t root@$nodeIp rm -f /root/$cbServerTarFileName
    echo "Loading docker image from tar: '$testrunnerTarFileName'"
    sshpass -p "couchbase" ssh -o StrictHostKeyChecking=no -t root@$nodeIp docker load -i /root/$testrunnerTarFileName
    echo "Deleting docker image tar: '$testrunnerTarFileName'"
    sshpass -p "couchbase" ssh -o StrictHostKeyChecking=no -t root@$nodeIp rm -f /root/$testrunnerTarFileName
done

echo "Deleting the tar file '$cbServerTarFileName'"
rm -f $cbServerTarFileName
echo "Deleting the tar file '$testrunnerTarFileName'"
rm -f $testrunnerTarFileName

echo "Creating secret"
showFileContent $secretFile
kubectl delete -f $secretFile &>/dev/null
kubectl create -f $secretFile
exitOnError $? "Unable to create secret"

echo "Making default SA cluster admin"
showFileContent $roleBindingFile
kubectl delete -f $roleBindingFile &>/dev/null
kubectl create -f $roleBindingFile
exitOnError $? "Unable to create role binding"

echo "Creating Couchbase Cluster"
showFileContent $cbClusterFile
kubectl delete -f $cbClusterFile &>/dev/null
kubectl create -f $cbClusterFile
exitOnError $? "Unable to create cb cluster"

sleep 180

echo "Pausing couchbase operator.."
sed -i "s/paused: false/paused: true/g" $cbClusterFile
exitOnError $? "Unable to replace string in cbcluster yaml"
showFileContent $cbClusterFile
kubectl --namespace=$KUBENAMESPACE apply -f $cbClusterFile
exitOnError $? "Unable to pause the cbcluster"

sleep 10

showFileContent $testRunnerYamlFileName
kubectl --namespace=$KUBENAMESPACE create -f $testRunnerYamlFileName
exitOnError $? "Unable to create testrunner"

echo "############################## Begin Test ##############################"

# wait for testrunner pod to be running
while true
    do
        testrunnerPodName=$(kubectl --namespace=$KUBENAMESPACE get -l job-name=testrunner-tpcc pods | tail -1 | awk '{print $1}')
        if [ "$testrunnerPodName" != "" ] ; then
            echo "Initializing pod '$testrunnerPodName'"
            for i in {1..300}
            do
                podRunning=$(kubectl --namespace=$KUBENAMESPACE describe pod $testrunnerPodName | grep "State:" | grep "Running" | wc -l | xargs )
                if [ $podRunning -eq 1 ] ; then
                    break
                fi
                sleep 1
            done

            if [ $podRunning -ne 1 ] ; then
                exitOnError 1 "Pod '$testrunnerPodName' not started running even after 5mins"
            fi
            unset podRunning
            break
        fi
done

# Redirect logs from testrunner pod
echo "----------- Logs from testrunner pod '$testrunnerPodName' -----------"
kubectl --namespace=$KUBENAMESPACE logs --follow=true $testrunnerPodName &

# Wait for testrunner job to complete
while true
do
    currTestrunnerPod=$(kubectl --namespace=$KUBENAMESPACE get -l job-name=testrunner-tpcc pods | tail -1 | awk '{print $1}')
    if [ "$currTestrunnerPod" != "$testrunnerPodName" ] ; then
        echo "job pod failed"
        kill %1
        kubectl delete job --all --namespace=$KUBENAMESPACE
        break
    fi

    isJobCompleted=$(kubectl --namespace=$KUBENAMESPACE logs $testrunnerPodName --tail=10 | grep "Testrunner: command completed" | wc -l)
    if [ $isJobCompleted -eq 1 ] ; then
        kill %1
        break
    fi
    sleep 10
done

echo ""
echo "Copying logs from testrunner pod for archiving"
masterNodeIp=$(echo $cloudClusterNodeIpList | cut -d" " -f 1)
testrunnerNodeIp=$(sshpass -p "couchbase" ssh -o StrictHostKeyChecking=no -t root@$masterNodeIp kubectl get pods -o wide \| grep "$testrunnerPodName" \| awk \'\{print \$6\}\')
workerNodeName=$(sshpass -p "couchbase" ssh -o StrictHostKeyChecking=no -t root@$masterNodeIp kubectl get pods -o wide \| grep "$testrunnerPodName" \| awk \'\{print \$7\}\')
workerNodeIpIndex=$(expr $(getWorkerNodeNum $workerNodeName) + 1)
targetWorkerIp=$(echo $cloudClusterNodeIpList | cut -d" " -f $workerNodeIpIndex)

echo "testrunnerNodeIp=$testrunnerNodeIp"
echo "workerNodeName=$workerNodeName"
echo "workerNodeIpIndex=$workerNodeIpIndex"
echo "masterNodeIp=$masterNodeIp"
echo "targetWorkerIp=$targetWorkerIp"

# Safely remove Ips from Known hosts file #
sed -i "/$targetWorkerIp /d" ~/.ssh/known_hosts
sshpass -p "couchbase" ssh -o StrictHostKeyChecking=no -t root@$targetWorkerIp "sed -i '/$testrunnerNodeIp /d' ~/.ssh/known_hosts"

sshpass -p "couchbase" ssh -o StrictHostKeyChecking=no -t root@$targetWorkerIp "sshpass -p 'couchbase' scp -o StrictHostKeyChecking=no -r root@$testrunnerNodeIp:/testrunner/logs /root/testrunnerLogs"
sshpass -p "couchbase" scp -o StrictHostKeyChecking=no -r root@$targetWorkerIp:/root/testrunnerLogs ${WORKSPACE}/logs
sshpass -p "couchbase" ssh -o StrictHostKeyChecking=no -t root@$targetWorkerIp "rm -rf /root/testrunnerLogs"

echo "################################# End Test ################################"
echo ""
