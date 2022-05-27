BIN_NAME = "cloud-provider-onmetal"

IMG ?= cloud-provider-onmetal:latest

GOPRIVATE ?= "github.com/onmetal/*"

ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: compile

.PHONY: compile
compile: fmt vet
	go build -o dist/$(BIN_NAME) cmd/$(BIN_NAME)/main.go

.PHONY: fmt
fmt:
	go fmt ./...

.PHONY: vet
vet:
	go vet ./...

.PHONY: docker-build
# Build the docker image
docker-build:
	docker build . -t $(DOCKER_REGISTRY)/$(DOCKER_IMAGE_NAME):$(DOCKER_IMAGE_TAG) --build-arg GOPRIVATE=$(GOPRIVATE)

.PHONY: docker-push
docker-push:
	docker push $(DOCKER_REGISTRY)/$(DOCKER_IMAGE_NAME):$(DOCKER_IMAGE_TAG)

.PHONY: clean
clean:
	rm -rf ./dist/