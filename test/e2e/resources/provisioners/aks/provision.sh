#!/bin/bash

# you must be signed into azure before using this script
# az login -u USERNAME -p PASSWORD

# example usage

# create single cluster
# sh provision.sh --action=create --type=single-cluster --resource-group=ao-dev-rg

# create 2 XDCR-ready clusters for testing
# sh provision.sh --action=create --type=test-clusters --resource-group=ao-testing-rg

function print_usage() {
  echo "$(basename "$0") - Create AKS Clusters for testing the Autonomous Operator
Usage: $(basename "$0") [options...]
Options:
  --action=STRING               Action to take: create or delete
  --type=STRING                 The resource type to take the action against: single-cluster, test-clusters
  --resource-group=STRING       Azure resource group to use
  --nodes-per-cluster=INT       Number of nodes to spin up per cluster
  --location=STRING             Azure location to use: westus, eastus, etc.
" >&2
}

function exitOnError() {
    if [ $1 -ne 0 ] ; then
        echo "Exiting: $2"
        exit $1
    fi
}

# input: resource group name
function checkResourceGroup(){
    az group show --name $1
    if [ $? -eq 0 ] ; then
        echo "Exiting: resource group already exists, please use a new resource group"
        exit 1
    fi
}

# input resource group name, vnet name, subnet name, address prefix, subnet prefix
function createVirtualNetwork(){
    az network vnet create -g $1 \
                           -n $2 \
                           --address-prefix $4 \
                           --subnet-name $3  \
                           --subnet-prefix $5
    exitOnError $? "Fail: error creating virtual network: $2"
}

# input: resource group name, location, nodes per cluster, subscription id, vnet name,
# subnet name, dns service ip, service cidr, cluster name, vm size, kubernetes version
function createCluster(){
    az aks create --name $9 \
                  --resource-group $1 \
                  --dns-name-prefix ao-testing \
                  --dns-service-ip $7 \
                  --docker-bridge-address 172.17.0.1/16 \
                  --kubernetes-version ${11} \
                  --location $2 \
                  --no-ssh-key \
                  --enable-addons http_application_routing \
                  --node-count $3 \
                  --node-osdisk-size 30 \
                  --node-vm-size ${10} \
                  --service-cidr $8 \
                  --vnet-subnet-id /subscriptions/$4/resourceGroups/$1/providers/Microsoft.Network/virtualNetworks/$5/subnets/$6 \
                  --subscription $4
    exitOnError $? "Fail: error creating aks cluster: cluster-1"
}

# input: resource group, subcription id, network security group name
function openInboundTraffic(){
    az network nsg rule create --name ALLOW_ALL_INBOUND \
                               --nsg-name $3 \
                               --priority 105 \
                               --resource-group $1 \
                               --access Allow \
                               --description "Allow all inbound" \
                               --destination-address-prefixes '*' \
                               --destination-port-ranges '*' \
                               --direction Inbound \
                               --source-address-prefixes '*' \
                               --source-port-ranges '*' \
                               --subscription $2
    exitOnError $? "Fail: error creating creating inbound traffic rule for cluster 1"
}

# input resource group, subscription id, network security group name
function openOutboundTraffic(){
    az network nsg rule create --name ALLOW_ALL_OUTBOUND \
                               --nsg-name $3 \
                               --priority 105 --resource-group $1 \
                               --access Allow \
                               --description "Allow all outbound" \
                               --destination-address-prefixes '*' \
                               --destination-port-ranges '*' \
                               --direction Outbound \
                               --source-address-prefixes '*' \
                               --source-port-ranges '*' \
                               --subscription $2
    exitOnError $? "Fail: error creating creating outbound traffic rule for cluster 1"
}

# input resource group, subscription id, cluster name, kubeconfig file path
function grabKubeConfig(){
    az aks get-credentials --name $3 \
                           --resource-group $1 \
                           --file $4 \
                           --subscription $2 \
                           --overwrite-existing
    exitOnError $? "Fail: error grabbing kubeconfig for $3"
}

# input: resource group, subscription id, peering name, vnet name, remote vnet name
function createPeering(){
    az network vnet peering create --name $3 \
                                   --remote-vnet /subscriptions/$2/resourceGroups/$1/providers/Microsoft.Network/virtualNetworks/$5 \
                                   --resource-group $1 \
                                   --vnet-name $4 \
                                   --subscription $2
    exitOnError $? "Fail: error creating network peer: $3"
}


# setting defaults for optional parameters
LOCATION=${LOCATION:-westus}
NODESPERCLUSTER=${NODESPERCLUSTER:-4}
VNETNAME1=${VNETNAME1:-vnet-1}
VNETSUBNETNAME1=${VNETSUBNETNAME1:-vnet-1-subnet}
ADDRESSPREFIX1=${ADDRESSPREFIX1:-10.0.0.0/12}
SUBNETPREFIX1=${SUBNETPREFIX1:-10.8.0.0/16}
DNSSERVICEIP1=${DNSSERVICEIP1:-10.0.0.10}
ADDRESSCIDR1=${ADDRESSCIDR1:-10.0.0.0/16}
CLUSTERNAME1=${CLUSTERNAME1:-cluster-1}
KUBECONFIGPATH1=${KUBECONFIGPATH1:-'~/.kube/config_aks_cluster_1'}
VMSIZE1=${VMSIZE1:-Standard_D8s_v3}
K8VERSION1=${K8VERSION1:-1.11.5}

VNETNAME2=${VNETNAME2:-vnet-2}
VNETSUBNETNAME2=${VNETSUBNETNAME2:-vnet-2-subnet}
ADDRESSPREFIX2=${ADDRESSPREFIX2:-10.16.0.0/12}
SUBNETPREFIX2=${SUBNETPREFIX2:-10.24.0.0/16}
DNSSERVICEIP2=${DNSSERVICEIP2:-10.16.0.10}
ADDRESSCIDR2=${ADDRESSCIDR2:-10.16.0.0/16}
CLUSTERNAME2=${CLUSTERNAME2:-cluster-2}
KUBECONFIGPATH2=${KUBECONFIGPATH2:-'~/.kube/config_aks_cluster_2'}
VMSIZE2=${VMSIZE2:-Standard_D8s_v3}
K8VERSION2=${K8VERSION2:-1.11.5}

PEERINGNAME1=${PEERINGNAME1:-vnet-1-to-vnet-2}
PEERINGNAME2=${PEERINGNAME2:-vnet-2-to-vnet-1}

TYPE=${TYPE:-single-cluster}

for i in "$@"
do
case $i in
    --action=*)
    ACTION="${i#*=}"
    ;;
    --type=*)
    TYPE="${i#*=}"
    ;;
    --resource-group=*)
    RESOURCEGROUP="${i#*=}"
    ;;
    --nodes-per-cluster=*)
    NODESPERCLUSTER="${i#*=}"
    ;;
    --location=*)
    LOCATION="${i#*=}"
    ;;
    --kube-config-path1=*)
    KUBECONFIGPATH1="${i#*=}"
    ;;
    --kube-config-path2=*)
    KUBECONFIGPATH2="${i#*=}"
    ;;
    --vm-size1=*)
    VMSIZE1="${i#*=}"
    ;;
    --vm-size2=*)
    VMSIZE2="${i#*=}"
    ;;
    --kubernetes-version1=*)
    K8VERSION1="${i#*=}"
    ;;
    --kubernetes-version2=*)
    K8VERSION2="${i#*=}"
    ;;
    -h|--help)
      print_usage
      exit 0
    ;;
    *)
      print_usage
      exit 1
    ;;
esac
done

# required input parameters
if [ -z "$ACTION" ]
  then
    echo "No action given"
    exit 1
fi

case "$ACTION" in
    "create")
        if [ -z "$TYPE" ]
          then
            echo "No type given"
            exit 1
        fi
        if [ -z "$RESOURCEGROUP" ]
          then
            echo "No resource group given"
            exit 1
        fi
        case "$TYPE" in
            "single-cluster")
                echo "ACTION=$ACTION : TYPE=$TYPE - creating kubernetes cluster..."
                # grabbing subscription id
                SUBSCRIPTIONID=$(az account list | jq '.[0].id' | tr -d '"')
                exitOnError $? "Fail: error grabbing subscription id, maybe you are not signed in to azure"

                # create resource group
                checkResourceGroup $RESOURCEGROUP
                echo "creating resource group: $RESOURCEGROUP..."

                az group create -l $LOCATION -n $RESOURCEGROUP
                exitOnError $? "Fail: error creating resource group: $RESOURCEGROUP"
                echo "Success: created resource group: $RESOURCEGROUP"

                # create vnet 1
                echo "creating virtual network: $VNETNAME1..."
                createVirtualNetwork $RESOURCEGROUP $VNETNAME1 $VNETSUBNETNAME1 $ADDRESSPREFIX1 $SUBNETPREFIX1
                echo "Success: created virtual network: $VNETNAME1"

                # create cluster 1
                echo "creating aks cluster: $CLUSTERNAME1"
                createCluster $RESOURCEGROUP $LOCATION $NODESPERCLUSTER $SUBSCRIPTIONID $VNETNAME1 $VNETSUBNETNAME1 $DNSSERVICEIP1 $ADDRESSCIDR1 $CLUSTERNAME1 $VMSIZE1 $K8VERSION1
                echo "Success: created aks cluster: $CLUSTERNAME1"

                # open inbound traffic to cluster 1
                echo "creating inbound traffic rule for $CLUSTERNAME1"
                MANAGEDRESOURCEGROUP1=MC_${RESOURCEGROUP}_${CLUSTERNAME1}_${LOCATION}
                NSG1=$(az network nsg list --resource-group $MANAGEDRESOURCEGROUP1 | jq '.[0].name' | tr -d '"')
                openInboundTraffic $MANAGEDRESOURCEGROUP1 $SUBSCRIPTIONID $NSG1
                echo "Success: created inbound traffic rule for $CLUSTERNAME1"

                # open outbound traffic from cluster 1
                echo "creating outbound traffic rule for $CLUSTERNAME1"
                openOutboundTraffic $MANAGEDRESOURCEGROUP $SUBSCRIPTIONID $NSG1
                echo "Success: created outbound traffic rule for $CLUSTERNAME1"

                # get kubeconfig for cluster 1
                echo "grabbing kubeconfig for $CLUSTERNAME1"
                grabKubeConfig $RESOURCEGROUP $SUBSCRIPTIONID $CLUSTERNAME1 $KUBECONFIGPATH1
                echo "Success: grabbed kubeconfig for $CLUSTERNAME1"
                echo "Cluster Ready!"
                ;;
            "test-clusters")
                echo "ACTION=$ACTION : TYPE=$TYPE - creating kubernetes clusters..."
                # grabbing subscription id
                SUBSCRIPTIONID=$(az account list | jq '.[0].id' | tr -d '"')
                exitOnError $? "Fail: error grabbing subscription id, maybe you are not signed in to azure"

                # create resource group
                checkResourceGroup $RESOURCEGROUP
                echo "creating resource group: $RESOURCEGROUP..."

                az group create -l $LOCATION -n $RESOURCEGROUP
                exitOnError $? "Fail: error creating resource group: $RESOURCEGROUP"
                echo "Success: created resource group: $RESOURCEGROUP"

                # create vnet 1
                echo "creating virtual network: $VNETNAME1..."
                createVirtualNetwork $RESOURCEGROUP $VNETNAME1 $VNETSUBNETNAME1 $ADDRESSPREFIX1 $SUBNETPREFIX1
                echo "Success: created virtual network: $VNETNAME1"

                # create vnet 2
                echo "creating virtual network: $VNETNAME2..."
                createVirtualNetwork $RESOURCEGROUP $VNETNAME2 $VNETSUBNETNAME2 $ADDRESSPREFIX2 $SUBNETPREFIX2
                echo "Success: created virtual network: $VNETNAME2"

                # create cluster 1
                echo "creating aks cluster: $CLUSTERNAME1"
                createCluster $RESOURCEGROUP $LOCATION $NODESPERCLUSTER $SUBSCRIPTIONID $VNETNAME1 $VNETSUBNETNAME1 $DNSSERVICEIP1 $ADDRESSCIDR1 $CLUSTERNAME1 $VMSIZE1 $K8VERSION1
                exitOnError $? "Fail: error creating aks cluster: $CLUSTERNAME1"
                echo "Success: created aks cluster: $CLUSTERNAME1"

                # create cluster 2
                echo "creating aks cluster: $CLUSTERNAME2"
                createCluster $RESOURCEGROUP $LOCATION $NODESPERCLUSTER $SUBSCRIPTIONID $VNETNAME2 $VNETSUBNETNAME2 $DNSSERVICEIP2 $ADDRESSCIDR2 $CLUSTERNAME2 $VMSIZE2 $K8VERSION2
                echo "Success: created aks cluster: $CLUSTERNAME2"

                # peer vnet 1 to vnet 2
                echo "creating network peer: $PEERINGNAME1"
                createPeering $RESOURCEGROUP $SUBSCRIPTIONID $PEERINGNAME1 $VNETNAME1 $VNETNAME2
                echo "Success: created network peer: $PEERINGNAME1"

                # peer vnet 2 to vnet 1
                echo "creating network peer: $PEERINGNAME2"
                createPeering $RESOURCEGROUP $SUBSCRIPTIONID $PEERINGNAME2 $VNETNAME2 $VNETNAME1
                echo "Success: created network peer: $PEERINGNAME2"

                # open inbound traffic to cluster 1
                echo "creating inbound traffic rule for $CLUSTERNAME1"
                MANAGEDRESOURCEGROUP1=MC_${RESOURCEGROUP}_${CLUSTERNAME1}_${LOCATION}
                NSG1=$(az network nsg list --resource-group $MANAGEDRESOURCEGROUP1 | jq '.[0].name' | tr -d '"')
                openInboundTraffic $MANAGEDRESOURCEGROUP1 $SUBSCRIPTIONID $NSG1
                echo "Success: created inbound traffic rule for $CLUSTERNAME1"

                # open outbound traffic from cluster 1
                echo "creating outbound traffic rule for $CLUSTERNAME1"
                openOutboundTraffic $MANAGEDRESOURCEGROUP1 $SUBSCRIPTIONID $NSG1
                echo "Success: created outbound traffic rule for $CLUSTERNAME1"

                # open inbound traffic to cluster 2
                echo "creating inbound traffic rule for $CLUSTERNAME2"
                MANAGEDRESOURCEGROUP2=MC_${RESOURCEGROUP}_${CLUSTERNAME2}_${LOCATION}
                NSG2=$(az network nsg list --resource-group $MANAGEDRESOURCEGROUP2 | jq '.[0].name' | tr -d '"')
                openInboundTraffic $MANAGEDRESOURCEGROUP2 $SUBSCRIPTIONID $NSG2
                echo "Success: created inbound traffic rule for $CLUSTERNAME2"

                # open outbound traffic from cluster 2
                echo "creating outbound traffic rule for $CLUSTERNAME2"
                openOutboundTraffic $MANAGEDRESOURCEGROUP2 $SUBSCRIPTIONID $NSG2
                echo "Success: created outbound traffic rule for $CLUSTERNAME2"

                # get kubeconfig for cluster 1
                echo "grabbing kubeconfig for $CLUSTERNAME1"
                grabKubeConfig $RESOURCEGROUP $SUBSCRIPTIONID $CLUSTERNAME1 $KUBECONFIGPATH1
                echo "Success: grabbed kubeconfig for $CLUSTERNAME1"

                # get kubeconfig from cluster 2
                echo "grabbing kubeconfig for $CLUSTERNAME2"
                grabKubeConfig $RESOURCEGROUP $SUBSCRIPTIONID $CLUSTERNAME2 $KUBECONFIGPATH2
                echo "Success: grabbed kubeconfig for $CLUSTERNAME2"
                echo "Clusters Ready!"
        esac
        ;;
    "delete")
        if [ -z "$RESOURCEGROUP" ]
          then
            echo "No resource group given"
            exit 1
        fi
        echo "Deleting resource group: $RESOURCEGROUP"
        az group delete -n $RESOURCEGROUP -y
        echo "Successfully deleted resource group: $RESOURCEGROUP"
        ;;
    "*")
        echo "Exiting: Invalid action: '$ACTION'"
        exit 1
esac