SHELL := /bin/bash
GO ?= go
GO_CMD := CGO_ENABLED=0 $(GO)
GIT_VERSION := $(shell git describe --tags --dirty)
VERSION := $(GIT_VERSION:v%=%)
GIT_COMMIT := $(shell git rev-parse HEAD)
DOCKER_REPO ?= moebiusss/netatmo-exporter
DOCKER_TAG ?= dev

.PHONY: all
all: test build-binary

include .bingo/Variables.mk

.PHONY: test
test:
	$(GO_CMD) test -cover ./...

.PHONY: lint
lint: $(GOLANGCI_LINT)
	@$(GOLANGCI_LINT) run --fix

.PHONY: build-binary
build-binary:
	$(GO_CMD) build -tags netgo -ldflags "-w -X main.Version=$(VERSION) -X main.GitCommit=$(GIT_COMMIT)" -o netatmo-exporter .

.PHONY: image
image:
	docker buildx build -t "ghcr.io/$(DOCKER_REPO):$(DOCKER_TAG)" --load .

.PHONY: all-images
all-images:
	docker buildx build -t "ghcr.io/$(DOCKER_REPO):$(DOCKER_TAG)" -t "docker.io/$(DOCKER_REPO):$(DOCKER_TAG)" --platform linux/amd64,linux/arm64 --push .

.PHONY: clean
clean:
	rm -f netatmo-exporter
