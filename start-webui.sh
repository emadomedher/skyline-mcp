#!/bin/bash
# Start Skyline Web UI with encryption key

cd ~/code/skyline-mcp

# Check if encryption key exists
if [ ! -f .encryption-key ]; then
    echo "❌ Error: .encryption-key not found"
    exit 1
fi

# Export encryption key
export CONFIG_SERVER_KEY=$(cat .encryption-key)

# Kill any existing skyline-server processes
pkill -f "skyline-server --listen"

# Start server
echo "🚀 Starting Skyline Web UI on port 9190..."
nohup ./bin/skyline-server --listen 0.0.0.0:9190 --auth-mode none > ~/skyline-server.log 2>&1 &

sleep 2

# Check if it started
if netstat -tlnp 2>/dev/null | grep -q ":9190"; then
    PID=$(pgrep -f "skyline-server --listen")
    echo "✅ Skyline Web UI started (PID: $PID)"
    echo "📍 Access at: http://10.135.198.128:9190/ui/"
else
    echo "❌ Failed to start - check ~/skyline-server.log"
    tail -10 ~/skyline-server.log
fi
