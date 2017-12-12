# Development

To install dependencies run the following command:

	glide install --strip-vendor

# Building the operator

This make command will build a docker image

	make container

Note: Make sure you tag your Docker image with something other than ‘latest’ and
use that tag while you pull the image. Otherwise, if you do not specify version
of your image, it will be assumed as :latest, with pull image policy of Always
correspondingly, which may eventually result in ErrImagePull as you may not have
any versions of your Docker image out there in the default docker registry
(usually DockerHub) yet.

# Uploading to DockerHub

Need instructions here...
