# MiniKube

To run this operator on minikube you first need to build a Docker image which
can be deployed into the Kubernetes environment. Normally you would need to push
your docker image to a Docker Repository and then pull it into Kubernetes, but
since you are using MiniKube you can build the image using the same Docker host
as the Minikube VM. That way the images are automatically present. To do this,
make sure you are using the Minikube Docker daemon:

	 eval $(minikube docker-env)

Note: Later when you no longer want to use the Docker host in the MiniKube VM
you can undo this change by running the command below.

	 eval $(minikube docker-env -u)

# Configuring MiniKube

Note: If you are not using minikube you can skip this step.

Building the code requires mounting your development directory inside a docker container.
This task is automated, but if your development directory is not located inside your home
directory then you may need to configure minikube's docker to recognize your development
directory as a shared folder.

If you need to mount your development directory then you will need to share the root directory of your
$GOPATH (ie../opt). This allows containers being started within the vm to access your dev
environment when generating helper code used to build the operator.

Make sure minikube is stopped.

	 minikube stop

Add the /opt directory as a shared folder to the minikube vm within virtualbox.
	Settings -> Shared Folders -> (click '+' to add folder)
Set the 'Folder Path' to '/opt' and check the 'Auto-Mount' box.

Start minikube.

	 minikube start
