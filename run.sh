make container
docker push ritak8s18reg.azurecr.io/couchbase-operator:v1.0.2
k delete -f example/couchbase-cluster.yaml
k delete -f example/deployment.yaml
k delete crd couchbaseclusters.couchbase.database.couchbase.com
k apply -f example/deployment.yaml
sleep 60
k apply -f example/couchbase-cluster.yaml