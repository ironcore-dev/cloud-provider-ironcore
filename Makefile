BIN_NAME = "cloud-provider-onmetal"
IMG ?= controller:latest

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.26.0

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

.PHONY: addlicense
addlicense: ## Add license headers to all go files.
	find . -name '*.go' -exec go run github.com/google/addlicense -c 'OnMetal authors' {} +

.PHONY: checklicense
checklicense: ## Check that every file has a license header present.
		find . -name '*.go' -exec go run github.com/google/addlicense  -check -c 'OnMetal authors' {} +

lint: ## Run golangci-lint against code.
		golangci-lint run ./...

check: checklicense lint test

.PHONY: test
test: fmt vet envtest checklicense ## Run tests.
		KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" go test ./... -coverprofile cover.out

.PHONY: docker-build
# Build the docker image
docker-build:
	docker build . -t $(IMG)

.PHONY: docker-push
docker-push:
	docker push $(IMG)

##@ Kind Deployment plumbing

.PHONY: kind-load-controller
kind-load-controller: ## Load the controller image into the kind cluster.
	kind load docker-image controller

.PHONY: kind-apply
kind-apply: ## Apply the config in kind. Caution: Without loading the images, the pods won't come up. Use kind-deploy for a deployment including loading the images.
#	kind get kubeconfig > ./config/kind/kubeconfig
	kubectl apply -k config/kind

.PHONY: kind-delete
kind-delete: ## Delete the config from kind.
	kubectl delete -k config/kind

.PHONY: kind-deploy
kind-deploy: kind-build-load-restart-cloud-controller kind-apply ## Build and load the cloud controller into the kind cluster, then apply the config. Restarts cloud controller if they were present.

.PHONY: kind-build-load-restart-cloud-controller
kind-build-load-restart-cloud-controller: kind-build-cloud-controller kind-load-controller kind-restart-cloud-controller ## Build, load and restart the controller in kind. Restart is useless if the manifests are not in place (deployed e.g. via kind-apply / kind-deploy).

.PHONY: kind-restart-cloud-controller
kind-restart-cloud-controller: ## Restart the controller in kind. Useless if the manifests are not in place (deployed e.g. via kind-apply / kind-deploy).
	kubectl -n onmetal-system delete rs -l k8s-app=onmetal-cloud-controller-manager

.PHONY: kind-build-controller
kind-build-cloud-controller: ## Build the cloud controller for usage in kind.
	docker build -t controller .

.PHONY: clean
clean:
	rm -rf ./dist/

##@ Build Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
		mkdir -p $(LOCALBIN)

## Tool Binaries
ENVTEST ?= $(LOCALBIN)/setup-envtest

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
		test -s $(LOCALBIN)/setup-envtest || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest
