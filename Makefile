# Read version from VERSION file
VERSION := $(shell cat VERSION)

# Cloud endpoint — set CLOUD_ENDPOINT env var for non-production builds:
#   CLOUD_ENDPOINT=https://your-dev-url make build
CLOUD_ENDPOINT ?= https://cloud.xskyline.com

# Build flags to inject version and cloud endpoint
LDFLAGS := -ldflags "-X main.Version=$(VERSION) -X skyline-mcp/internal/serverconfig.DefaultCloudEndpoint=$(CLOUD_ENDPOINT) -s -w"

.PHONY: all build install test clean version

all: build

# Build binary. Set CLOUD_ENDPOINT env var to override the default production endpoint.
build:
	@echo "Building skyline v$(VERSION)..."
	go build $(LDFLAGS) -o bin/skyline ./cmd/skyline
	@echo "✅ Built bin/skyline"

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
