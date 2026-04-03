# Read version from VERSION file
VERSION := $(shell cat VERSION)

# Cloud endpoint — override for dev builds:
#   make build CLOUD_ENDPOINT=https://your-dev-endpoint
CLOUD_ENDPOINT ?= https://cloud.xskyline.com

# Build flags to inject version and cloud endpoint
LDFLAGS := -ldflags "-X main.Version=$(VERSION) -X skyline-mcp/internal/serverconfig.DefaultCloudEndpoint=$(CLOUD_ENDPOINT) -s -w"

.PHONY: all build build-dev install test clean version

all: build

# Build for production (default endpoint: cloud.xskyline.com)
build:
	@echo "Building skyline v$(VERSION) → $(CLOUD_ENDPOINT)..."
	go build $(LDFLAGS) -o bin/skyline ./cmd/skyline
	@echo "✅ Built bin/skyline"

# Build for development (endpoint: your-dev-endpoint)
build-dev:
	@$(MAKE) build CLOUD_ENDPOINT=https://your-dev-endpoint

# Install to system
install: build
	@echo "Installing skyline..."
	cp bin/skyline /usr/local/bin/skyline
	@echo "✅ Installed skyline v$(VERSION) to /usr/local/bin/"

# Run tests
test:
	go test ./...

# Clean build artifacts
clean:
	rm -rf bin/
	rm -f skyline

# Show current version
version:
	@echo "$(VERSION)"

# Bump patch version (0.1.0 -> 0.1.1)
bump-patch:
	@echo "Current version: $(VERSION)"
	@NEW_VERSION=$$(echo $(VERSION) | awk -F. '{print $$1"."$$2"."$$3+1}'); \
	echo $$NEW_VERSION > VERSION; \
	echo "Bumped to: $$NEW_VERSION"

# Bump minor version (0.1.5 -> 0.2.0)
bump-minor:
	@echo "Current version: $(VERSION)"
	@NEW_VERSION=$$(echo $(VERSION) | awk -F. '{print $$1"."$$2+1".0"}'); \
	echo $$NEW_VERSION > VERSION; \
	echo "Bumped to: $$NEW_VERSION"

# Bump major version (0.5.3 -> 1.0.0)
bump-major:
	@echo "Current version: $(VERSION)"
	@NEW_VERSION=$$(echo $(VERSION) | awk -F. '{print $$1+1".0.0"}'); \
	echo $$NEW_VERSION > VERSION; \
	echo "Bumped to: $$NEW_VERSION"
