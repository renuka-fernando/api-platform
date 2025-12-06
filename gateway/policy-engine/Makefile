# Makefile for Envoy Policy Engine
#
# NOTE: Policy Engine Docker images are built via gateway-builder.
# This Makefile only contains runtime and testing targets.
# To build policy-engine, use: make -C .. build

# Variables
VERSION ?= $(shell cat ../VERSION 2>/dev/null || echo "0.0.1-SNAPSHOT")
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -X main.Version=$(VERSION) -X main.GitCommit=$(GIT_COMMIT) -X main.BuildDate=$(BUILD_DATE)
DOCKER_REGISTRY ?= ghcr.io/renuka-fernando/api-platform
RUNTIME_IMAGE := $(DOCKER_REGISTRY)/policy-engine
RUNTIME_VERSION := $(VERSION)

# Docker image tags
RUNTIME_TAG := $(RUNTIME_IMAGE):$(RUNTIME_VERSION)

# Go variables
GOCMD := go
GOBUILD := $(GOCMD) build
GOCLEAN := $(GOCMD) clean
GOTEST := $(GOCMD) test
GOMOD := $(GOCMD) mod

# Binary name
BINARY_NAME := policy-engine

.PHONY: all test clean docker-clean help \
	run stop tidy lint logs ps restart rebuild check-deps \
	build-sample-setheader test-integration test-envoy clean-all version

# Default target
all: help

## help: Display this help message
help:
	@echo "Envoy Policy Engine (Version: $(VERSION))"
	@echo ""
	@echo "Note: Policy Engine Docker images are built via gateway-builder."
	@echo "To build policy-engine, use: make -C .. build"
	@echo ""
	@echo "Runtime Targets:"
	@echo "  make run                  - Run the Policy Engine with docker-compose"
	@echo "  make stop                 - Stop the running Policy Engine services"
	@echo "  make restart              - Restart all services"
	@echo "  make rebuild              - Rebuild and restart"
	@echo ""
	@echo "Development Targets:"
	@echo "  make test                 - Run tests"
	@echo "  make clean                - Clean build artifacts"
	@echo "  make docker-clean         - Remove all Docker images and containers"
	@echo ""
	@echo "Other Targets:"
	@echo "  make tidy                 - Run go mod tidy"
	@echo "  make lint                 - Run Go linter (if installed)"
	@echo "  make logs                 - View docker-compose logs"
	@echo "  make ps                   - Show running services"
	@echo ""

## run: Start the Policy Engine with docker-compose
run:
	@echo "Starting Policy Engine services..."
	docker-compose up -d
	@echo "Services started. Access Envoy at http://localhost:10000"
	@echo "   - Envoy Admin: http://localhost:9901"
	@echo "   - Policy Engine ext_proc: localhost:9001"
	@echo ""
	@echo "View logs with: make logs"

## stop: Stop the Policy Engine services
stop:
	@echo "Stopping Policy Engine services..."
	docker-compose down
	@echo "Services stopped"

## test: Run tests
test:
	@echo "Running tests..."
	$(GOTEST) -v ./...
	@echo "Tests completed"

## clean: Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	$(GOCLEAN)
	rm -f $(BINARY_NAME)
	@echo "Clean completed"

## docker-clean: Remove all Docker images and containers
docker-clean:
	@echo "Removing Docker containers..."
	-docker-compose down -v
	@echo "Removing Docker images..."
	-docker rmi $(RUNTIME_TAG) 2>/dev/null || true
	@echo "Docker cleanup completed"

## tidy: Run go mod tidy
tidy:
	@echo "Running go mod tidy..."
	$(GOMOD) tidy
	cd ../../sdk && $(GOMOD) tidy
	@echo "go mod tidy completed for all modules"

## lint: Run Go linter (requires golangci-lint)
lint:
	@echo "Running linter..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
		echo "Linting completed"; \
	else \
		echo "⚠️  golangci-lint not installed. Install with:"; \
		echo "   go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
	fi

## logs: View docker-compose logs
logs:
	@echo "Viewing service logs (Ctrl+C to exit)..."
	docker-compose logs -f

## ps: Show running services
ps:
	@echo "Running services:"
	docker-compose ps

## restart: Restart all services
restart: stop run

## rebuild: Rebuild and restart (Note: Run 'make -C .. build' first to rebuild images)
rebuild: restart

# Development helpers

## check-deps: Check if required tools are installed
check-deps:
	@echo "Checking dependencies..."
	@command -v docker >/dev/null 2>&1 || { echo "❌ docker is required but not installed"; exit 1; }
	@command -v docker-compose >/dev/null 2>&1 || { echo "❌ docker-compose is required but not installed"; exit 1; }
	@command -v go >/dev/null 2>&1 || { echo "❌ go is required but not installed"; exit 1; }
	@echo "All required dependencies are installed"

# Sample policy builds

## build-sample-setheader: Build SetHeader sample policy
build-sample-setheader:
	@echo "Building SetHeader sample policy..."
	cd sample-policies/set-header/v1.0.0 && $(GOMOD) tidy
	@echo "SetHeader policy ready"

# Testing targets

## test-integration: Run integration tests (requires docker-compose)
test-integration: run
	@echo "Running integration tests..."
	@sleep 5  # Wait for services to be ready
	@echo "Testing Envoy endpoint..."
	@curl -s -o /dev/null -w "Status: %{http_code}\n" http://localhost:10000/ || true
	@echo "Testing Envoy admin..."
	@curl -s -o /dev/null -w "Status: %{http_code}\n" http://localhost:9901/ || true
	@echo "Integration tests completed"

## test-envoy: Test Envoy configuration
test-envoy:
	@echo "Testing Envoy configuration..."
	docker run --rm -v $(PWD)/configs/envoy.yaml:/etc/envoy/envoy.yaml \
		envoyproxy/envoy:v1.36.2 \
		--mode validate -c /etc/envoy/envoy.yaml
	@echo "Envoy configuration is valid"

# Cleanup targets

## clean-all: Clean everything (build artifacts + Docker)
clean-all: clean docker-clean
	@echo "Complete cleanup finished"

# Version and info

## version: Show version information
version:
	@echo "Policy Engine Build Information"
	@echo "================================"
	@echo "Image: $(RUNTIME_TAG)"
	@echo "Go Version: $(shell go version)"
	@echo "Docker Version: $(shell docker --version)"
	@echo "Docker Compose Version: $(shell docker-compose --version)"
