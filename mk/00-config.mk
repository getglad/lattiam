# Configuration and Environment Variables
# This file must load first to set up build configuration

# Project information
PROJECT_NAME := lattiam
BINARY_NAME := lattiam
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# Go configuration
GO_VERSION := 1.21
GOFLAGS := -mod=readonly
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

# Build configuration
BUILD_DIR := build
DIST_DIR := dist
COVERAGE_DIR := coverage
DOCKER_REGISTRY ?= lattiam
DOCKER_TAG ?= $(VERSION)

# Paths
CMD_DIR := ./cmd
PKG_DIR := ./pkg
INTERNAL_DIR := ./internal
DOCS_DIR := ./docs
EXAMPLES_DIR := ./examples

# Build flags
LDFLAGS := -ldflags="-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME) -X main.gitCommit=$(GIT_COMMIT)"

# Test configuration
TEST_TIMEOUT := 10m
COVERAGE_THRESHOLD := 80

# Linting and formatting
GOLANGCI_LINT_VERSION := v1.55.2

# Colors for output
RED := \033[31m
GREEN := \033[32m
YELLOW := \033[33m
BLUE := \033[34m
MAGENTA := \033[35m
CYAN := \033[36m
WHITE := \033[37m
RESET := \033[0m

# Export variables for sub-makes
export PROJECT_NAME
export VERSION
export BUILD_TIME
export GIT_COMMIT
export GOFLAGS
export LDFLAGS