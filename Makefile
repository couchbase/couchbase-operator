# General Information
# ===================
#
# All binary builds in this repository will be done in Docker.
# Why? Three reasons:
#
# 1) Some things don't build on Mac with a native tool chain
# 2) Everyone is using exactly the same always, see point 3
# 3) The source of truth is this file/repo, there are no external
#    dependencies in the build system any more.
#
# The most important things you need to know about are:
#
# PLATFORM:
#   Affects TOOLS_TARGET/IMAGE_TARGET below, and provides a
#   shortcut to build for openshift as opposed to kubernetes.
#
# TOOLS_TARGET/IMAGE_TARGET:
#   These are triplets e.g. kubernetes-linux-amd64, that
#   controls what is built with each invocation of a make
#   goal.  The "platform" (kubernetes or openshift) affects
#   packaging names and contents, build flags and docker
#   build files.  The OS will obviously tailor binary formats
#   and system calls, and the architecture will change the CPU
#   the instruction  set.  We need two different targets to
#   cater for Apple users who need to compile for
#   kubernetes-macos-arm64 for tools and kubernetes-linux-amd64
#   for containers.  The tools target defaults to whatever it
#   can detect about your local machine, the images are for
#   use with linux/amd64.
#
# make (all):
#   Default make goal will build the necessary tools to do your
#   job and place them in "build/bin/$(TOOLS_TARGET)", it will
#   also build docker images.
#
# make kind-images:
#   Makes all the images and pokes them in to kind
#
# Architecture
# ============
#
# Basic Build Goals
# -----------------
#
# These are things like 'make binary IMAGE=couchbase-operator'.  They
# are not intended for end user use, as they will yield inconsistent binaries
# across teams and developers.  You can use them for a quick smoke test
# of does-it-compile, but these are best left for use inside a build
# container where the environment is consistent and predicatable.
# These build targets are not "targeted", so they just end up in
# "./build/bin" and thus can cause confusion and mistakes.
#
# This leads us on to how to correctly build binaries.  There are a
# couple goals like "make tools" that will build properly.  For example
# the cao binary may end up in "build/bin/kubernetes-linux-amd64", where
# the target bit depends on the TOOLS_TARGET triplet.  When one of these
# target files is rebuilt, it will build the binary inside a build
# container and copy the binary out.  The correct compiler version, tools
# or image target and version are propagated into the container and then
# back into make, thus making this file (and its inputs) a canonical source
# of truth.
#
# With this in mind we can do all the cross compilation we'll ever need.
#
# Advanced Build Goals
# --------------------
#
# There are two occasions an image needs to be built, the first -- and most
# common -- is for normal developers, the second for the build system.  Due
# to the nature of the build system, there are a bunch of requirements
# such as each image being a directory in an archive, containing certain
# Dockerfiles.  Rather than fight these demands -- for now -- we utilize them
# to improve testing.
#
# Images start off as a collection of files in a directory, for example
# ./build/couchbase-operator-image_9.9.9-999/couchbase-operator, these all have
# recipes to copy static files, generate them, or trigger binary builds as
# necessary.  Here we can do two things, call "docker build" ourselves, or
# archive the whole directory for the build system.  Thus, when we build images
# for ourselves, we're actually testing the build system won't break.
#
# Note as the binaries are passed around as binaries between archival and build
# doing its thing, we can distribute this to customers as there is no source.
#
# Build System Goals
# ------------------
#
# The only interface build forces us to support is "make dist".  This triggers
# builds of all images (as discussed above) in an archive, and all tools and
# distributable bumpf in a per-target archive.  For the most part, this just
# uses recursive invocations of this make file with different targets for
# different platforms, operating systems and architectures as necessary.

################################################################################
# Variables
# ---------
# These can be changed by the build system, or however you see fit.
################################################################################

# Product defines the product/application name, and has a bearing on
# what the package artifacts are called.
PRODUCT := couchbase-autonomous-operator

# These are overidden by the build system, so need to be optional
# if undefined.  The build system also doesn't use -e to override
# so we need to be careful here.
VERSION ?= 2.3.0
BLD_NUM ?= 999

# This controls the build version of docker used.
# The only caveat, is the build system doesn't use this as the source
# of truth, so you'll want to update the defaults found in ./docker/...
GO_VERSION := 1.17.2

# Short cut for setting the platform across all targets.
PLATFORM := kubernetes

# The target controls what's built as regards cross compilation.
# These are similar to target triplets in the C world e.g. x86_64-unknown-linux.
# The syntax is <platform>-<os>-<arch>, where platform is either
# kubernetes or openshift, and the os and architecture are directly
# compatible with Go.  These are top level and only relevant to developers
# whose laptop and kubernetes clusters may have wildly different environments.
# These targets will be recursively passed to the make system based on
# the context of what's being built as TARGET.
TOOLS_TARGET := $(PLATFORM)-$(shell go env GOHOSTOS)-$(shell go env GOHOSTARCH)
IMAGE_TARGET := $(PLATFORM)-linux-amd64

################################################################################
# Static/Generated Variables
################################################################################

# The main target is set by a recursive call to this makefile, after that,
# the real work can begin!
TARGET := undefined
TARGET_PLATFORM := $(word 1,$(subst -, ,$(TARGET)))
TARGET_OS := $(word 2,$(subst -, ,$(TARGET)))
TARGET_ARCH := $(word 3,$(subst -, ,$(TARGET)))

# Static configuration parameters.
BUILDDIR := build
ARTIFACTSDIR := dist

# Variable for propagating build arguments.
BUILD_ENV := VERSION=$(VERSION) BLD_NUM=$(BLD_NUM)

################################################################################
# Binary Related Variables
################################################################################

# There are two binary directories defined, the BINDIR is used to actually
# run Go and compile things, this SHOULD be used in a build container that
# is totally under our control.  The TARGET_BINDIR is what ends up on your
# system aka the result of a containerized build.
BINDIR := $(BUILDDIR)/bin
TARGET_BINDIR := $(BUILDDIR)/bin/$(TARGET)

# Define binary types, that in turn define the type of compilation required,
# and flags etc.
STATIC_BINARIES := \
	cao \
	cbopcfg \
	cbopinfo \
	couchbase-operator \
	couchbase-operator-admission

DYNAMIC_BINARIES := \
	couchbase-operator-certification

# Define the binary files when compiled in a container.
BINARIES := $(STATIC_BINARIES) $(DYNAMIC_BINARIES)
STATIC_BINARIES := $(addprefix $(BINDIR)/,$(STATIC_BINARIES))
DYNAMIC_BINARIES := $(addprefix $(BINDIR)/,$(DYNAMIC_BINARIES))

# Define the binary paths when compiled in, and then extracted from, a container.
TARGET_BINARIES := $(addprefix $(TARGET_BINDIR)/,$(BINARIES))

# The target binary is the interface used to select a specific binary to build
# within a container.  Used with "make binary" and "make target-binary".
BINARY := undefined

################################################################################
# Repository/Source Related Variables
################################################################################

# These files define the V2 Kubernetes API.
APISRC_V2 := \
	pkg/apis/couchbase/v2/doc.go \
  pkg/apis/couchbase/v2/types.go

# Modifying these files triggers a rebuild of kubernetes clients and informers.
APISRC := $(APISRC_V2)

# This is this module's name.
PACKAGE_BASE := github.com/couchbase/couchbase-operator

# This is the base of all kubernetes APIs we define.
API_PACKAGE_BASE := $(PACKAGE_BASE)/pkg/apis

# This is the set of directories we consider as autogenerated inputs (must be comma separated).
API_PACKAGE_V2 := $(API_PACKAGE_BASE)/couchbase/v2

# Clientset name (change me!)
# This influences generated code package names, so we are constantly having to
# refer to versioned.CouchbaseV2(), which is somewhat non-descript.
CLIENTSET_NAME := versioned

# Go include path for generated content.
GENERATED_PACKAGE_BASE := $(PACKAGE_BASE)/pkg/generated
CLIENTSET_PACKAGE := $(GENERATED_PACKAGE_BASE)/clientset
LISTERS_PACKAGE := $(GENERATED_PACKAGE_BASE)/listers
INFORMERS_PACKAGE := $(GENERATED_PACKAGE_BASE)/informers

# Common arguments for the Kubernetes code generator tool chain.
CODEGEN_ARGS := --go-header-file scripts/codegen/boilerplate.go.txt --output-base ../../..

# CRD file target, contains all the CRDs.
CRD_FILE := example/crd.yaml

# These files (and directories) are auto generated.
GENERATED_FILES := pkg/apis/couchbase/v2/zz_generated.deepcopy.go \
  pkg/generated/clientset \
  pkg/generated/listers \
  pkg/generated/informers

# Define all source files.  A change in any of these must trigger a
# rebuild of any binaries.
SOURCE := $(shell find . -name *.go -type f)

# The git revision, infinitely more useful than an arbitrary build number.
REVISION := $(shell git rev-parse HEAD)

################################################################################
# Go Language Variables
# ---------------------
# Defines linker and build flags type stuff.
################################################################################

# External go tools configuration.
# We install these when used, so they override things other builds have done
# and also avoid scruitiny from scanners.
GOPATH := $(shell go env GOPATH)
GOBIN := $(if $(GOPATH),$(GOPATH)/bin,$(HOME)/go/bin)
GOLINT_VERSION := v1.42.1
CODE_GENERATOR_VERSION := v0.23.2 # Should be kept in sync with other libs in go.mod
CONTROLLER_TOOLS_VERSION := v0.8.0 # See https://github.com/kubernetes-sigs/controller-tools/releases

# Common environment settings for all Go builds.
STATIC_GOENV := CGO_ENABLED=0 GOOS=$(TARGET_OS) GOARCH=$(TARGET_ARCH)
DYNAMIC_GOENV := GOOS=$(TARGET_OS) GOARCH=$(TARGET_ARCH)

# These are propagated into each binary so we can tell for sure the exact build
# that a binary came from.
LDFLAGS = \
  -X github.com/couchbase/couchbase-operator/pkg/version.Version=$(VERSION) \
  -X github.com/couchbase/couchbase-operator/pkg/version.BuildNumber=$(BLD_NUM) \
  -X github.com/couchbase/couchbase-operator/pkg/revision.gitRevision=$(REVISION)

# This specifies any flags that need to be passed to the Go compiler when
# building binaries.
BUILDFLAGS := -trimpath

ifeq ($(TARGET_PLATFORM),openshift)
BUILDFLAGS += -tags redhat
endif

################################################################################
# Docker Variables
# ----------------
# Defines the docker files to use for building binaries and creating image
# archives.
################################################################################

# This allows the container image tags to be explicitly set.
DOCKER_USER := couchbase
DOCKER_TAG := $(VERSION)

# We can select the flavor of container that a binary is built in.
DOCKERFILE := Dockerfile

ifeq ($(TARGET_PLATFORM),openshift)
DOCKERFILE := Dockerfile.rhel
endif

# The build image is platform specific (depends on the dockerfile), thus
# enabling concurrency.
DOCKER_BUILD_IMAGE := couchbase/operator-build:$(TARGET_PLATFORM)

# All binary builds get these arguments, acting as a stable interface between
# the make system and any binaries emitted.  Thus the correct version is
# propagated, the correct compiler to use, and what target to build for.
DOCKER_BUILD_ARGS := \
	--build-arg GO_VERSION=$(GO_VERSION) \
	--build-arg VERSION=$(VERSION) \
	--build-arg BLD_NUM=$(BLD_NUM) \
  --build-arg TARGET=$(TARGET)

################################################################################
# Tools Artifact Variables
# ------------------------
# What goes into user distributables.  This is invoked once per target, make
# cannot handle the weird layout we require and multiple targets using pattern
# matching, so we do this recursively.
################################################################################

# Archive is the archive type used to create a tools artifact.
ARCHIVE := tar.gz

# Define files that must be part of a build artifact, then append
# any platform specific ones.  This is the canonical list that controls
# what's distributed to customers.
TOOLS_PACKAGE_FILES := \
	VERSION.txt \
	License.txt \
	README.txt \
	crd.yaml \
	couchbase-cluster.yaml \
	sync-gateway.yaml \
	bin/cao \
	bin/cbopcfg \
	bin/cbopinfo

ifeq ($(TARGET_PLATFORM),kubernetes)
TOOLS_PACKAGE_FILES += network-policies.yaml pillowfight-data-loader.yaml
endif

ifeq ($(TARGET_PLATFORM),openshift)
TOOLS_PACKAGE_FILES += cluster-role-user.yaml
endif

# The distributable artifact that will be generated for the specific target and
# archive type.  The build directory is a staging location for necessary files
# as defined above.

# HACK: The one legacy caveat here is that build requires that darwin
# be called macos for notarization.
ARTIFACT_TARGET := $(TARGET)

ifeq ($(TARGET_OS),darwin)
ARTIFACT_TARGET := $(TARGET_PLATFORM)-macos-$(TARGET_ARCH)
endif

# The artifact name needs to have $(VERSION)-$(BLD_NUM) in it or it won't get released.
TOOLS_ARTIFACT_NAME := $(PRODUCT)_$(VERSION)-$(BLD_NUM)-$(ARTIFACT_TARGET)
TOOLS_ARTIFACT := $(ARTIFACTSDIR)/$(TOOLS_ARTIFACT_NAME).$(ARCHIVE)
TOOLS_ARTIFACT_BUILDDIR := $(BUILDDIR)/$(TOOLS_ARTIFACT_NAME)
TOOLS_ARTIFACT_FILES := $(addprefix $(TOOLS_ARTIFACT_BUILDDIR)/,$(TOOLS_PACKAGE_FILES))

################################################################################
# Image and Artifact Variables
# ----------------------------
# What goes into image artifacts.  This is invoked once per image, make
# cannot handle the weird layout we require and multiple images/targets using
# pattern matching, so we do this recursively.
################################################################################

# Image artifacts are bundled together in a single archive, so we make use
# of recursive builds to simplify the process, calling once per image to
# create the archive contents.  Used with "make image" and
# "make image-artifact".
IMAGE := undefined

# A list of all possible images.
IMAGES := \
	couchbase-operator \
	couchbase-operator-admission \
	couchbase-operator-certification

# Explicitly select the files that will be part of an image artifact.
# This will get processed multiple times with different targets, so
# we expect the static files to remain common/constant with only the
# binaries being accumulated in different target directories.
IMAGE_FILES := \
	$(TARGET)/$(IMAGE) \
	Dockerfile \
	Dockerfile.rhel \
	License.txt \
	README.txt

ifeq ($(IMAGE),couchbase-operator)
IMAGE_FILES += passwd
else ifeq ($(IMAGE),couchbase-operator-admission)
IMAGE_FILES += passwd
else ifeq ($(IMAGE),couchbase-operator-certification)
IMAGE_FILES += \
	$(TARGET)/cao \
	validation.yaml \
	crd.yaml
endif

IMAGE_ARTIFACT_NAME := couchbase-operator-image_$(VERSION)-$(BLD_NUM)
IMAGE_ARTIFACT_BUILDDIR := $(BUILDDIR)/$(IMAGE_ARTIFACT_NAME)/$(IMAGE)
IMAGE_ARTIFACT := $(ARTIFACTSDIR)/$(IMAGE_ARTIFACT_NAME).tgz
IMAGE_ARTIFACT_FILES := $(addprefix $(IMAGE_ARTIFACT_BUILDDIR)/,$(IMAGE_FILES))

################################################################################
# Last Variables
# --------------
# Everything that depends on everything before it.
################################################################################

# These are the directories that can be created.
DIRECTORIES := \
  $(BINDIR) \
  $(TARGET_BINDIR) \
  $(TOOLS_ARTIFACT_BUILDDIR) \
  $(TOOLS_ARTIFACT_BUILDDIR)/bin \
  $(IMAGE_ARTIFACT_BUILDDIR) \
  $(IMAGE_ARTIFACT_BUILDDIR)/$(TARGET)

################################################################################
# User Goals
# ----------
# These you/CI should/can use.
################################################################################

# Default target for a dev is to build the required binaries, and images.
.PHONY: all
all: crd tools images

# Tools builds the tools of the trade.
.PHONY: tools
tools:
	$(MAKE) target-binary -e $(BUILD_ENV) BINARY=cao TARGET=$(TOOLS_TARGET)

# Remove any ephemeral bits, especially anything to do with the
# official build process, because it's not very Makefile friendly.
.PHONY: clean
clean:
	rm -rf $(BUILDDIR) $(ARTIFACTSDIR)

# Timestamps aren't preserved by a git checkout, so the ordering is random as far
# as we're concerned.  This 'fixes' generated target timestamps after checkout to
# inhibit rebuilds.
.PHONY: touch-generated
touch-generated:
	touch ${GENERATED_FILES}

# Build target for the CRD files.
.PHONY: crd
crd: $(CRD_FILE)

# Lint target to test source code compliance.
.PHONY: lint
lint: $(GENERATED_FILES)
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLINT_VERSION)
	$(GOBIN)/golangci-lint run ./pkg/... ./cmd/... ./test/...

# Lint python scripts for dodgy code.
.PHONY: lint-python
lint-python:
	scripts/pylinter

# Create all images.
.PHONY: images
images:
	$(foreach image,$(IMAGES),$(MAKE) image -e $(BUILD_ENV) IMAGE=$(image) TARGET=$(IMAGE_TARGET);)

# The go-to swiss army knife of targets, make all images and install in kind.
.PHONY: kind-images
kind-images: images
	kind load docker-image $(foreach image,$(IMAGES),${DOCKER_USER}/$(image):${DOCKER_TAG})

# Clean up any images for this build, useful for CI jobs.
.PHONY: images-clean
images-clean:
	docker rmi -f $(foreach image,$(IMAGES),${DOCKER_USER}/$(image):${DOCKER_TAG})

# This target pushes the images to a public repository.
# A typical one liner to deploy to the cloud would be:
#   make images-public -e DOCKER_USER=couchbase DOCKER_TAG=2.0.0
.PHONY: images-public
images-public: images
	$(foreach image,$(IMAGES),docker push ${DOCKER_USER}/$(image):${DOCKER_TAG};)

# Run any Go unit tests that exist.
.PHONY: unit-test
test-unit:
	go test $(BUILDFLAGS) -v ./pkg/...

# Docs are partially auto-generated from the CRD.
.PHONY: docs
docs: $(CRD_FILE)
	scripts/asciidoc-gen --version v2
	scripts/asciidoc-link
	go run scripts/mangen.go

# Check the docs are spelled correctly and compile etc.
.PHONY: docs-lint
docs-lint:
	scripts/asciidoc-lint

################################################################################
# Private Goals
# -------------
# These should be invoked for testing purposes only or via the build
# system.  Must also be invoked with TARGET set.
################################################################################

# Calling this will perform a containerized build and place the resulting
# binary in the target bin folder.  Requires BINARY to be set.
.PHONY: target-binary
target-binary: $(TARGET_BINDIR)/$(BINARY)

# Calling this will perform a build of the binary, and should happen in a
# container. Requires BINARY to be set.
.PHONY: binary
binary: $(BINDIR)/$(BINARY)

# Create a single image from an image artifact.  Requires IMAGE to be set.
.PHONY: image
image: image-artifact
	cd $(IMAGE_ARTIFACT_BUILDDIR) && DOCKER_BUILDKIT=1 docker build -f $(DOCKERFILE) -t ${DOCKER_USER}/$(IMAGE):${DOCKER_TAG} $(IMAGE_DOCKER_BUILD_ARGS) .

################################################################################
# Build System Goals
################################################################################

# Create a single image artifact.
.PHONY: image-artifact
image-artifact: $(IMAGE_ARTIFACT_FILES)

# Create a single tools artifact.
.PHONY: tools-artifact
tools-artifact: $(TOOLS_ARTIFACT)

# Target to build all combinations of tooling for various platforms.
.PHONY: tools-artifacts
tools-artifacts:
	$(MAKE) tools-artifact -e $(BUILD_ENV) TARGET=kubernetes-linux-amd64 ARCHIVE=tar.gz
	$(MAKE) tools-artifact -e $(BUILD_ENV) TARGET=kubernetes-darwin-amd64 ARCHIVE=zip
	$(MAKE) tools-artifact -e $(BUILD_ENV) TARGET=kubernetes-darwin-arm64 ARCHIVE=zip
	$(MAKE) tools-artifact -e $(BUILD_ENV) TARGET=kubernetes-windows-amd64 ARCHIVE=zip
	$(MAKE) tools-artifact -e $(BUILD_ENV) TARGET=openshift-linux-amd64 ARCHIVE=tar.gz
	$(MAKE) tools-artifact -e $(BUILD_ENV) TARGET=openshift-darwin-amd64 ARCHIVE=zip
	$(MAKE) tools-artifact -e $(BUILD_ENV) TARGET=openshift-darwin-arm64 ARCHIVE=zip
	$(MAKE) tools-artifact -e $(BUILD_ENV) TARGET=openshift-windows-amd64 ARCHIVE=zip

# target to build all combinations of images for various platforms.
.PHONY: image-artifacts
image-artifacts: | $(ARTIFACTSDIR)
	$(MAKE) image-artifact -e $(BUILD_ENV) IMAGE=couchbase-operator TARGET=kubernetes-linux-amd64
	$(MAKE) image-artifact -e $(BUILD_ENV) IMAGE=couchbase-operator TARGET=openshift-linux-amd64
	$(MAKE) image-artifact -e $(BUILD_ENV) IMAGE=couchbase-operator-admission TARGET=kubernetes-linux-amd64
	$(MAKE) image-artifact -e $(BUILD_ENV) IMAGE=couchbase-operator-admission TARGET=openshift-linux-amd64
	$(MAKE) image-artifact -e $(BUILD_ENV) IMAGE=couchbase-operator-certification TARGET=kubernetes-linux-amd64
	$(MAKE) image-artifact -e $(BUILD_ENV) IMAGE=couchbase-operator-certification TARGET=openshift-linux-amd64
	tar czf $(IMAGE_ARTIFACT) -C $(BUILDDIR)/$(IMAGE_ARTIFACT_NAME) $(IMAGES)

# Interface used by build to trigger things it needs.
# Here's where/why the build system is totally in need of some love...
# Make should define a set of files to create, and these should depend on any
# directories that are required to fulfill those targets.  However, what it actually
# does is call a target that is a directory, thus going against how make is supposed
# to be used.  This leads us on to the schizophrenic nature of this target.
# When called explicity, it will recursively ask for different targets to
# be built.  In any other context, this functions correctly, as a dependency
# of a file that resides within it.
dist:
ifeq ($(MAKECMDGOALS),dist)
	$(MAKE) touch-generated image-artifacts tools-artifacts -e $(BUILD_ENV)
else
	mkdir -p dist
endif

################################################################################
# Code Recipes
# ------------
# Used to build any auto-generated code and the like.
################################################################################

# Build the V2 deep copy functions when the API source changes.
pkg/apis/couchbase/v2/zz_generated.deepcopy.go: $(APISRC_V2)
	@go install k8s.io/code-generator/cmd/deepcopy-gen@$(CODE_GENERATOR_VERSION)
	$(GOBIN)/deepcopy-gen --input-dirs $(API_PACKAGE_V2) -O zz_generated.deepcopy --bounding-dirs $(API_PACKAGE_BASE) $(CODEGEN_ARGS)

# Build the couchbase kubernetes client when any API source changes.
pkg/generated/clientset: $(APISRC)
	@rm -rf $@
	@go install k8s.io/code-generator/cmd/client-gen@$(CODE_GENERATOR_VERSION)
	$(GOBIN)/client-gen --clientset-name $(CLIENTSET_NAME) --input-base "" --input $(API_PACKAGE_V2) --output-package $(CLIENTSET_PACKAGE) $(CODEGEN_ARGS)

# Build the couchbase kubernetes listers when any API source changes.
pkg/generated/listers: $(APISRC)
	@rm -rf $@
	@go install k8s.io/code-generator/cmd/lister-gen@$(CODE_GENERATOR_VERSION)
	$(GOBIN)/lister-gen --input-dirs $(API_PACKAGE_V2) --output-package $(LISTERS_PACKAGE) $(CODEGEN_ARGS)

# Build the couchbase kubernetes informers when the clients or listers update.
pkg/generated/informers: pkg/generated/clientset pkg/generated/listers
	@rm -rf $@
	@go install k8s.io/code-generator/cmd/informer-gen@$(CODE_GENERATOR_VERSION)
	$(GOBIN)/informer-gen --input-dirs $(API_PACKAGE_V2) --versioned-clientset-package $(CLIENTSET_PACKAGE)/$(CLIENTSET_NAME) --listers-package $(LISTERS_PACKAGE) --output-package $(INFORMERS_PACKAGE) $(CODEGEN_ARGS)

# Build the CRDs from the binary when the binary updates.
$(CRD_FILE): $(APISRC_V2)
	@go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)
	$(GOBIN)/controller-gen crd:crdVersions=v1 paths=./pkg/apis/couchbase/v2 output:dir=example
	@cat example/couchbase.com_*.yaml > $@
	go run -ldflags "$(LDFLAGS)" scripts/crd-transform.go -in $@ -out $@
	@rm -f example/couchbase.com_*.yaml

################################################################################
# Private Recipes
################################################################################

# Rule to make any directories required.
$(DIRECTORIES):
	mkdir -p $@

# Rules to build binaries.
$(STATIC_BINARIES): $(GENERATED_FILES) $(SOURCE)
	$(STATIC_GOENV) go build $(BUILDFLAGS) -o $@ -ldflags "$(LDFLAGS)" ./cmd/$(notdir $@)
$(DYNAMIC_BINARIES): $(GENERATED_FILES) $(SOURCE)
	$(DYNAMIC_GOENV) go test $(BUILDFLAGS) -race -c $(PACKAGE_BASE)/test/e2e -o $@ -ldflags "$(LDFLAGS)"

# All binary builds for image builds will match these rules, and all builds will happen in a container
# so we have full, in-tree, control of the tool chain.
$(TARGET_BINARIES): .dockerignore docker/couchbase-operator-build/$(DOCKERFILE) $(GENERATED_FILES) $(SOURCE) | $(TARGET_BINDIR)
	DOCKER_BUILDKIT=1 docker build -f docker/couchbase-operator-build/$(DOCKERFILE) -t $(DOCKER_BUILD_IMAGE) --build-arg GOAL=binary --build-arg BINARY=$(notdir $@) $(DOCKER_BUILD_ARGS) .
	docker run --rm --entrypoint /bin/cat $(DOCKER_BUILD_IMAGE) /tmp/src/github.com/couchbase/couchbase-operator/$(BINDIR)/$(notdir $@) > $(TARGET_BINDIR)/$(notdir $@)
	chmod +x $(TARGET_BINDIR)/$(notdir $@)
	docker rmi -f $(DOCKER_BUILD_IMAGE)

################################################################################
# Build System Recipes
################################################################################

# Rule to create a tar artifact archive.
$(ARTIFACTSDIR)/%.tar.gz: $(TOOLS_ARTIFACT_FILES) | $(ARTIFACTSDIR)
	tar -czf $@ -C $(BUILDDIR) $*

# Rules to create a zip artifact archive.
$(ARTIFACTSDIR)/%.zip: $(BUILDDIR)/%.zip
	cp $< $@
$(BUILDDIR)/%.zip: $(TOOLS_ARTIFACT_FILES) | $(ARTIFACTSDIR)
	cd $(BUILDDIR); zip -r $*.zip $*

# Rule to create distributable artifact files.
$(TOOLS_ARTIFACT_BUILDDIR)/VERSION.txt: | $(TOOLS_ARTIFACT_BUILDDIR)
	echo $(VERSION)-$(BLD_NUM) > $@
$(TOOLS_ARTIFACT_BUILDDIR)/%.txt: docs/%.txt | $(TOOLS_ARTIFACT_BUILDDIR)
	cp $< $@
$(TOOLS_ARTIFACT_BUILDDIR)/crd.yaml: $(CRD_FILE) | $(TOOLS_ARTIFACT_BUILDDIR)
	cp $< $@
$(TOOLS_ARTIFACT_BUILDDIR)/bin/%: $(TARGET_BINDIR)/% | $(TOOLS_ARTIFACT_BUILDDIR)/bin
	cp $< $@
$(TOOLS_ARTIFACT_BUILDDIR)/%.yaml: docs/user/modules/ROOT/examples/$(TARGET_PLATFORM)/%.yaml | $(TOOLS_ARTIFACT_BUILDDIR)
	cp $< $@

# This group of rules defines individual image artifact files, and their direct
# dependencies.
$(IMAGE_ARTIFACT_BUILDDIR)/%.txt: docs/%.txt | $(IMAGE_ARTIFACT_BUILDDIR)
	cp $< $@
$(IMAGE_ARTIFACT_BUILDDIR)/Dockerfile: docker/$(IMAGE)/Dockerfile | $(IMAGE_ARTIFACT_BUILDDIR)
	cp $< $@
$(IMAGE_ARTIFACT_BUILDDIR)/Dockerfile.rhel: docker/$(IMAGE)/Dockerfile.rhel | $(IMAGE_ARTIFACT_BUILDDIR)
	cp $< $@
$(IMAGE_ARTIFACT_BUILDDIR)/passwd: scripts/passwd | $(IMAGE_ARTIFACT_BUILDDIR)
	cp $< $@
$(IMAGE_ARTIFACT_BUILDDIR)/validation.yaml: test/e2e/resources/validation/validation.yaml | $(IMAGE_ARTIFACT_BUILDDIR)
	cp $< $@
$(IMAGE_ARTIFACT_BUILDDIR)/crd.yaml: $(CRD_FILE) | $(IMAGE_ARTIFACT_BUILDDIR)
	cp $< $@
$(IMAGE_ARTIFACT_BUILDDIR)/$(TARGET)/%: $(TARGET_BINDIR)/% | $(IMAGE_ARTIFACT_BUILDDIR)/$(TARGET)
	cp $< $@
