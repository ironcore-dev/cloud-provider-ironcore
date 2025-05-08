BIN_NAME = "cloud-provider-ironcore"
IMG ?= controller:latest

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.32

ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

.PHONY: build
build: fmt vet
	go build -o dist/$(BIN_NAME) cmd/$(BIN_NAME)/main.go

.PHONY: lint
lint: golangci-lint ## Run golangci-lint on the code.
	$(GOLANGCI_LINT) run ./...

.PHONY: add-license
add-license: addlicense ## Add license headers to all go files.
	find . -name '*.go' -exec $(ADDLICENSE) -f hack/license-header.txt {} +

.PHONY: check-license
check-license: addlicense ## Check that every file has a license header present.
	find . -name '*.go' -exec $(ADDLICENSE) -check -c 'IronCore authors' {} +

.PHONY: check
check: add-license fmt lint test # Generate manifests, code, lint, add licenses, test

.PHONY: fmt
fmt: goimports ## Run goimports against code.
	$(GOIMPORTS) -w .

vet: ## Run go vet against code.
	go vet ./...

test: fmt vet envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test $$(go list ./... | grep -v /e2e) -coverprofile cover.out

.PHONY: docker-build
# Build the docker image
docker-build:
	docker build . -t $(IMG)

.PHONY: docker-push
docker-push:
	docker push $(IMG)

.PHONY: clean-local-bin
clean-local-bin:
	rm -rf $(LOCALBIN)/*

##@ Build Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
		mkdir -p $(LOCALBIN)

## Tool Binaries
ENVTEST ?= $(LOCALBIN)/setup-envtest
ADDLICENSE ?= $(LOCALBIN)/addlicense
GOIMPORTS ?= $(LOCALBIN)/goimports
GOLANGCI_LINT ?= $(LOCALBIN)/golangci-lint

## Tool Versions
ADDLICENSE_VERSION ?= v1.1.1
GOIMPORTS_VERSION ?= v0.31.0
GOLANGCI_LINT_VERSION ?= v2.1
SETUP_ENVTEST_VERSION ?= release-0.20

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	test -s $(LOCALBIN)/setup-envtest || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@$(SETUP_ENVTEST_VERSION)

.PHONY: addlicense
addlicense: $(ADDLICENSE) ## Download addlicense locally if necessary.
$(ADDLICENSE): $(LOCALBIN)
	test -s $(LOCALBIN)/addlicense || GOBIN=$(LOCALBIN) go install github.com/google/addlicense@$(ADDLICENSE_VERSION)

.PHONY: goimports
goimports: $(GOIMPORTS) ## Download goimports locally if necessary.
$(GOIMPORTS): $(LOCALBIN)
	test -s $(LOCALBIN)/goimports || GOBIN=$(LOCALBIN) go install golang.org/x/tools/cmd/goimports@$(GOIMPORTS_VERSION)

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	test -s $(LOCALBIN)/golangci-lint || GOBIN=$(LOCALBIN) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
