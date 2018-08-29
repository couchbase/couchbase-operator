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
runType="nodeSanity"
targetCluster="kubernetes"
namespace="default"
testrunnerBranch="master"
serverBranchName="vulcan"
numNodes=1
cloudClusterNodeIpList=""
cloudClusterMasterNodeIp=""

sshArgs="-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null"
sshUser="root"
sshPassword="couchbase"
dockerAccount="couchbase"

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

    "--runType")
        runType=$2
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

# show variables
echo "Using cloud space from '$targetCluster', namespace '$namespace' to run '$runType'"
echo "Cloud node master: '$cloudClusterMasterNodeIp', workers: '$cloudClusterNodeIpList'"
echo "Using docker hub account '$dockerAccount'"
echo "Couchbase-operator version '$operatorVersion'"
echo "Couchbase-server version '$serverVersion', build '$serverBuildNum', branch '$serverBranchName'"
echo "Using testrunner branch '$testrunnerBranch' with '$numNodes' node cluster"

# Set target files and job name based on runType
case "$runType" in
    "nodeSanity")
        cbClusterFile="./testrunner/${numNodes}node/cb-cluster-${numNodes}node.yaml"
        testRunnerYamlFileName="./testrunner/${numNodes}node/${numNodes}node-sanity.yaml"
        testRunnerImgTag="${numNodes}node"
        jobName="testrunner-${numNodes}node-sanity"
        ;;
    "platform-cert"|"tpcc")
        cbClusterFile="./testrunner/${runType}/cb-cluster-${numNodes}node.yaml"
        testRunnerYamlFileName="./testrunner/${runType}/${runType}.yaml"
        testRunnerImgTag="$runType"
        jobName="testrunner-${runType}"
        ;;
    "sdk-sanity")
        cbClusterFile="./sdk/sanity/cb-cluster-${numNodes}node.yaml"
        testRunnerYamlFileName="./sdk/sanity/sdk-sanity.yaml"
        ;;
    "*")
        echo "Error: Invalid runType '$runType'"
        exit 1
esac

deploymentFile="../../../example/deployment.yaml"
secretFile="../../../example/secret.yaml"
roleBindingFile="./testrunner/default-cluster-role-binding.yaml"
clusterName=$(grep "name:" $cbClusterFile | head -1 | xargs | cut -d' ' -f 2)

cbOperatorDockerImageName="couchbase/couchbase-operator-internal:$operatorVersion"
cbServerDockerImageName="couchbase/server:${serverVersion}-test"
testRunnerDockerImageName="${dockerAccount}/testrunner-cloud:$testRunnerImgTag"

# Build required images #
sh ./build-cb-server.sh "$serverVersion" "$serverBuildNum" "$serverBranchName" "${cbServerDockerImageName}"
exitOnError $? "Unable to build cb server docker file"
sh ./build-testrunner.sh "$testRunnerImgTag" "$testRunnerDockerImageName" "$numNodes"
exitOnError $? "Unable to build testrunner $testRunnerImgTag docker file"

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
exitOnError $? "Unable to create tar of testrunner $testRunnerImgTag docker image"

for nodeIp in $cloudClusterNodeIpList
do
    echo "Deleting previous cb server, testrunner docker image on: '$nodeIp'"
    sshpass -p "$sshPassword" ssh $sshArgs -t $sshUser@$nodeIp docker images | grep $cbServerImageName | grep $cbServerTagName | awk '{print $3}' | xargs docker rmi -f
    sshpass -p "$sshPassword" ssh $sshArgs -t $sshUser@$nodeIp docker images | grep $testrunnerImageName | grep $testrunnerTagName | awk '{print $3}' | xargs docker rmi -f

    echo "Copying '$cbServerTarFileName' & '$testrunnerTarFileName' to '$nodeIp:~/' path"
    sshpass -p "$sshPassword" scp $sshArgs $cbServerTarFileName $testrunnerTarFileName $sshUser@$nodeIp:~/

    echo "Loading docker image from '$cbServerTarFileName' & '$testrunnerTarFileName'"
    sshpass -p "$sshPassword" ssh $sshArgs -t $sshUser@$nodeIp "docker load -i ~/$cbServerTarFileName ; rm -f ~/$cbServerTarFileName"
    sshpass -p "$sshPassword" ssh $sshArgs -t $sshUser@$nodeIp "docker load -i ~/$testrunnerTarFileName ; rm -f ~/$testrunnerTarFileName"
done

echo "Deleting files '$cbServerTarFileName' & '$testrunnerTarFileName'"
rm -f $cbServerTarFileName $testrunnerTarFileName

# Set proper couchbase server image in cb cluster yaml
serverVer=$(echo $cbServerDockerImageName | cut -d':' -f 2)
sed -i "s/version:.*\$/version: $serverVer/g" $cbClusterFile

echo "Setting cb cluster to pause=false"
sed -i "s/paused: .*\$/paused: false/g" $cbClusterFile
exitOnError $? "Unable to replace string in cbcluster yaml"

echo "Creating secret"
showFileContent $secretFile
kubectl --namespace=$namespace delete -f $secretFile &>/dev/null
kubectl --namespace=$namespace create -f $secretFile
exitOnError $? "Unable to create secret"

echo "Making default SA cluster admin"
showFileContent $roleBindingFile
kubectl --namespace=$namespace delete -f $roleBindingFile &>/dev/null
kubectl --namespace=$namespace create -f $roleBindingFile
exitOnError $? "Unable to create role binding"

echo "Creating Couchbase Cluster"
showFileContent $cbClusterFile
kubectl --namespace=$namespace delete -f $cbClusterFile &>/dev/null
kubectl --namespace=$namespace create -f $cbClusterFile
exitOnError $? "Unable to create cb cluster"

echo "Waiting for cluster size to reach $numNodes nodes"
for index in {1..60}
do
    podLen=$(kubectl --namespace=$namespace get pods -l couchbase_cluster=cb-example -o json | jq '.items|length')
    if [ $podLen -eq $numNodes ]; then
        # Sleep for last pod to be added into cluster and rebalanced in
        sleep 60
        break
    fi
    sleep 15
done

if [ $podLen -ne $numNodes ]; then
    echo "Error: All cb server pods not yet initialized"
    kubectl --namespace=$namespace get pods
    exit 1
fi

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

echo "############################## Using couchbase-server '$serverVersion' ##############################"

# wait for testrunner pod to be running
while true
    do
        testrunnerPodName=$(kubectl --namespace=$namespace get -l job-name=$jobName pods | tail -1 | awk '{print $1}')
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
    currTestrunnerPod=$(kubectl --namespace=$namespace get -l job-name=$jobName pods | tail -1 | awk '{print $1}')
    if [ "$currTestrunnerPod" != "$testrunnerPodName" ] ; then
        echo "job pod failed"
        kill %1
        kubectl --namespace=$namespace delete job --all
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
testrunnerNodeIp=$(sshpass -p "$sshPassword" ssh $sshArgs -t $sshUser@$masterNodeIp kubectl --namespace=$namespace get pods -o wide \| grep "$testrunnerPodName" \| awk \'\{print \$6\}\')
workerNodeName=$(sshpass -p "$sshPassword" ssh $sshArgs -t $sshUser@$masterNodeIp kubectl --namespace=$namespace get pods -o wide \| grep "$testrunnerPodName" \| awk \'\{print \$7\}\')
workerNodeIpIndex=$(expr $(getWorkerNodeNum $workerNodeName) + 1)
targetWorkerIp=$(echo $cloudClusterNodeIpList | cut -d" " -f $workerNodeIpIndex)

echo "testrunnerNodeIp=$testrunnerNodeIp"
echo "workerNodeName=$workerNodeName"
echo "workerNodeIpIndex=$workerNodeIpIndex"
echo "masterNodeIp=$masterNodeIp"
echo "targetWorkerIp=$targetWorkerIp"

# Safely remove Ips from Known hosts file #
sshpass -p "$sshPassword" ssh $sshArgs -t $sshUser@$targetWorkerIp "sed -i '/$testrunnerNodeIp /d' ~/.ssh/known_hosts"

sshpass -p "$sshPassword" ssh $sshArgs -t $sshUser@$targetWorkerIp "sshpass -p '$sshPassword' scp -o StrictHostKeyChecking=no -r $sshUser@$testrunnerNodeIp:/testrunner/logs ~/testrunnerLogs"
sshpass -p "$sshPassword" scp $sshArgs -r $sshUser@$targetWorkerIp:~/testrunnerLogs ${WORKSPACE}/logs
sshpass -p "$sshPassword" ssh $sshArgs -t $sshUser@$targetWorkerIp "rm -rf ~/testrunnerLogs"

echo "################################# End of test using '$serverVersion' ################################"
echo ""
