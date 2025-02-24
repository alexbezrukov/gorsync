# Project settings
APP_NAME = gorsync
BUILD_DIR = build
SOURCE_DIR = cmd

# Default Go settings
GO ?= go
GOFLAGS ?=

# Platform-specific build settings
BINARY_LINUX = $(BUILD_DIR)/$(APP_NAME)-linux
BINARY_WINDOWS = $(BUILD_DIR)/$(APP_NAME).exe
BINARY_MACOS = $(BUILD_DIR)/$(APP_NAME)-darwin

.PHONY: all build clean run test

# Default target
all: build-all

# Build the application for the current platform
build:
	@echo "Building $(APP_NAME)..."
	$(GO) build -o $(BUILD_DIR)/$(APP_NAME) $(SOURCE_DIR)/main.go
	@echo "Build complete. Binary available at $(BUILD_DIR)/$(APP_NAME)"

# Run the application (server mode)
run-server:
	@echo "Starting server..."
	$(BUILD_DIR)/$(APP_NAME) server

# Run the application (client mode)
run-client:
	@echo "Starting client..."
	$(BUILD_DIR)/$(APP_NAME) client --source=/path/to/source --dest=127.0.0.1:8080

# Build binaries for multiple platforms
build-linux:
	@echo "Building Linux binary..."
	GOOS=linux GOARCH=amd64 $(GO) build -o $(BINARY_LINUX) $(SOURCE_DIR)/main.go

build-windows:
	@echo "Building Windows binary..."
	$(GO) build -o $(BINARY_WINDOWS) $(SOURCE_DIR)/main.go

build-macos:
	@echo "Building macOS binary..."
	GOOS=darwin GOARCH=amd64 $(GO) build -o $(BINARY_MACOS) $(SOURCE_DIR)/main.go

# Build all platform binaries
build-all: build-linux build-windows build-macos
	@echo "All platform builds complete. Binaries available in $(BUILD_DIR)/"

# Test the project
test:
	@echo "Running tests..."
	$(GO) test ./...

# Clean up build files
clean:
	@echo "Cleaning up..."
	rm -rf $(BUILD_DIR)
	rm -rf config.yaml
	@echo "Clean complete."
