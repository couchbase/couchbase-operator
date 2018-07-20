#!/usr/bin/env bash
 
# Service account created using above manifest file
namespace=$1
serviceaccount=$2
kubeConfig=$3

if [ -z $namespace ]; then
    echo "Error: Provide namespace"
    exit 1
fi

if [ -z $serviceaccount ]; then
    echo "Error: Provide service account name"
    exit 1
fi

if [ -z $kubeConfig ]; then
    echo "Error: Provide kubeConfig path"
    exit 1
fi

export KUBECONFIG=$kubeConfig

# Get related Secrets for this Service Account
secret=$(kubectl get serviceaccount $serviceaccount -n $namespace -o json | jq -r .secrets[].name)
 
# Get ca.crt from secret (using OSX base64 with -D flag for decode)
kubectl get secret $secret -n $namespace -o json | jq -r '.data["ca.crt"]' | base64 -D > ca.crt
 
# Get service account token from secret
user_token=$(kubectl get secret $secret -n $namespace -o json | jq -r '.data["token"]' | base64 -D)
 
# Get information from your kubectl config, this will use current context. Your kubeconfig file may have multiple context.
context=`kubectl config current-context`
 
# get cluster name of context
name=`kubectl config get-contexts $context | awk '{print $3}' | tail -n 1`
 
# get endpoint of current context
endpoint=`kubectl config view -o jsonpath="{.clusters[?(@.name == \"$name\")].cluster.server}"`
 
 
# Set cluster (run in directory where ca.crt is stored)
kubectl config set-cluster $serviceaccount-$context \
  --embed-certs=true \
  --server=$endpoint \
  --certificate-authority=./ca.crt
 
# Set user credentials
kubectl config set-credentials $serviceaccount-$context --token=$user_token
 
# Define the combination of user with the cluster
kubectl config set-context $serviceaccount-$context \
  --cluster=$serviceaccount-$context  \
  --user=$serviceaccount-$context  \
  --namespace=$namespace

# Switch current-context to $serviceaccount-$context for the user
kubectl config use-context $serviceaccount-$context
