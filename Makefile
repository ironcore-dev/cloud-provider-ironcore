BIN_NAME = "cloud-provider-onmetal"
IMG ?= localhost:5000/cloud-provider-onmetal:latest

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

ENVTEST_ASSETS_DIR=$(shell pwd)/testbin
test: fmt vet
	mkdir -p ${ENVTEST_ASSETS_DIR}
	test -f ${ENVTEST_ASSETS_DIR}/setup-envtest.sh || curl -sSLo ${ENVTEST_ASSETS_DIR}/setup-envtest.sh https://raw.githubusercontent.com/kubernetes-sigs/controller-runtime/v0.8.3/hack/setup-envtest.sh
	source ${ENVTEST_ASSETS_DIR}/setup-envtest.sh; fetch_envtest_tools $(ENVTEST_ASSETS_DIR); setup_envtest_env $(ENVTEST_ASSETS_DIR); go test ./... -coverprofile cover.out

.PHONY: docker-build
# Build the docker image
docker-build:
	DOCKER_BUILDKIT=1 docker build . -t $(IMG) --build-arg GOPRIVATE=$(GOPRIVATE)

.PHONY: docker-push
docker-push:
	docker push $(IMG)

.PHONY: clean
clean:
	rm -rf ./dist/