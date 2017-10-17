
###Developing:

To install dependencies run the following command:

	glide install --strip-vendor

###Running on MiniKube:

To run this operator on minikube you first need to build a docker image which
can be deployed into the Kubernetes environment. Normally you would need to push
your docker image to a Docker Repository and then pull it into Kubernetes, but
since you are using MiniKube you can build the image using the same Docker host
as the Minikube VM. That way the images are automatically present. To do this,
make sure you are using the Minikube Docker daemon:

	eval $(minikube docker-env)

Note: Later when you no longer want to use the Docker host in the MiniKube VM
you can undo this change by running the command below.

	eval $(minikube docker-env -u)

Then run make command to build the docker image.

	make container

Note: Make sure you tag your Docker image with something other than ‘latest’ and
use that tag while you pull the image. Otherwise, if you do not specify version
of your image, it will be assumed as :latest, with pull image policy of Always
correspondingly, which may eventually result in ErrImagePull as you may not have
any versions of your Docker image out there in the default docker registry
(usually DockerHub) yet.