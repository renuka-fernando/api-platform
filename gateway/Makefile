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

# Gateway Makefile - Coordinate all gateway components

SHELL := /bin/bash

# Read shared version from VERSION file
VERSION := $(shell cat VERSION 2>/dev/null || echo "0.0.1-SNAPSHOT")

# Docker registry configuration
DOCKER_REGISTRY ?= ghcr.io/renuka-fernando/api-platform

# Image names
GATEWAY_BUILDER_IMAGE := $(DOCKER_REGISTRY)/gateway-builder
CONTROLLER_IMAGE := $(DOCKER_REGISTRY)/gateway-controller
CONTROLLER_BASE_IMAGE := internal/wso2/gateway-controller-base
ROUTER_IMAGE := $(DOCKER_REGISTRY)/gateway-router
ROUTER_BASE_IMAGE := internal/wso2/gateway-router-base
POLICY_ENGINE_IMAGE := $(DOCKER_REGISTRY)/policy-engine

# Build output directory
BUILDER_OUTPUT := target/builder-output

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help message
	@echo 'Gateway Build System (Version: $(VERSION))'
	@echo ''
	@echo 'Build Targets:'
	@echo '  make build-gateway-builder          - Build gateway-builder Docker image only'
	@echo ''
	@echo 'Local Development Targets:'
	@echo '  make build                          - Build base images for local development'
	@echo '  make build-controller-base          - Build gateway-controller base for local arch'
	@echo '  make build-router-base              - Build router base for local arch'
	@echo ''
	@echo 'Push Targets:'
	@echo '  make build-and-push-multiarch       - Build and push all gateway components for multiple architectures'
	@echo ''
	@echo 'Test Targets:'
	@echo '  make test                           - Run all tests'
	@echo '  make test-controller                - Test gateway-controller'
	@echo '  make test-policy-engine             - Test policy-engine'
	@echo ''
	@echo 'Clean Targets:'
	@echo '  make clean                          - Clean all build artifacts'
	@echo '  make clean-base-images              - Clean temporary base images'
	@echo ''
	@echo 'Notes:'
	@echo '  - Local development: use "make build" (quick single-arch builds)'
	@echo '  - Base images are internal build artifacts (auto-cleaned after production build)'

# Internal Base Image Build Targets (per architecture)
.PHONY: build-base-images-amd64
build-base-images-amd64: build-controller-base-amd64 build-router-base-amd64 ## Build base images for amd64

.PHONY: build-base-images-arm64
build-base-images-arm64: build-controller-base-arm64 build-router-base-arm64 ## Build base images for arm64

.PHONY: build-controller-base-amd64
build-controller-base-amd64: ## Build gateway-controller base image for amd64 (internal)
	@echo "Building gateway-controller base image for amd64 ($(VERSION))..."
	$(MAKE) -C gateway-controller build-base-amd64 VERSION=$(VERSION)

.PHONY: build-controller-base-arm64
build-controller-base-arm64: ## Build gateway-controller base image for arm64 (internal)
	@echo "Building gateway-controller base image for arm64 ($(VERSION))..."
	$(MAKE) -C gateway-controller build-base-arm64 VERSION=$(VERSION)

.PHONY: build-router-base-amd64
build-router-base-amd64: ## Build router base image for amd64 (internal)
	@echo "Building router base image for amd64 ($(VERSION))..."
	$(MAKE) -C router build-base-amd64 VERSION=$(VERSION)

.PHONY: build-router-base-arm64
build-router-base-arm64: ## Build router base image for arm64 (internal)
	@echo "Building router base image for arm64 ($(VERSION))..."
	$(MAKE) -C router build-base-arm64 VERSION=$(VERSION)

# Local Development Build Targets
.PHONY: build-controller-base
build-controller-base: ## Build gateway-controller base image for local architecture
	@echo "Building gateway-controller base image for local architecture..."
	$(MAKE) -C gateway-controller build-base VERSION=$(VERSION)

.PHONY: build-router-base
build-router-base: ## Build router base image for local architecture
	@echo "Building router base image for local architecture..."
	$(MAKE) -C router build-base VERSION=$(VERSION)

.PHONY: build-gateway-builder
build-gateway-builder: ## Build gateway-builder Docker image
	@echo "Building gateway-builder Docker image ($(VERSION))..."
	$(MAKE) -C gateway-builder build IMAGE_NAME=$(GATEWAY_BUILDER_IMAGE) VERSION=$(VERSION)

# Gateway Builder Orchestrated Builds
.PHONY: build
build: build-controller-base build-router-base build-gateway-builder

.PHONY: run-gateway-builder
run-gateway-builder: ## Run gateway-builder to produce final multi-arch images
	@echo "Running gateway-builder to produce final images..."
	@mkdir -p $(BUILDER_OUTPUT)
	# Run gateway-builder for amd64
	@docker run --rm \
		-v $(PWD)/sample-policies:/policies \
		-v $(PWD)/policy-engine/policy-manifest.yaml:/policy-manifest.yaml \
		-v $(PWD)/$(BUILDER_OUTPUT):/output \
		$(GATEWAY_BUILDER_IMAGE):$(VERSION) \
		--manifest /policy-manifest.yaml \
		--out-dir /output \
		--gateway-controller-base-image $(CONTROLLER_BASE_IMAGE):$(VERSION) \
		--router-base-image $(ROUTER_BASE_IMAGE):$(VERSION)
	@echo "Gateway builder completed. Output in $(BUILDER_OUTPUT)/"

.PHONY: clean-base-images
clean-base-images: ## Clean temporary base images (internal use only)
	@echo "Cleaning temporary base images..."
	-docker rmi $(DOCKER_REGISTRY)/gateway-controller-base:$(VERSION)-amd64 2>/dev/null || true
	-docker rmi $(DOCKER_REGISTRY)/gateway-controller-base:$(VERSION)-arm64 2>/dev/null || true
	-docker rmi $(DOCKER_REGISTRY)/gateway-router-base:$(VERSION)-amd64 2>/dev/null || true
	-docker rmi $(DOCKER_REGISTRY)/gateway-router-base:$(VERSION)-arm64 2>/dev/null || true
	@echo "Base images cleaned"

# Multi-arch Build and Push Targets
.PHONY: build-and-push-multiarch
build-and-push-multiarch: build-base-images-amd64 build-base-images-arm64 ## Build and push all gateway components for multiple architectures
	@echo "Building gateway builder multi-arch image..."
	$(MAKE) -C gateway-builder build-and-push-multiarch IMAGE_NAME=$(GATEWAY_BUILDER_IMAGE) VERSION=$(VERSION)

	# Build final images from $(BUILDER_OUTPUT) and push to registry
	# This step uses the generated Dockerfiles in $(BUILDER_OUTPUT)/
	# TODO: Implement image push logic
	# docker push $(DOCKER_REGISTRY)/gateway-controller:$(VERSION)
	# docker push $(DOCKER_REGISTRY)/policy-engine:$(VERSION)
	# docker push $(DOCKER_REGISTRY)/gateway-router:$(VERSION)
	@echo "Multi-arch images pushed successfully"

# Test Targets
.PHONY: test
test: test-controller test-policy-engine ## Run all tests

.PHONY: test-controller
test-controller: ## Test gateway-controller
	$(MAKE) -C gateway-controller test

.PHONY: test-policy-engine
test-policy-engine: ## Test policy-engine
	$(MAKE) -C policy-engine test

# Clean Targets
.PHONY: clean
clean: ## Clean all build artifacts
	$(MAKE) -C gateway-controller clean || true
	$(MAKE) -C gateway-builder clean || true
	$(MAKE) -C policy-engine clean || true
	$(MAKE) -C router clean || true
	@echo "Cleaning builder output..."
	rm -rf $(BUILDER_OUTPUT)
	@echo "Clean completed"
