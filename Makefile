# FastReader Makefile

# Default target
.DEFAULT_GOAL := help

# Variable definitions
BINARY_NAME := fastreader
SOURCE_FILE := main.go
BUILD_DIR := dist
LDFLAGS := -s -w
BUILD_FLAGS := -trimpath

# Color definitions
RED := \033[0;31m
GREEN := \033[0;32m
YELLOW := \033[1;33m
BLUE := \033[0;34m
NC := \033[0m # No Color

# Help information
.PHONY: help
help:
	@echo "$(BLUE)FastReader Build Tool$(NC)"
	@echo ""
	@echo "$(YELLOW)Available targets:$(NC)"
	@echo "  help           Show this help message"
	@echo "  build          Build for current platform"
	@echo "  clean          Clean build files"
	@echo "  test           Run tests"
	@echo ""
	@echo "$(YELLOW)Cross-compilation targets:$(NC)"
	@echo "  linux-amd64    Build Linux 64-bit version"
	@echo "  linux-386      Build Linux 32-bit version"
	@echo "  linux-arm64    Build Linux ARM64 version"
	@echo "  linux-arm      Build Linux ARM version"
	@echo "  windows-amd64  Build Windows 64-bit version"
	@echo "  windows-386    Build Windows 32-bit version"
	@echo "  darwin-amd64   Build macOS Intel version"
	@echo "  darwin-arm64   Build macOS Apple Silicon version"
	@echo "  freebsd-amd64  Build FreeBSD 64-bit version"
	@echo ""
	@echo "$(YELLOW)Batch builds:$(NC)"
	@echo "  all            Build all platform versions"
	@echo "  release        Build release versions (all platforms)"
	@echo ""
	@echo "$(YELLOW)Examples:$(NC)"
	@echo "  make build              # Build for current platform"
	@echo "  make linux-amd64        # Build Linux 64-bit"
	@echo "  make all                # Build all platforms"

# Check Go environment
.PHONY: check-go
check-go:
	@which go > /dev/null || (echo "$(RED)Error: Go compiler not found$(NC)" && exit 1)
	@echo "$(BLUE)Using Go version: $(shell go version | cut -d' ' -f3)$(NC)"

# Create build directory
$(BUILD_DIR):
	@mkdir -p $(BUILD_DIR)

# Build current platform version
.PHONY: build
build: check-go
	@echo "$(BLUE)Building current platform version...$(NC)"
	@CGO_ENABLED=0 go build $(BUILD_FLAGS) -ldflags "$(LDFLAGS)" -o $(BINARY_NAME) $(SOURCE_FILE)
	@echo "$(GREEN)Build completed: $(BINARY_NAME)$(NC)"

# Cross-compilation function
define build-target
	@echo "$(BLUE)Building $(1)-$(2)...$(NC)"
	@mkdir -p $(BUILD_DIR)
	GOOS=$(1) GOARCH=$(2) CGO_ENABLED=0 go build $(BUILD_FLAGS) -ldflags "$(LDFLAGS)" \
		-o $(BUILD_DIR)/$(BINARY_NAME)-$(1)-$(2)$(if $(filter windows,$(1)),.exe) $(SOURCE_FILE)
	@echo "$(GREEN)Completed: $(BUILD_DIR)/$(BINARY_NAME)-$(1)-$(2)$(if $(filter windows,$(1)),.exe)$(NC)"
endef

# Linux targets
.PHONY: linux-amd64
linux-amd64: check-go $(BUILD_DIR)
	$(call build-target,linux,amd64)

.PHONY: linux-386
linux-386: check-go $(BUILD_DIR)
	$(call build-target,linux,386)

.PHONY: linux-arm64
linux-arm64: check-go $(BUILD_DIR)
	$(call build-target,linux,arm64)

.PHONY: linux-arm
linux-arm: check-go $(BUILD_DIR)
	$(call build-target,linux,arm)

# Windows targets
.PHONY: windows-amd64
windows-amd64: check-go $(BUILD_DIR)
	$(call build-target,windows,amd64)

.PHONY: windows-386
windows-386: check-go $(BUILD_DIR)
	$(call build-target,windows,386)

# macOS targets
.PHONY: darwin-amd64
darwin-amd64: check-go $(BUILD_DIR)
	$(call build-target,darwin,amd64)

.PHONY: darwin-arm64
darwin-arm64: check-go $(BUILD_DIR)
	$(call build-target,darwin,arm64)

# FreeBSD targets
.PHONY: freebsd-amd64
freebsd-amd64: check-go $(BUILD_DIR)
	$(call build-target,freebsd,amd64)

# Build all platforms
.PHONY: all
all: linux-amd64 linux-386 linux-arm64 linux-arm windows-amd64 windows-386 darwin-amd64 darwin-arm64 freebsd-amd64
	@echo "$(GREEN)All platforms build completed!$(NC)"
	@echo "$(BLUE)Build files are located in $(BUILD_DIR)/ directory$(NC)"

# Release version (with compression)
.PHONY: release
release: all
	@echo "$(BLUE)Creating release packages...$(NC)"
	@cd $(BUILD_DIR) && \
	for file in $(BINARY_NAME)-*; do \
		if [[ "$$file" == *.exe ]]; then \
			zip "$${file%.exe}.zip" "$$file"; \
		else \
			tar -czf "$$file.tar.gz" "$$file"; \
		fi; \
		echo "$(GREEN)Packaged: $$file$(NC)"; \
	done
	@echo "$(GREEN)Release packages created!$(NC)"

# Clean build files
.PHONY: clean
clean:
	@echo "$(YELLOW)Cleaning build files...$(NC)"
	@rm -rf $(BUILD_DIR)
	@rm -f $(BINARY_NAME)
	@echo "$(GREEN)Cleanup completed$(NC)"

# Run tests
.PHONY: test
test: check-go
	@echo "$(BLUE)Running tests...$(NC)"
	@echo "Creating test file..."
	@echo -e "Line 1 test data\nLine 2 test data\nLine 3 test data\nLine 4 test data\nLine 5 test data" > test_makefile.csv
	@echo "$(BLUE)Building program...$(NC)"
	@$(MAKE) build > /dev/null
	@echo "$(BLUE)Testing index generation...$(NC)"
	@./$(BINARY_NAME) test_makefile.csv gen
	@echo "$(BLUE)Testing line count...$(NC)"
	@./$(BINARY_NAME) test_makefile.csv cnt
	@echo "$(BLUE)Testing line query...$(NC)"
	@echo "First line:"
	@./$(BINARY_NAME) test_makefile.csv 1
	@echo "Last line:"
	@./$(BINARY_NAME) test_makefile.csv -1
	@echo "$(GREEN)All tests passed!$(NC)"
	@rm -f test_makefile.csv test_makefile.csv.frc

# Show build information
.PHONY: info
info:
	@echo "$(BLUE)Build information:$(NC)"
	@echo "  Binary name: $(BINARY_NAME)"
	@echo "  Source file: $(SOURCE_FILE)"
	@echo "  Build directory: $(BUILD_DIR)"
	@echo "  Build flags: $(BUILD_FLAGS)"
	@echo "  Link flags: $(LDFLAGS)"
	@echo "  Go version: $(shell go version 2>/dev/null || echo 'Not installed')"
