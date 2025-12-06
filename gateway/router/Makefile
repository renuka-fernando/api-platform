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

# Makefile for Router (Envoy Proxy)

# Version management
VERSION ?= $(shell cat ../VERSION 2>/dev/null || echo "0.0.1-SNAPSHOT")

# Detect local architecture
LOCAL_ARCH := $(shell uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')

# Docker image configuration
DOCKER_REGISTRY ?= ghcr.io/renuka-fernando/api-platform
IMAGE_NAME := $(DOCKER_REGISTRY)/gateway-router

.PHONY: help clean build-base build-base-amd64 build-base-arm64

help: ## Show this help message
	@echo 'Gateway Router (Version: $(VERSION))'
	@echo ''
	@echo 'Note: Final gateway-router images are built via gateway-builder.'
	@echo 'For production multi-arch builds, use: make -C .. build-and-push-multiarch'
	@echo ''
	@echo 'Development Targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build-base: ## Build router base image for local architecture
	@echo "Building router base image for local architecture ($(LOCAL_ARCH))..."
	@docker buildx build \
		--build-arg VERSION=$(VERSION) \
		-t $(DOCKER_REGISTRY)/gateway-router-base:$(VERSION)-$(LOCAL_ARCH) \
		-t $(DOCKER_REGISTRY)/gateway-router-base:$(VERSION) \
		-t $(DOCKER_REGISTRY)/gateway-router-base:latest \
		--load \
		.
	@echo "Base image built: $(DOCKER_REGISTRY)/gateway-router-base:$(VERSION)"

# Internal targets - called by parent gateway/Makefile only
# These build architecture-specific base images for gateway-builder to extend

build-base-amd64: ## Build router base image for amd64 (internal use)
	@echo "Building router base image for amd64 ($(VERSION))..."
	@docker buildx build \
		--platform linux/amd64 \
		--build-arg VERSION=$(VERSION) \
		-t $(DOCKER_REGISTRY)/gateway-router-base:$(VERSION)-amd64 \
		--load \
		.

build-base-arm64: ## Build router base image for arm64 (internal use)
	@echo "Building router base image for arm64 ($(VERSION))..."
	@docker buildx build \
		--platform linux/arm64 \
		--build-arg VERSION=$(VERSION) \
		-t $(DOCKER_REGISTRY)/gateway-router-base:$(VERSION)-arm64 \
		--load \
		.

clean: ## Clean Docker images
	@echo "Cleaning Docker images..."
	@docker rmi $(IMAGE_NAME):$(VERSION) || true
	@docker rmi $(IMAGE_NAME):latest || true
