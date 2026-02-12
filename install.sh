#!/bin/bash
set -e

# Skyline MCP Installer
# Usage: curl -fsSL https://skyline.projex.cc/install | bash
# Or build from source: curl -fsSL https://skyline.projex.cc/install | bash -s source

VERSION="latest"
BUILD_FROM_SOURCE=false

# Check for source build flag
if [ "$1" = "source" ]; then
  BUILD_FROM_SOURCE=true
fi

# Detect OS
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
  linux) OS="linux" ;;
  darwin) OS="darwin" ;;
  *) echo "❌ Unsupported OS: $OS"; exit 1 ;;
esac

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "❌ Unsupported architecture: $ARCH"; exit 1 ;;
esac

echo "🚀 Installing Skyline MCP..."
echo "   Platform: ${OS}-${ARCH}"

if [ "$BUILD_FROM_SOURCE" = true ]; then
  echo "   Mode: Build from source"
  echo ""
  
  # Check for Go
  if ! command -v go &> /dev/null; then
    echo "❌ Go not found. Install Go 1.23+ first:"
    echo "   https://go.dev/dl/"
    exit 1
  fi
  
  GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
  echo "   Go version: $GO_VERSION"
  
  # Clone and build
  echo ""
  echo "📦 Cloning repository..."
  TEMP_DIR=$(mktemp -d)
  cd "$TEMP_DIR"
  
  if ! git clone --depth 1 https://github.com/emadomedher/skyline-mcp.git; then
    echo "❌ Failed to clone repository"
    exit 1
  fi
  
  cd skyline-mcp
  
  echo "🔨 Building binaries..."
  go build -ldflags="-s -w" -o skyline ./cmd/skyline
  go build -ldflags="-s -w" -o skyline-server ./cmd/skyline-server
  
  # Move to install location
  if [ -w /usr/local/bin ]; then
    mv skyline /usr/local/bin/skyline
    mv skyline-server /usr/local/bin/skyline-server
    echo ""
    echo "✅ Installed to /usr/local/bin/"
  else
    mkdir -p "$HOME/.local/bin"
    mv skyline "$HOME/.local/bin/skyline"
    mv skyline-server "$HOME/.local/bin/skyline-server"
    echo ""
    echo "✅ Installed to $HOME/.local/bin/"
    echo "⚠️  Add to PATH: export PATH=\"\$HOME/.local/bin:\$PATH\""
  fi
  
  # Cleanup
  cd "$HOME"
  rm -rf "$TEMP_DIR"
  
else
  echo "   Mode: Pre-built binary"
  echo ""
  
  BINARY="skyline-${OS}-${ARCH}"
  URL="https://github.com/emadomedher/skyline-mcp/releases/latest/download/${BINARY}"
  
  echo "📥 Downloading from GitHub releases..."
  
  # Download binary
  if command -v curl &> /dev/null; then
    if ! curl -fsSL "$URL" -o skyline; then
      echo "❌ Download failed. Check release exists for ${OS}-${ARCH}"
      exit 1
    fi
  elif command -v wget &> /dev/null; then
    if ! wget -q "$URL" -O skyline; then
      echo "❌ Download failed. Check release exists for ${OS}-${ARCH}"
      exit 1
    fi
  else
    echo "❌ curl or wget required"
    exit 1
  fi
  
  chmod +x skyline
  
  # Move to install location
  if [ -w /usr/local/bin ]; then
    mv skyline /usr/local/bin/skyline
    echo ""
    echo "✅ Installed to /usr/local/bin/skyline"
  else
    mkdir -p "$HOME/.local/bin"
    mv skyline "$HOME/.local/bin/skyline"
    echo ""
    echo "✅ Installed to $HOME/.local/bin/skyline"
    echo "⚠️  Add to PATH: export PATH=\"\$HOME/.local/bin:\$PATH\""
  fi
fi

echo ""
echo "🎉 Skyline MCP installed successfully!"
echo ""
echo "📝 Next steps:"
echo "   1. Create config.yaml with your API specs"
echo "   2. Run: skyline --config=config.yaml"
echo ""
echo "📚 Documentation: https://skyline.projex.cc/docs"
echo "💡 Examples: https://github.com/emadomedher/skyline-mcp/tree/main/examples"
