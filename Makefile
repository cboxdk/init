.PHONY: build build-all clean test test-all test-integration bench coverage lint deps install dev help \
	check check-configs cover-check fmt fmt-check vet vulncheck sbom sbom-check license-check

# Build variables
BINARY_NAME=cbox-init
# Derive the version from git so locally built binaries report the real
# version (e.g. v2.3.1-3-gabc123). Release CI overrides this with the tag.
# Override explicitly with `make build VERSION=x.y.z` if needed.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//' || echo dev)
BUILD_DIR=build
CMD_DIR=cmd/cbox-init
# Example config used by `make dev`
DEV_CONFIG ?= configs/examples/minimal.yaml

# Go build flags for static binary
LDFLAGS=-ldflags "-w -s -X main.version=$(VERSION)"
STATIC_FLAGS=CGO_ENABLED=0

# Build the binary
build:
	@echo "🔨 Building Cbox Init..."
	@mkdir -p $(BUILD_DIR)
	$(STATIC_FLAGS) go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./$(CMD_DIR)
	@echo "✅ Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

# Build for multiple platforms
build-all:
	@echo "🔨 Building for all platforms..."
	@mkdir -p $(BUILD_DIR)

	@echo "Building for Linux AMD64..."
	GOOS=linux GOARCH=amd64 $(STATIC_FLAGS) go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./$(CMD_DIR)

	@echo "Building for Linux ARM64..."
	GOOS=linux GOARCH=arm64 $(STATIC_FLAGS) go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./$(CMD_DIR)

	@echo "Building for macOS AMD64..."
	GOOS=darwin GOARCH=amd64 $(STATIC_FLAGS) go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 ./$(CMD_DIR)

	@echo "Building for macOS ARM64..."
	GOOS=darwin GOARCH=arm64 $(STATIC_FLAGS) go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./$(CMD_DIR)

	@echo "✅ All builds complete"
	@ls -lh $(BUILD_DIR)

# Clean build artifacts
clean:
	@echo "🧹 Cleaning build artifacts..."
	@rm -rf $(BUILD_DIR)
	@echo "✅ Clean complete"

# Run tests
test:
	@echo "🧪 Running tests..."
	go test -v -race -coverprofile=coverage.out ./...
	@echo "✅ Tests complete"

# Run all tests (unit + integration)
test-all:
	@echo "🧪 Running complete test suite..."
	@chmod +x tests/run-all-tests.sh
	@./tests/run-all-tests.sh

# Validate the shipped example configs against the real schema, so documented
# configuration cannot drift from what the binary accepts. Builds if needed.
check-configs: build
	@echo "🧾 Validating example configs..."
	@BIN=$(BUILD_DIR)/$(BINARY_NAME) ./tools/check-doc-configs.sh

# Run integration tests
test-integration:
	@echo "🧪 Running integration tests..."
	@for distro in alpine debian ubuntu; do \
		echo "Testing on $$distro..."; \
		docker build -f tests/integration/Dockerfile.$$distro -t cbox-init-test-$$distro . && \
		docker run --rm cbox-init-test-$$distro || exit 1; \
	done
	@echo "✅ All integration tests passed"

# Run benchmarks
bench:
	@echo "⚡ Running benchmarks..."
	go test -bench=. -benchmem ./...

# Check test coverage
coverage:
	@echo "📊 Generating coverage report..."
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "✅ Coverage report: coverage.html"

# Minimum total statement coverage the gate enforces. This is a regression
# backstop, not the target — the goal is >80% (see CLAUDE.md); the floor sits
# a little below so ordinary fluctuation does not fail the build, while a real
# drop does. Raise it as coverage climbs.
COVERAGE_FLOOR ?= 78.0

# cover-check reads the coverage.out written by `test` and fails if total
# statement coverage fell below the floor. Run `test` first (the `check` target
# does).
cover-check:
	@echo "📊 Enforcing coverage floor ($(COVERAGE_FLOOR)%)..."
	@test -f coverage.out || { echo "❌ coverage.out not found; run 'make test' first"; exit 1; }
	@total=$$(go tool cover -func=coverage.out | awk '/^total:/{gsub(/%/,"",$$NF); print $$NF}'); \
	awk -v t="$$total" -v f="$(COVERAGE_FLOOR)" 'BEGIN{ if (t+0 < f+0) { printf "❌ total coverage %.1f%% is below the %.1f%% floor\n", t, f; exit 1 } printf "✅ total coverage %.1f%% meets the %.1f%% floor\n", t, f }'

# The full gate, matching CI. Run this before pushing.
check: fmt-check vet lint test cover-check vulncheck sbom-check license-check check-configs
	@echo "✅ All checks passed"

fmt:
	@echo "🎨 Formatting..."
	gofmt -w .

fmt-check:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "❌ Not gofmt-clean:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi
	@echo "✅ gofmt clean"

vet:
	@echo "🔍 Vetting..."
	go vet ./...

# Known-vulnerable dependencies, and the standard library the binary is built
# against. Reports against the toolchain in use, so keep it in step with CI.
vulncheck:
	@echo "🛡️  Scanning for vulnerabilities..."
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

# Deterministic CycloneDX 1.5 SBOM: no serial number, no timestamp, and
# normalised by tools/sbomnorm, so it only changes when dependencies do.
sbom:
	@echo "📦 Generating SBOM..."
	go run github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@v1.10.0 \
		mod -json -licenses -assert-licenses -noserial -notimestamp \
		-output-version 1.5 -output sbom.json .
	go run ./tools/sbomnorm sbom.json

sbom-check: sbom
	git diff --exit-code sbom.json
	@echo "✅ SBOM current"

license-check:
	@go run ./tools/licensecheck

# Lint (matches CI). Requires golangci-lint (https://golangci-lint.run).
lint:
	@echo "🔍 Linting..."
	@command -v golangci-lint >/dev/null 2>&1 || { echo "❌ golangci-lint not found. Install: https://golangci-lint.run/welcome/install/"; exit 1; }
	golangci-lint run ./...
	@echo "✅ Lint complete"

# Install dependencies
deps:
	@echo "📦 Installing dependencies..."
	go mod download
	go mod tidy
	@echo "✅ Dependencies installed"

# Install binary to system
install: build
	@echo "📥 Installing $(BINARY_NAME)..."
	@cp $(BUILD_DIR)/$(BINARY_NAME) /usr/local/bin/$(BINARY_NAME)
	@echo "✅ Installed to /usr/local/bin/$(BINARY_NAME)"

# Development: build and run against an example config
dev: build
	@echo "🚀 Running $(BINARY_NAME) with $(DEV_CONFIG)..."
	@$(BUILD_DIR)/$(BINARY_NAME) serve --config $(DEV_CONFIG)

# Show help
help:
	@echo "Cbox Init - Make targets:"
	@echo "  build            - Build binary for current platform (version: $(VERSION))"
	@echo "  build-all        - Build for all platforms (Linux, macOS, AMD64, ARM64)"
	@echo "  clean            - Remove build artifacts"
	@echo "  test             - Run unit tests (race + coverage)"
	@echo "  test-all         - Run full suite (unit + functional/API, needs Docker)"
	@echo "  test-integration - Run integration tests on alpine/debian/ubuntu (needs Docker)"
	@echo "  check-configs    - Validate configs/examples/*.yaml against the real schema"
	@echo "  bench            - Run benchmarks"
	@echo "  coverage         - Generate HTML coverage report"
	@echo "  lint             - Run golangci-lint (matches CI)"
	@echo "  deps             - Install/update dependencies"
	@echo "  install          - Install binary to /usr/local/bin"
	@echo "  dev              - Build and run against $(DEV_CONFIG)"
	@echo "  help             - Show this help message"
