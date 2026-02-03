# QuotaGuard Makefile

.PHONY: all test unit-test integration-test integration-race clean build run setup

# Variables
BINARY_NAME=quotaguard
VERSION=$(shell git describe --tags --always 2>/dev/null || echo "dev")
LDFLAGS=-ldflags "-X main.Version=$(VERSION)"
COVERAGE_FILE=coverage.out

# Default target
all: test

# Run all tests
test: unit-test integration-test

# Run unit tests only
unit-test:
	@echo "Running unit tests..."
	go test ./... -v -coverprofile=$(COVERAGE_FILE) -covermode=atomic
	@echo "Unit tests completed. Coverage: $$(go tool cover -func=$(COVERAGE_FILE) | grep total | awk '{print $$3}')"

# Run integration tests
integration-test:
	@echo "Running integration tests..."
	go test -tags=integration ./test/integration/... -v -race -coverprofile=$(COVERAGE_FILE) -covermode=atomic
	@echo "Integration tests completed."

# Run tests with race detector
integration-race:
	@echo "Running integration tests with race detector..."
	go test -tags=integration ./test/integration/... -race -v
	@echo "Race detector completed."

# Run all tests with coverage report
coverage:
	@echo "Generating coverage report..."
	go test ./... -coverprofile=$(COVERAGE_FILE) -covermode=atomic
	go tool cover -html=$(COVERAGE_FILE) -o coverage.html
	@echo "Coverage report generated: coverage.html"
	@echo "Coverage summary:"
	@go tool cover -func=$(COVERAGE_FILE) | grep total

# Run tests in watch mode (requires watchexec)
watch:
	@echo "Watching for changes..."
	@if command -v watchexec >/dev/null 2>&1; then \
		watchexec -e go -r 'make test'; \
	else \
		echo "watchexec not found. Install with: go install github.com/watchexec/watchexec@latest"; \
	fi

# Build the binary
build:
	@echo "Building $(BINARY_NAME) $(VERSION)..."
	go build $(LDFLAGS) -o $(BINARY_NAME) ./cmd/quotaguard
	@echo "Build completed: $(BINARY_NAME)"

# Run the application
run: build
	@echo "Running QuotaGuard..."
	./$(BINARY_NAME)

# Run with custom config
run-config: build
	@echo "Running with custom config..."
	./$(BINARY_NAME) -config $(CONFIG)

# Setup auto-discovery
setup: build
	@echo "Running setup..."
	@if [ -n "$(AUTHS_PATH)" ]; then \
		./$(BINARY_NAME) setup "$(AUTHS_PATH)"; \
	else \
		./$(BINARY_NAME) setup; \
	fi

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -f $(BINARY_NAME) $(COVERAGE_FILE) coverage.html
	@echo "Clean completed."

# Lint the code
lint:
	@echo "Linting code..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not found. Install with: curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin"; \
	fi

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...
	@echo "Format completed."

# Run vet
vet:
	@echo "Running go vet..."
	go vet ./...
	@echo "Vet completed."

# Show help
help:
	@echo "QuotaGuard Makefile"
	@echo ""
	@echo "Targets:"
	@echo "  all              - Run all tests (default)"
	@echo "  test             - Run unit and integration tests"
	@echo "  unit-test        - Run unit tests with coverage"
	@echo "  integration-test - Run integration tests with race detector"
	@echo "  integration-race - Run integration tests with race detector (verbose)"
	@echo "  coverage         - Generate coverage report (HTML)"
	@echo "  watch            - Watch for changes and run tests"
	@echo "  build            - Build the binary"
	@echo "  run              - Build and run the application"
	@echo "  run-config       - Run with custom config file"
	@echo "  setup            - Auto-discover CLIProxyAPI auths"
	@echo "  clean            - Clean build artifacts"
	@echo "  lint             - Lint the code"
	@echo "  fmt              - Format code"
	@echo "  vet              - Run go vet"
	@echo "  help             - Show this help"
