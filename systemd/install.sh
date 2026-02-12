#!/bin/bash
set -e

echo "🚀 Installing Skyline systemd services..."

# Create config directory
mkdir -p ~/.skyline
echo "✓ Created ~/.skyline/"

# Copy service files
mkdir -p ~/.config/systemd/user
cp skyline.service ~/.config/systemd/user/
cp skyline-server.service ~/.config/systemd/user/
echo "✓ Installed service files"

# Generate encryption key if not exists
if [ ! -f ~/.skyline/skyline.env ]; then
    KEY=$(openssl rand -hex 32)
    cat > ~/.skyline/skyline.env << EOF
SKYLINE_PROFILES_KEY=$KEY
EOF
    chmod 600 ~/.skyline/skyline.env
    echo "✓ Generated encryption key"
else
    echo "✓ Using existing encryption key"
fi

# Create empty config if not exists
if [ ! -f ~/.skyline/config.yaml ]; then
    cat > ~/.skyline/config.yaml << 'EOF'
# Skyline MCP Configuration
# Add your APIs here or use the Web UI at http://localhost:19190/ui/

apis: []
EOF
    echo "✓ Created default config.yaml"
fi

# Reload systemd
systemctl --user daemon-reload
echo "✓ Reloaded systemd"

echo ""
echo "✅ Installation complete!"
echo ""
echo "📝 Next steps:"
echo ""
echo "1. Enable and start skyline-server (Web UI):"
echo "   systemctl --user enable --now skyline-server"
echo "   Open: http://localhost:19190/ui/"
echo ""
echo "2. Enable and start skyline (MCP server):"
echo "   systemctl --user enable --now skyline"
echo ""
echo "3. Check status:"
echo "   systemctl --user status skyline"
echo "   systemctl --user status skyline-server"
echo ""
echo "4. View logs:"
echo "   journalctl --user -u skyline -f"
echo "   journalctl --user -u skyline-server -f"
echo ""
