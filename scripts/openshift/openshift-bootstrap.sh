oc logout
oc login -u system:admin
oc adm policy --as system:admin add-cluster-role-to-user cluster-admin developer

# push crd to inform cluster about custom couchbase types
oc create -n openshift -f provision/crd.yaml

# When creating new project from templates we need to
# give the operator cluster_admin permissions.
# Such permissions can only be granted from another admin
# so we create a cluster-admin serviceaccount inside of the
# openshift project which will be able to grant the operator
# it's admin permissions
oc new-project admin-cb-ctl
oc create -f provision/couchbase-operator-admin-rbac.yaml
oc create -f provision/couchbase-operator-admin-controller.yaml

# get cluster ip for assigning default route hostname
IP=$(oc config view --minify -o jsonpath='{.clusters[*].cluster.server}'  | awk -F ':' '{print $2}' | sed 's/\/\///')

# push templates
sed "s/127.0.0.1.nip.io/$IP.nip.io/" templates/couchbase-template-mds.yaml | oc create -n openshift -f -
