# --------------------------------------------------------------------
# Copyright (c) 2025, WSO2 LLC. (https://www.wso2.com).
#
# WSO2 LLC. licenses this file to you under the Apache License,
# Version 2.0 (the "License"); you may not use this file except
# in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing,
# software distributed under the License is distributed on an
# "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
# KIND, either express or implied.  See the License for the
# specific language governing permissions and limitations
# under the License.
# --------------------------------------------------------------------

# Makefile for Gateway-Controller

# Version management
VERSION ?= $(shell cat ../VERSION 2>/dev/null || echo "0.0.1-SNAPSHOT")
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -X main.Version=$(VERSION) -X main.GitCommit=$(GIT_COMMIT) -X main.BuildDate=$(BUILD_DATE)

# Detect local architecture
LOCAL_ARCH := $(shell uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')

# Docker image configuration
DOCKER_REGISTRY ?= ghcr.io/renuka-fernando/api-platform
IMAGE_NAME := $(DOCKER_REGISTRY)/gateway-controller

.PHONY: help generate test generate-listener-certs build-base build-base-amd64 build-base-arm64

help: ## Show this help message
	@echo 'Gateway Controller (Version: $(VERSION))'
	@echo ''
	@echo 'Note: Final gateway-controller images are built via gateway-builder.'
	@echo 'For production multi-arch builds, use: make -C .. build-and-push-multiarch'
	@echo ''
	@echo 'Development Targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-25s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

generate: ## Generate API server code from OpenAPI spec
	@echo "Generating API server code from OpenAPI spec..."
	@go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.5.1 --config=oapi-codegen.yaml api/openapi.yaml

test: ## Run unit and integration tests
	@echo "Running tests..."
	@go test -v ./... -cover

build-base: generate ## Build gateway-controller base image for local architecture
	@echo "Building gateway-controller base image for local architecture ($(LOCAL_ARCH))..."
	@docker buildx build -f Dockerfile \
		--build-context sdk=../../sdk \
		--build-arg VERSION=$(VERSION) \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		-t $(DOCKER_REGISTRY)/gateway-controller-base:$(VERSION)-$(LOCAL_ARCH) \
		-t $(DOCKER_REGISTRY)/gateway-controller-base:$(VERSION) \
		-t $(DOCKER_REGISTRY)/gateway-controller-base:latest \
		--load \
		.
	@echo "Base image built: $(DOCKER_REGISTRY)/gateway-controller-base:$(VERSION)"

# Internal targets - called by parent gateway/Makefile only
# These build architecture-specific base images for gateway-builder to extend

build-base-amd64: generate ## Build gateway-controller base image for amd64 (internal use)
	@echo "Building gateway-controller base image for amd64 ($(VERSION))..."
	@docker buildx build -f Dockerfile \
		--build-context sdk=../../sdk \
		--platform linux/amd64 \
		--build-arg VERSION=$(VERSION) \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		-t $(DOCKER_REGISTRY)/gateway-controller-base:$(VERSION)-amd64 \
		--load \
		.

build-base-arm64: generate ## Build gateway-controller base image for arm64 (internal use)
	@echo "Building gateway-controller base image for arm64 ($(VERSION))..."
	@docker buildx build -f Dockerfile \
		--build-context sdk=../../sdk \
		--platform linux/arm64 \
		--build-arg VERSION=$(VERSION) \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		-t $(DOCKER_REGISTRY)/gateway-controller-base:$(VERSION)-arm64 \
		--load \
		.

generate-listener-certs: ## Generate listener certificates
	@echo "Generating listener certificates..."
	@mkdir -p listener-certs
	@openssl req -x509 \
		-newkey rsa:4096 \
		-keyout listener-certs/default-listener.key \
		-out listener-certs/default-listener.crt \
		-days 36500 \
		-nodes \
		-subj "/C=US/ST=California/L=San Francisco/O=WSO2/OU=API Gateway/CN=localhost" \
		-addext "subjectAltName=DNS:localhost,DNS:*.localhost,IP:127.0.0.1"
	@echo "Certificate and key generated successfully in listener-certs/"
	@echo "  Certificate: listener-certs/default-listener.crt"
	@echo "  Private Key: listener-certs/default-listener.key"
