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
  mingw*|msys*|cygwin*) OS="windows" ;;
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
  
  EXT=""
  if [ "$OS" = "windows" ]; then EXT=".exe"; fi
  BINARY="skyline-${OS}-${ARCH}${EXT}"
  URL="https://github.com/emadomedher/skyline-mcp/releases/latest/download/${BINARY}"

  echo "📥 Downloading from GitHub releases..."

  # Download skyline binary
  LOCAL_NAME="skyline${EXT}"
  if command -v curl &> /dev/null; then
    if ! curl -fsSL "$URL" -o "$LOCAL_NAME"; then
      echo "❌ Download failed. Check release exists for ${OS}-${ARCH}"
      exit 1
    fi
  elif command -v wget &> /dev/null; then
    if ! wget -q "$URL" -O "$LOCAL_NAME"; then
      echo "❌ Download failed. Check release exists for ${OS}-${ARCH}"
      exit 1
    fi
  else
    echo "❌ curl or wget required"
    exit 1
  fi

  chmod +x "$LOCAL_NAME"

  # Move to install location
  if [ "$OS" = "windows" ]; then
    # Windows (Git Bash / MSYS2) — install to user's local bin
    INSTALL_DIR="$HOME/.local/bin"
    mkdir -p "$INSTALL_DIR"
    mv "$LOCAL_NAME" "$INSTALL_DIR/skyline.exe"
    echo ""
    echo "✅ Installed to $INSTALL_DIR/skyline.exe"
    if ! echo "$PATH" | grep -q "$INSTALL_DIR"; then
      echo "⚠️  Add to PATH: export PATH=\"\$HOME/.local/bin:\$PATH\""
    fi
  elif [ -w /usr/local/bin ]; then
    mv "$LOCAL_NAME" /usr/local/bin/skyline
    INSTALL_DIR="/usr/local/bin"
    echo ""
    echo "✅ Installed to /usr/local/bin/"
  else
    mkdir -p "$HOME/.local/bin"
    mv "$LOCAL_NAME" "$HOME/.local/bin/skyline"
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
    
    # Validate/initialize encryption key and profiles
    echo ""
    echo -e "${BLUE}🔐 Checking encryption setup...${NC}"
    
    # Helper function: Ensure key is in systemd environment
    ensure_key_in_systemd_env() {
      local KEY="$1"
      mkdir -p ~/.config/environment.d
      echo "SKYLINE_PROFILES_KEY=$KEY" > ~/.config/environment.d/skyline.conf
      chmod 600 ~/.config/environment.d/skyline.conf
      # Also set for current systemd user manager
      systemctl --user set-environment SKYLINE_PROFILES_KEY="$KEY" 2>/dev/null || true
    }
    
    # Helper function: Ensure key is in shell profile (if file exists)
    ensure_key_in_shell_profile() {
      local KEY="$1"
      
      # Detect shell profile
      if [ -n "$ZSH_VERSION" ] || [ "$SHELL" = "$(which zsh 2>/dev/null)" ]; then
        PROFILE_FILE="$HOME/.zshrc"
      elif [ -n "$BASH_VERSION" ] || [ "$SHELL" = "$(which bash 2>/dev/null)" ]; then
        PROFILE_FILE="$HOME/.bashrc"
      else
        PROFILE_FILE="$HOME/.profile"
      fi
      
      # Only write if profile file already exists (don't create it)
      if [ -f "$PROFILE_FILE" ]; then
        if grep -q "SKYLINE_PROFILES_KEY" "$PROFILE_FILE" 2>/dev/null; then
          echo -e "${GREEN}✓ Key already in $PROFILE_FILE${NC}"
        else
          echo "" >> "$PROFILE_FILE"
          echo "# Skyline MCP encryption key (added $(date +%Y-%m-%d))" >> "$PROFILE_FILE"
          echo "export SKYLINE_PROFILES_KEY=\"$KEY\"" >> "$PROFILE_FILE"
          echo -e "${GREEN}✓ Added key to $PROFILE_FILE${NC}"
        fi
      fi
    }
    
    # Run validation
    $INSTALL_DIR/skyline --validate 2>/dev/null
    VALIDATE_EXIT=$?
    
    case $VALIDATE_EXIT in
      0)
        # Case 1: File exists + Key valid
        echo -e "${GREEN}✓ Encryption setup valid${NC}"
        # Ensure key is persisted in both locations
        if [ -n "$SKYLINE_PROFILES_KEY" ]; then
          ensure_key_in_systemd_env "$SKYLINE_PROFILES_KEY"
          ensure_key_in_shell_profile "$SKYLINE_PROFILES_KEY"
        fi
        ;;
      
      1)
        # Case 3 or 4: File not found
        if [ -n "$SKYLINE_PROFILES_KEY" ]; then
          # Case 3: Key exists but no file
          echo -e "${YELLOW}⚠️  Encryption key found, but profiles file missing${NC}"
          echo -e "${BLUE}Creating encrypted profiles file...${NC}"
          $INSTALL_DIR/skyline --init-profiles
          if [ $? -eq 0 ]; then
            echo -e "${GREEN}✓ Created encrypted profiles file${NC}"
            ensure_key_in_systemd_env "$SKYLINE_PROFILES_KEY"
            ensure_key_in_shell_profile "$SKYLINE_PROFILES_KEY"
          else
            echo -e "${RED}❌ Failed to create profiles file${NC}"
            exit 1
          fi
        else
          # Case 4: Neither exists (fresh install)
          echo -e "${BLUE}Generating new encryption key...${NC}"
          KEY=$(openssl rand -hex 32)
          export SKYLINE_PROFILES_KEY="$KEY"
          
          # Create encrypted profiles file
          $INSTALL_DIR/skyline --init-profiles
          if [ $? -ne 0 ]; then
            echo -e "${RED}❌ Failed to create profiles file${NC}"
            exit 1
          fi
          
          # Persist key in both locations
          ensure_key_in_systemd_env "$KEY"
          ensure_key_in_shell_profile "$KEY"
          
          # Display key to user
          echo ""
          echo -e "${BLUE}════════════════════════════════════════════════════════${NC}"
          echo -e "${GREEN}🔑 ENCRYPTION KEY GENERATED${NC}"
          echo -e "${BLUE}════════════════════════════════════════════════════════${NC}"
          echo ""
          echo "  $KEY"
          echo ""
          echo -e "${YELLOW}⚠️  SAVE THIS KEY SECURELY!${NC}"
          echo ""
          echo "This key encrypts your API credentials in:"
          echo "  ~/.skyline/profiles.enc.yaml"
          echo ""
          echo "It has been automatically saved to:"
          echo "  ~/.config/environment.d/skyline.conf (for systemd)"
          if [ -f "$PROFILE_FILE" ]; then
            echo "  $PROFILE_FILE (for interactive shells)"
          fi
          echo ""
          echo "Without this key, your encrypted profiles cannot be decrypted!"
          echo ""
          echo -e "${BLUE}════════════════════════════════════════════════════════${NC}"
          echo ""
        fi
        ;;
      
      2|3)
        # Case 2: File exists but key missing/invalid
        echo -e "${RED}❌ Encrypted profiles file exists but key is missing or invalid${NC}"
        echo ""
        echo "You have an encrypted profiles file at:"
        echo "  ~/.skyline/profiles.enc.yaml"
        echo ""
        echo "But SKYLINE_PROFILES_KEY is not set or cannot decrypt the file."
        echo ""
        echo "To fix this:"
        echo "  1. If you have the key, set it:"
        echo "     export SKYLINE_PROFILES_KEY=<your-key>"
        echo "     Then re-run this installer"
        echo ""
        echo "  2. If you lost the key, delete the file and start fresh:"
        echo "     rm ~/.skyline/profiles.enc.yaml"
        echo "     Then re-run this installer"
        echo ""
        echo "Service will be installed but will NOT start until this is fixed."
        echo ""
        # Continue installation but don't start service
        SKIP_SERVICE_START=true
        ;;
    esac
    
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
    engine: "goja"  # Embedded JS runtime (zero external dependencies)
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
    
    # Ask if user wants to start services now (unless blocked by validation failure)
    if [ "$SKIP_SERVICE_START" = "true" ]; then
      # Case 2: Validation failed - don't ask, just skip
      echo ""
      echo -e "${YELLOW}⏭️  Service installed but NOT started (encryption key issue)${NC}"
      echo ""
      echo "Fix the encryption key issue above, then start the service:"
      echo -e "  ${BLUE}systemctl --user start skyline${NC}"
      echo ""
    else
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
      fi # End of SKIP_SERVICE_START check
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
  # macOS, Windows, or no systemctl - show manual instructions
  echo ""
  echo "📝 Next steps:"
  if [ "$OS" = "windows" ]; then
    echo "   1. Run: skyline.exe"
    echo "   2. Open Admin UI at https://localhost:8191/ui"
  else
    echo "   1. Run: skyline"
    echo "   2. Open Admin UI at https://localhost:8191/ui"
  fi
  echo ""
  echo "📚 Documentation: https://skyline.projex.cc/docs"
  echo ""
fi
