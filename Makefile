# Read version from VERSION file
VERSION := $(shell cat VERSION)

# Build flags to inject version
LDFLAGS := -ldflags "-X main.Version=$(VERSION) -s -w"

.PHONY: all build install test clean version

all: build

# Build unified binary (HTTP + Admin UI + STDIO modes)
build:
	@echo "Building skyline v$(VERSION) (unified binary)..."
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
