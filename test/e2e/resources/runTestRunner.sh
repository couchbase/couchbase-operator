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
targetCluster=kubernetes
namespace=default
testrunnerBranch=master
serverBranchName=vulcan
numNodes=1
cloudClusterNodeIpList=""
cloudClusterMasterNodeIp=""
dockerAccount=couchbase

while [ $# -ne 0 ]
do
    case "$1" in
    "--clusterType")
        targetCluster=$2
        shift ; shift
        ;;

    "--namespace")
        namespace=$2
        shift ; shift
        ;;

    "--numNodes")
        numNodes=$2
        shift ; shift
        ;;

    "--testrunnerBranch")
        testrunnerBranch=$2
        shift ; shift
        ;;

    "--clusterMasterIp")
        cloudClusterMasterNodeIp=$2
        shift ; shift
        ;;

    "--clusterNodeIp")
        cloudClusterNodeIpList=$2
        shift ; shift
        ;;

    "--dockerAccount")
        dockerAccount=$2
        shift ; shift
        ;;

    "--operatorVersion")        # Eg: 1.0.0-100
        operatorVersion=$2
        shift ; shift
        ;;

    "--serverVersion")          # Eg. 5.5.0, 6.0.0
        serverVersion=$2
        shift ; shift
        ;;

    "--serverBuildNum")         # Eg. 4239
        serverBuildNum=$2
        shift ; shift
        ;;

    "--serverBranchName")       # vulcan, alice
        serverBranchName=$2
        shift ; shift
        ;;
    *)
        echo "Error: Unsupported argument '$1'"
        exit 1
    esac
done

#show variables
echo "Using cloud space from '$targetCluster', namespace '$namespace'"
echo "Cloud node master: '$cloudClusterMasterNodeIp', workers: '$cloudClusterNodeIpList'"
echo "Using docker hub account '$dockerAccount'"
echo "Couchbase-operator version '$operatorVersion'"
echo "Couchbase-server version '$serverVersion', build '$serverBuildNum', branch '$serverBranchName'"
echo "Using testrunner branch '$testrunnerBranch' with '$numNodes' node cluster"

# Create test.properties file contents
echo "cbServerVersionsToRun=$cbServerVersionsToRun" >> ${WORKSPACE}/test.properties
echo "operatorVersion=$operatorVersion" >> ${WORKSPACE}/test.properties
echo "cbOperatorBranch=$cbOperatorBranch" >> ${WORKSPACE}/test.properties
echo "dockerAccount=$dockerAccount" >> ${WORKSPACE}/test.properties
echo "targetCluster=$targetCluster" >> ${WORKSPACE}/test.properties
echo "testrunnerBranch=$testrunnerBranch" >> ${WORKSPACE}/test.properties
echo "cloudClusterMasterNodeIp=$cloudClusterMasterNodeIp" >> ${WORKSPACE}/test.properties
echo "cloudClusterNodeIpList=$cloudClusterNodeIpList" >> ${WORKSPACE}/test.properties
showFileContent "${WORKSPACE}/test.properties"

deploymentFile="../../../example/deployment.yaml"
secretFile="../../../example/secret.yaml"
roleBindingFile="./testrunner/default-cluster-role-binding.yaml"
cbClusterFile="./testrunner/${numNodes}node/cb-cluster-${numNodes}node.yaml"
testRunnerYamlFileName="./testrunner/${numNodes}node/${numNodes}node-sanity.yaml"
clusterName=$(grep "name:" $cbClusterFile | head -1 | xargs | cut -d' ' -f 2)

cbOperatorDockerImageName="couchbase/couchbase-operator-internal:$operatorVersion"
cbServerDockerImageName="couchbase/server:${serverVersion}-test"
testRunnerDockerImageName="${dockerAccount}/testrunner-cloud:${numNodes}node"

# Build required images #
sh ./build-cb-server.sh "$serverVersion" "$serverBuildNum" "$serverBranchName" "${cbServerDockerImageName}"
exitOnError $? "Unable to build cb server docker file"
sh ./build-testrunner.sh "${numNodes}node" "${testRunnerDockerImageName}"
exitOnError $? "Unable to build testrunner ${numNodes}node docker file"

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
exitOnError $? "Unable to create tar of testrunner ${numNodes}node docker image"

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
kubectl --namespace=$namespace apply -f $cbClusterFile
exitOnError $? "Unable to pause the cbcluster"

sleep 10

showFileContent $testRunnerYamlFileName
kubectl --namespace=$namespace create -f $testRunnerYamlFileName
exitOnError $? "Unable to create testrunner"

echo "############################## Using couchbase-server '$cbServerVersionsToRun' ##############################"

# wait for testrunner pod to be running
while true
    do
        testrunnerPodName=$(kubectl --namespace=$namespace get -l job-name=testrunner-${numNodes}node-sanity pods | tail -1 | awk '{print $1}')
        if [ "$testrunnerPodName" != "" ] ; then
            echo "Initializing pod '$testrunnerPodName'"
            for i in {1..300}
            do
                podRunning=$(kubectl --namespace=$namespace describe pod $testrunnerPodName | grep "State:" | grep "Running" | wc -l | xargs )
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
kubectl --namespace=$namespace logs --follow=true $testrunnerPodName &

# Wait for testrunner job to complete
while true
do
    currTestrunnerPod=$(kubectl --namespace=$namespace get -l job-name=testrunner-${numNodes}node-sanity pods | tail -1 | awk '{print $1}')
    if [ "$currTestrunnerPod" != "$testrunnerPodName" ] ; then
        echo "job pod failed"
        kill %1
        kubectl delete job --all --namespace=$namespace
        break
    fi

    isJobCompleted=$(kubectl --namespace=$namespace logs $testrunnerPodName --tail=10 | grep "Testrunner: command completed" | wc -l)
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

echo "################################# End of test using '$cbServerVersionsToRun' ################################"
echo ""
