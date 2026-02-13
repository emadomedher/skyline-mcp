#!/bin/bash
set -e

# Skyline MCP Installer
# Usage: curl -fsSL https://skyline.projex.cc/install | bash
# Or build from source: curl -fsSL https://skyline.projex.cc/install | bash -s source

VERSION="latest"
BUILD_FROM_SOURCE=false

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

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
echo ""

# Check for Deno (required for code execution - 98% cost reduction)
if command -v deno &> /dev/null; then
  DENO_VERSION=$(deno --version | head -n1 | awk '{print $2}')
  echo -e "${GREEN}✓ Deno found:${NC} v${DENO_VERSION}"
  echo "  Code execution enabled (98% cost reduction)"
else
  echo -e "${YELLOW}⚠️  Deno not found${NC}"
  echo ""
  echo "Skyline uses code execution by default for 98% cost reduction."
  echo "Without Deno, it will fall back to traditional MCP (slower, more expensive)."
  echo ""
  echo -e "${BLUE}Would you like to install Deno now? (recommended)${NC}"
  
  if [ -t 0 ]; then
    # Interactive terminal
    read -p "Install Deno? (Y/n): " -n 1 -r
    echo ""
  else
    # Non-interactive (curl pipe) - default to yes
    echo "Non-interactive mode detected. Installing Deno..."
    REPLY="y"
  fi
  
  if [[ $REPLY =~ ^[Yy]$ ]] || [[ -z $REPLY ]]; then
    echo ""
    echo -e "${BLUE}📥 Installing Deno...${NC}"
    
    if curl -fsSL https://deno.land/install.sh | sh; then
      # Add Deno to PATH for this session
      export DENO_INSTALL="$HOME/.deno"
      export PATH="$DENO_INSTALL/bin:$PATH"
      
      DENO_VERSION=$(deno --version 2>/dev/null | head -n1 | awk '{print $2}')
      echo -e "${GREEN}✓ Deno installed:${NC} v${DENO_VERSION}"
      echo "  Code execution enabled (98% cost reduction)"
      
      # Add to shell profile if not already there
      SHELL_RC=""
      if [ -f "$HOME/.bashrc" ]; then
        SHELL_RC="$HOME/.bashrc"
      elif [ -f "$HOME/.zshrc" ]; then
        SHELL_RC="$HOME/.zshrc"
      fi
      
      if [ -n "$SHELL_RC" ]; then
        if ! grep -q 'DENO_INSTALL' "$SHELL_RC"; then
          echo "" >> "$SHELL_RC"
          echo '# Deno' >> "$SHELL_RC"
          echo 'export DENO_INSTALL="$HOME/.deno"' >> "$SHELL_RC"
          echo 'export PATH="$DENO_INSTALL/bin:$PATH"' >> "$SHELL_RC"
          echo -e "${GREEN}✓ Added Deno to ${SHELL_RC}${NC}"
        fi
      fi
    else
      echo -e "${RED}❌ Deno installation failed${NC}"
      echo "You can install it manually later:"
      echo "  curl -fsSL https://deno.land/install.sh | sh"
      echo ""
      echo "Skyline will use traditional MCP mode (no code execution)."
    fi
  else
    echo ""
    echo -e "${YELLOW}⏭️  Skipping Deno installation${NC}"
    echo "Skyline will use traditional MCP mode (slower, higher costs)."
    echo ""
    echo "To install Deno later and enable code execution:"
    echo "  curl -fsSL https://deno.land/install.sh | sh"
  fi
fi

echo ""

# Check if skyline is already installed
EXISTING_VERSION=""
SKYLINE_PATH=""
if command -v skyline &> /dev/null; then
  SKYLINE_PATH=$(command -v skyline)
  if EXISTING_VERSION=$(skyline --version 2>/dev/null | head -n1); then
    echo ""
    echo "📦 Existing installation found:"
    echo "   Location: $SKYLINE_PATH"
    echo "   Version: $EXISTING_VERSION"
    echo "   Status: Will be replaced"
  fi
fi

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
  
  echo "🔨 Building binary..."
  go build -ldflags="-s -w" -o skyline ./cmd/skyline
  
  # Move to install location
  if [ -w /usr/local/bin ]; then
    mv skyline /usr/local/bin/skyline
    INSTALL_DIR="/usr/local/bin"
    echo ""
    echo "✅ Installed to /usr/local/bin/"
  else
    mkdir -p "$HOME/.local/bin"
    mv skyline "$HOME/.local/bin/skyline"
    INSTALL_DIR="$HOME/.local/bin"
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
  
  # Download skyline binary
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
    INSTALL_DIR="/usr/local/bin"
    echo ""
    echo "✅ Installed to /usr/local/bin/"
  else
    mkdir -p "$HOME/.local/bin"
    mv skyline "$HOME/.local/bin/skyline"
    INSTALL_DIR="$HOME/.local/bin"
    echo ""
    echo "✅ Installed to $HOME/.local/bin/"
    echo "⚠️  Add to PATH: export PATH=\"\$HOME/.local/bin:\$PATH\""
  fi
fi

echo ""
# Check new version
NEW_VERSION=$(skyline --version 2>/dev/null | head -n1 || echo "Unknown version")

if [ -n "$EXISTING_VERSION" ]; then
  echo "✅ Skyline MCP updated successfully!"
  echo ""
  echo "   Old: $EXISTING_VERSION"
  echo "   New: $NEW_VERSION"
else
  echo "🎉 Skyline MCP installed successfully!"
  echo ""
  echo "   Version: $NEW_VERSION"
fi

# Ask about systemd service installation (Linux only)
if [ "$OS" = "linux" ] && command -v systemctl &> /dev/null; then
  echo ""
  echo -e "${BLUE}╔════════════════════════════════════════════════╗${NC}"
  echo -e "${BLUE}║                                                ║${NC}"
  echo -e "${BLUE}║      Systemd Service Installation              ║${NC}"
  echo -e "${BLUE}║                                                ║${NC}"
  echo -e "${BLUE}╚════════════════════════════════════════════════╝${NC}"
  echo ""
  echo -e "${YELLOW}Would you like to install Skyline as a systemd service?${NC}"
  echo "This will allow you to:"
  echo "  • Run Skyline in the background"
  echo "  • Auto-start on boot"
  echo "  • Manage with 'skyline service' commands"
  echo ""
  
  if [ -t 0 ]; then
    # Interactive terminal
    read -p "Install systemd services? (Y/n): " -n 1 -r
    echo ""
  else
    # Non-interactive (curl pipe) - default to yes
    echo "Non-interactive mode detected. Installing systemd services..."
    REPLY="y"
  fi
  
  # Default to yes if user just presses Enter
  if [[ -z $REPLY ]] || [[ $REPLY =~ ^[Yy]$ ]]; then
    echo ""
    echo -e "${BLUE}📦 Installing systemd services...${NC}"
    
    # Create config directory
    mkdir -p ~/.skyline
    echo -e "${GREEN}✓ Created ~/.skyline/${NC}"
    
    # Generate encryption key if not exists
    if [ ! -f ~/.skyline/skyline.env ]; then
      KEY=$(openssl rand -hex 32)
      cat > ~/.skyline/skyline.env << EOF
SKYLINE_PROFILES_KEY=$KEY
CONFIG_SERVER_KEY=$KEY
EOF
      chmod 600 ~/.skyline/skyline.env
      echo -e "${GREEN}✓ Generated encryption key${NC}"
    else
      echo -e "${GREEN}✓ Using existing encryption key${NC}"
    fi
    
    # Create config if not exists
    if [ ! -f ~/.skyline/config.yaml ]; then
      cat > ~/.skyline/config.yaml << 'EOF'
# Skyline MCP Server Configuration
# Manage these settings via Web UI at http://localhost:19190/ui/settings
# or edit this file directly

server:
  # HTTP transport settings
  listen: "localhost:8191"
  # timeout: 30s
  # maxRequestSize: 10MB
  
  # TLS (optional, for production)
  # tls:
  #   enabled: false
  #   cert: /path/to/cert.pem
  #   key: /path/to/key.pem

runtime:
  # Code execution engine (98% cost reduction vs traditional MCP)
  codeExecution:
    enabled: true
    engine: "deno"  # or "node", "bun"
    # denoPath: "/home/user/.deno/bin/deno"  # auto-detect if not set
    timeout: 30s
    memoryLimit: "512MB"
    
  # Discovery cache (for repeated API calls)
  cache:
    enabled: true
    ttl: 1h
    maxSize: 100MB

audit:
  enabled: true
  database: "~/.skyline/skyline-audit.db"
  # rotateAfter: 30d
  # maxSize: 1GB

profiles:
  # API credentials & rate-limiting configurations
  # Managed via Web UI - stores auth tokens, rate limits, custom headers
  storage: "~/.skyline/profiles.enc.yaml"
  encryptionKey: "${SKYLINE_PROFILES_KEY}"  # from skyline.env

# Security
security:
  # Allowed domains for discovery mode (wildcard supported)
  allowedDomains:
    - "*"  # Allow all by default (can restrict in production)
  # cors:
  #   enabled: true
  #   origins: ["http://localhost:*"]
  
# Logging
logging:
  level: "info"  # debug, info, warn, error
  format: "json"  # json or text
  # output: "~/.skyline/skyline.log"
EOF
      echo -e "${GREEN}✓ Created default config.yaml${NC}"
    fi
    
    # No wrapper needed - skyline is a single binary with service commands built-in
    echo -e "${GREEN}✓ Skyline binary installed${NC}"
    
    # Install systemd service file
    mkdir -p ~/.config/systemd/user
    
    # skyline.service (single service with default flags: http + admin)
    cat > ~/.config/systemd/user/skyline.service << EOF
[Unit]
Description=Skyline MCP Server (HTTP + Admin UI)
Documentation=https://skyline.projex.cc/docs
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=${INSTALL_DIR}/skyline --bind=localhost:19190 --storage=%h/.skyline/profiles.enc.yaml --config=%h/.skyline/config.yaml
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal

Environment="PATH=${INSTALL_DIR}:/usr/local/bin:/usr/bin:/bin"
EnvironmentFile=-%h/.skyline/skyline.env

NoNewPrivileges=true
PrivateTmp=true

WorkingDirectory=%h/.skyline

[Install]
WantedBy=default.target
EOF
    
    echo -e "${GREEN}✓ Installed systemd service file${NC}"
    
    # Reload systemd
    systemctl --user daemon-reload
    echo -e "${GREEN}✓ Reloaded systemd${NC}"
    
    echo ""
    echo -e "${BLUE}═══════════════════════════════════════════════${NC}"
    echo -e "${GREEN}✅ Service installation complete!${NC}"
    echo -e "${BLUE}═══════════════════════════════════════════════${NC}"
    echo ""
    
    # Ask if user wants to start services now
    echo -e "${YELLOW}Would you like to start the services now?${NC}"
    
    if [ -t 0 ]; then
      # Interactive terminal
      read -p "Start services? (Y/n): " -n 1 -r
      echo ""
    else
      # Non-interactive (curl pipe) - default to yes
      echo "Non-interactive mode detected. Starting services..."
      REPLY="y"
    fi
    
    # Default to yes if user just presses Enter
    if [[ -z $REPLY ]] || [[ $REPLY =~ ^[Yy]$ ]]; then
      echo ""
      echo -e "${BLUE}🚀 Starting service...${NC}"
      systemctl --user enable --now skyline
      
      sleep 2
      
      echo ""
      echo -e "${GREEN}✅ Service started!${NC}"
      echo ""
      systemctl --user status skyline --no-pager | head -15
      
      echo ""
      echo -e "${BLUE}═══════════════════════════════════════════════${NC}"
      echo -e "${GREEN}📚 Quick Reference${NC}"
      echo -e "${BLUE}═══════════════════════════════════════════════${NC}"
      echo ""
      echo -e "  ${BLUE}systemctl --user status skyline${NC}   - Check service status"
      echo -e "  ${BLUE}systemctl --user restart skyline${NC}  - Restart service"
      echo -e "  ${BLUE}journalctl --user -u skyline -f${NC}   - View logs"
      echo ""
      echo -e "  ${BLUE}Web UI:${NC} http://localhost:19190/ui/"
      echo -e "  ${BLUE}Admin:${NC} http://localhost:19190/admin/"
      echo -e "  ${BLUE}Config:${NC} ~/.skyline/config.yaml"
      echo ""
    else
      echo ""
      echo -e "${YELLOW}⏭️  Service not started${NC}"
      echo ""
      echo "To start it later, run:"
      echo -e "  ${BLUE}systemctl --user start skyline${NC}"
      echo ""
      echo -e "${BLUE}═══════════════════════════════════════════════${NC}"
      echo -e "${GREEN}📚 Quick Reference${NC}"
      echo -e "${BLUE}═══════════════════════════════════════════════${NC}"
      echo ""
      echo -e "  ${BLUE}systemctl --user start skyline${NC}    - Start service"
      echo -e "  ${BLUE}systemctl --user status skyline${NC}   - Check service status"
      echo -e "  ${BLUE}journalctl --user -u skyline -f${NC}   - View logs"
      echo ""
      echo -e "  ${BLUE}Web UI:${NC} http://localhost:19190/ui/"
      echo -e "  ${BLUE}Admin:${NC} http://localhost:19190/admin/"
      echo -e "  ${BLUE}Config:${NC} ~/.skyline/config.yaml"
      echo ""
    fi
  else
    echo ""
    echo -e "${YELLOW}⏭️  Skipping service installation${NC}"
    echo ""
    echo "📝 Next steps:"
    echo "   1. Create config.yaml with your API specs"
    echo "   2. Run: skyline --config=config.yaml"
    echo ""
    echo "📚 Documentation: https://skyline.projex.cc/docs"
    echo "💡 Examples: https://github.com/emadomedher/skyline-mcp/tree/main/examples"
    echo ""
  fi
else
  # macOS or no systemctl - show manual instructions
  echo ""
  echo "📝 Next steps:"
  echo "   1. Create config.yaml with your API specs"
  echo "   2. Run: skyline --config=config.yaml"
  echo ""
  echo "📚 Documentation: https://skyline.projex.cc/docs"
  echo "💡 Examples: https://github.com/emadomedher/skyline-mcp/tree/main/examples"
  echo ""
fi
