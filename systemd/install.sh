#!/bin/bash
set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}╔════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║                                                ║${NC}"
echo -e "${BLUE}║         Skyline Service Installer              ║${NC}"
echo -e "${BLUE}║                                                ║${NC}"
echo -e "${BLUE}╔════════════════════════════════════════════════╗${NC}"
echo ""

# Check if skyline is installed
if ! command -v skyline &> /dev/null; then
    echo -e "${RED}✗ Error: skyline binary not found in PATH${NC}"
    echo ""
    echo "Please install skyline first:"
    echo "  curl -fsSL https://skyline.projex.cc/install | bash"
    echo ""
    exit 1
fi

SKYLINE_BIN=$(which skyline)
SKYLINE_DIR=$(dirname "$SKYLINE_BIN")
echo -e "${GREEN}✓ Found skyline at: $SKYLINE_BIN${NC}"
echo ""

# Ask if user wants to install systemd services
echo -e "${YELLOW}Would you like to install Skyline as a systemd service?${NC}"
echo "This will allow you to:"
echo "  • Run Skyline in the background"
echo "  • Auto-start on boot"
echo "  • Manage with 'skyline service' commands"
echo ""
read -p "Install systemd services? (y/n): " -n 1 -r
echo ""

if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo -e "${YELLOW}⏭️  Skipping service installation${NC}"
    exit 0
fi

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

# Create empty config if not exists
if [ ! -f ~/.skyline/config.yaml ]; then
    cat > ~/.skyline/config.yaml << 'EOF'
# Skyline MCP Configuration
# Add your APIs here or use the Web UI at http://localhost:19190/ui/

apis: []
EOF
    echo -e "${GREEN}✓ Created default config.yaml${NC}"
fi

# Install service wrapper if skyline binary exists
if [ -f "$SKYLINE_BIN" ]; then
    # Backup original binary
    if [ ! -f "$SKYLINE_DIR/skyline-bin" ]; then
        mv "$SKYLINE_BIN" "$SKYLINE_DIR/skyline-bin"
        echo -e "${GREEN}✓ Backed up skyline binary to skyline-bin${NC}"
    fi
    
    # Download and install service wrapper
    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    if [ -f "$SCRIPT_DIR/skyline-service-wrapper.sh" ]; then
        cp "$SCRIPT_DIR/skyline-service-wrapper.sh" "$SKYLINE_BIN"
    else
        # Inline wrapper if separate file doesn't exist
        cat > "$SKYLINE_BIN" << 'WRAPPER_EOF'
#!/bin/bash
REAL_BINARY="$(dirname "$0")/skyline-bin"

if [ "$1" = "service" ]; then
    shift
    COMMAND="$1"
    
    case "$COMMAND" in
        status)
            echo "📊 Skyline Service Status"
            echo ""
            systemctl --user status skyline --no-pager | head -15
            echo ""
            systemctl --user status skyline-server --no-pager | head -15
            ;;
        start)
            echo "🚀 Starting Skyline services..."
            systemctl --user start skyline
            systemctl --user start skyline-server
            sleep 1
            systemctl --user status skyline --no-pager | head -10
            systemctl --user status skyline-server --no-pager | head -10
            ;;
        stop)
            echo "⏹️  Stopping Skyline services..."
            systemctl --user stop skyline
            systemctl --user stop skyline-server
            echo "✓ Services stopped"
            ;;
        restart)
            echo "🔄 Restarting Skyline services..."
            systemctl --user restart skyline
            systemctl --user restart skyline-server
            sleep 1
            systemctl --user status skyline --no-pager | head -10
            systemctl --user status skyline-server --no-pager | head -10
            ;;
        enable)
            echo "🔧 Enabling Skyline services..."
            systemctl --user enable skyline
            systemctl --user enable skyline-server
            echo "✓ Services enabled"
            ;;
        disable)
            echo "🔧 Disabling Skyline services..."
            systemctl --user disable skyline
            systemctl --user disable skyline-server
            echo "✓ Services disabled"
            ;;
        logs)
            SERVICE="${2:-skyline}"
            [ "$SERVICE" = "server" ] && SERVICE="skyline-server"
            echo "📜 Following logs for $SERVICE (Ctrl+C to exit)..."
            journalctl --user -u "$SERVICE" -f
            ;;
        *)
            echo "Usage: skyline service <command>"
            echo ""
            echo "Commands:"
            echo "  status   - Show service status"
            echo "  start    - Start services"
            echo "  stop     - Stop services"
            echo "  restart  - Restart services"
            echo "  enable   - Enable auto-start"
            echo "  disable  - Disable auto-start"
            echo "  logs [server] - Follow logs"
            exit 1
            ;;
    esac
else
    exec "$REAL_BINARY" "$@"
fi
WRAPPER_EOF
    fi
    
    chmod +x "$SKYLINE_BIN"
    echo -e "${GREEN}✓ Installed service management wrapper${NC}"
fi

# Install systemd service files
mkdir -p ~/.config/systemd/user

# skyline.service
cat > ~/.config/systemd/user/skyline.service << EOF
[Unit]
Description=Skyline MCP Server
Documentation=https://skyline.projex.cc/docs
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=$SKYLINE_DIR/skyline-bin --config=%h/.skyline/config.yaml --transport=http --listen=:8191
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal

Environment="PATH=$SKYLINE_DIR:/usr/local/bin:/usr/bin:/bin"

NoNewPrivileges=true
PrivateTmp=true

WorkingDirectory=%h/.skyline

[Install]
WantedBy=default.target
EOF

# skyline-server.service
cat > ~/.config/systemd/user/skyline-server.service << EOF
[Unit]
Description=Skyline Web UI & Profile Server
Documentation=https://skyline.projex.cc/docs
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=$SKYLINE_DIR/skyline-server --listen=localhost:19190 --storage=%h/.skyline/profiles.enc.yaml
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal

Environment="PATH=$SKYLINE_DIR:/usr/local/bin:/usr/bin:/bin"
EnvironmentFile=-%h/.skyline/skyline.env

NoNewPrivileges=true
PrivateTmp=true

WorkingDirectory=%h/.skyline

[Install]
WantedBy=default.target
EOF

echo -e "${GREEN}✓ Installed systemd service files${NC}"

# Reload systemd
systemctl --user daemon-reload
echo -e "${GREEN}✓ Reloaded systemd${NC}"

echo ""
echo -e "${BLUE}═══════════════════════════════════════════════${NC}"
echo -e "${GREEN}✅ Installation complete!${NC}"
echo -e "${BLUE}═══════════════════════════════════════════════${NC}"
echo ""

# Ask if user wants to start services now
echo -e "${YELLOW}Would you like to start the services now?${NC}"
read -p "Start services? (y/n): " -n 1 -r
echo ""

if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo ""
    echo -e "${BLUE}🚀 Starting services...${NC}"
    systemctl --user enable --now skyline
    systemctl --user enable --now skyline-server
    
    sleep 2
    
    echo ""
    echo -e "${GREEN}✅ Services started!${NC}"
    echo ""
    skyline service status
else
    echo ""
    echo -e "${YELLOW}⏭️  Services not started${NC}"
    echo ""
    echo "To start them later, run:"
    echo -e "  ${BLUE}skyline service start${NC}"
fi

echo ""
echo -e "${BLUE}═══════════════════════════════════════════════${NC}"
echo -e "${GREEN}📚 Quick Reference${NC}"
echo -e "${BLUE}═══════════════════════════════════════════════${NC}"
echo ""
echo -e "  ${BLUE}skyline service status${NC}   - Check service status"
echo -e "  ${BLUE}skyline service start${NC}    - Start services"
echo -e "  ${BLUE}skyline service stop${NC}     - Stop services"
echo -e "  ${BLUE}skyline service restart${NC}  - Restart services"
echo -e "  ${BLUE}skyline service logs${NC}     - View logs"
echo ""
echo -e "  ${BLUE}Web UI:${NC} http://localhost:19190/ui/"
echo -e "  ${BLUE}Config:${NC} ~/.skyline/config.yaml"
echo ""
