SHELL := /bin/bash

.PHONY: all
all: build

# Include the library makefile
include $(addprefix ./vendor/github.com/openshift/build-machinery-go/make/, \
	golang.mk \
	targets/openshift/deps.mk \
	lib/tmp.mk \
)

.PHONY: images
images:
	podman build . -t quay.io/ctrontesting/drogue-cloud-ocm-addon:latest
	podman push quay.io/ctrontesting/drogue-cloud-ocm-addon:latest
