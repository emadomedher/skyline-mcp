# Read version from VERSION file
VERSION := $(shell cat VERSION)

# Build flags to inject version
LDFLAGS := -ldflags "-X main.Version=$(VERSION) -s -w"

.PHONY: all build build-skyline build-server install test clean version

all: build

# Build both binaries
build: build-skyline build-server

# Build skyline (MCP server)
build-skyline:
	@echo "Building skyline v$(VERSION)..."
	go build $(LDFLAGS) -o bin/skyline ./cmd/skyline

# Build skyline-server (config server + web UI)
build-server:
	@echo "Building skyline-server v$(VERSION)..."
	go build $(LDFLAGS) -o bin/skyline-server ./cmd/skyline-server

# Install to system
install: build
	@echo "Installing binaries..."
	cp bin/skyline /usr/local/bin/skyline
	cp bin/skyline-server /usr/local/bin/skyline-server
	@echo "Installed skyline v$(VERSION)"

# Run tests
test:
	go test ./...

# Clean build artifacts
clean:
	rm -rf bin/
	rm -f skyline skyline-server

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
