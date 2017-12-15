
# Developing:

In order to start working with the Couchbase Operator you first need to install
Kubernetes. The easiest way to do so if you do not already have a Kubernetes
cluster is with MiniKube. MiniKube handles deploying a single node Kubernetes
cluster by using VirtualBox. Setup and installation varies based on OS, but if
you're using Mac OS X the easiest way to install MiniKube is with Homebrew. Once
MiniKube is installed you can run the command below to start MiniKube.

    $ minikube start

Once you have MiniKube running `cd` into the couchbase-operator root directory
and run the following command to pull in the couchbase-operator dependencies.

    $ glide install --strip-vendor

## Running on MiniKube:

To run this operator on minikube you first need to build a docker image which
can be deployed into the Kubernetes environment. Normally you would need to push
your docker image to a Docker Repository and then pull it into Kubernetes, but
since you are using MiniKube you can build the image using the same Docker host
as the Minikube VM. That way the images are automatically present. To do this,
make sure you are using the Minikube Docker daemon:

    $ eval $(minikube docker-env)

Note: Later when you no longer want to use the Docker host in the MiniKube VM
you can undo this change by running the command below.

    $ eval $(minikube docker-env -u)

## Configuring MiniKube:

Note: If you are not using MiniKube you can skip this step.

Building the code requires mounting your development directory inside a docker
container. This task is automated, but if your development directory is not
located inside your home directory then you may need to configure MiniKube's
docker to recognize your development directory as a shared folder.

If you need to mount your development directory then you will need to share the
root directory of your $GOPATH (ie../opt). This allows containers being started
within the VM to access your dev environment when generating helper code used to
build the operator.

Make sure minikube is stopped.

    $ minikube stop

Add the /opt directory as a shared folder to the MiniKube VM within VirtualBox.
	Settings -> Shared Folders -> (click '+' to add folder)
Set the 'Folder Path' to '/opt' and check the 'Auto-Mount' box.

Start minikube.

    $ minikube start


## Building the operator:

Run make command to build the docker image.

    $ make container

Note: Make sure you tag your Docker image with something other than ‘latest’ and
use that tag while you pull the image. Otherwise, if you do not specify version
of your image, it will be assumed as :latest, with pull image policy of Always
correspondingly, which may eventually result in ErrImagePull as you may not have
any versions of your Docker image out there in the default docker registry
(usually DockerHub) yet.

## Creating a Couchbase Cluster

You should now have a Kubernetes cluster setup and a Docker image installed into
your Kubernetes environment that contains the Couchbase Operator binary. The
next step is to start the Couchbase Operator in your Kubernetes cluster so that
Kubernetes natively knows about and can handle CouchbaseCluster objects. This
can be done by running the following.

    $ kubectl create -f example/deployment.yaml

Now you can create Couchbase clusters by specifying configurations for the
CouchbaseCluster kind. We provide an example Couchbase cluster that you can
load into Kubernetes by running the command below.

    $ kubectl create -f example/couchbase-cluster.yaml
