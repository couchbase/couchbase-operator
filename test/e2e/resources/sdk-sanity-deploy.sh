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
numNodes=4
cloudClusterNodeIpList=$1
cloudClusterMasterNodeIp=$2
dockerHub="ashwin2002"

#show variables
echo "Using cloud space from '$targetCluster', namespace '$KUBENAMESPACE'"
echo "Cloud node IPs '$cloudClusterNodeIpList'"
echo "Couchbase-server version '$cbServerVersionsToRun'"
echo "Couchbase-opeartor version '$cbOperatorVersion'"
echo "Using testrunner branch '$testRunnerBranch' for testing '$numOfNodes' node cluster"
echo "Using docker hub account '$dockerHub'"

# Create test.properties file contents
echo "KUBENAMESPACE=$KUBENAMESPACE" > ${WORKSPACE}/test.properties
echo "cbServerVersionsToRun=$cbServerVersionsToRun" >> ${WORKSPACE}/test.properties
echo "cbOperatorVersion=$cbOperatorVersion" >> ${WORKSPACE}/test.properties
echo "cbOperatorBranch=$cbOperatorBranch" >> ${WORKSPACE}/test.properties
echo "dockerHub=$dockerHub" >> ${WORKSPACE}/test.properties
echo "targetCluster=$targetCluster" >> ${WORKSPACE}/test.properties
echo "testRunnerBranch=$testRunnerBranch" >> ${WORKSPACE}/test.properties
echo "cloudClusterMasterNodeIp=$cloudClusterMasterNodeIp" >> ${WORKSPACE}/test.properties
echo "cloudClusterNodeIpList=$cloudClusterNodeIpList" >> ${WORKSPACE}/test.properties
showFileContent "${WORKSPACE}/test.properties"

deploymentFile="./testrunner/deployment.yaml"
secretFile="./testrunner/secret.yaml"
roleBindingFile="./testrunner/default-cluster-role-binding.yaml"
cbClusterFile="./sdk/sanity/cb-cluster-4node.yaml"
testRunnerYamlFileName="./sdk/sanity/sdk-sanity.yaml"
clusterName=$(grep "name:" $cbClusterFile | head -1 | xargs | cut -d' ' -f 2)

cbOperatorDockerImageName="couchbase/couchbase-operator-internal:$cbOperatorVersion"
cbServerDockerImageName="couchbase/server:5.5.0-test"
testRunnerDockerImageName="sdk-s435.sc.couchbase.com/sdkd-java-client:test"

# Build required images #
sh ./build-cb-server.sh "5.5.0" "2958" "vulcan" "${cbServerDockerImageName}"
exitOnError $? "Unable to build cb server docker file"
sh ./build-sdk.sh "sanity" "${testRunnerDockerImageName}"
exitOnError $? "Unable to build sdk docker file"

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
testrunnerTarFileName="sdkDockerImage.tar"
rm -f $testrunnerTarFileName
echo "Creating docker image tar file '$testrunnerTarFileName'"
docker save -o $testrunnerTarFileName $testRunnerDockerImageName
exitOnError $? "Unable to create tar of sdk docker image"

for nodeIp in $cloudClusterNodeIpList
do
    echo "Deleting previous cb server docker image on: '$nodeIp'"
    sshpass -p "couchbase" ssh -o StrictHostKeyChecking=no -t root@$nodeIp docker images | grep $cbServerImageName | grep $cbServerTagName | awk '{print $3}' | xargs docker rmi -f
    echo "Deleting previous sdk docker image on: '$nodeIp'"
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

echo "############################## Using couchbase-server '$cbServerVersionsToRun' ##############################"

# wait for testrunner pod to be running
while true
    do
        testrunnerPodName=$(kubectl --namespace=$KUBENAMESPACE get -l job-name=sdk-sanity pods | tail -1 | awk '{print $1}')
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
    currTestrunnerPod=$(kubectl --namespace=$KUBENAMESPACE get -l job-name=sdk-sanity pods | tail -1 | awk '{print $1}')
    if [ "$currTestrunnerPod" != "$testrunnerPodName" ] ; then
        echo "@@@@ Testrunner pod '$testrunnerPodName' replaced with new pod '$currTestrunnerPod' @@@@"
        kill %1
        kubectl --namespace=$KUBENAMESPACE delete pod $testrunnerPodName
        testrunnerPodName=$currTestrunnerPod
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
            exitOnError 1 "Pod '$1' not started running even after 5mins"
        fi
        unset podRunning
        echo "----------- Logs from new sdk pod '$testrunnerPodName' -----------"
        kubectl --namespace=$KUBENAMESPACE logs --follow=true $testrunnerPodName &
    fi

    isJobCompleted=$(kubectl --namespace=$KUBENAMESPACE logs $testrunnerPodName --tail=10 | grep "sdk: command completed" | wc -l)
    if [ $isJobCompleted -eq 1 ] ; then
        kill %1
        break
    fi
    sleep 10
done

echo "################################# End of test using '$cbServerVersionsToRun' ################################"
echo ""
