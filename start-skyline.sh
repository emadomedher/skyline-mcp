#!/bin/bash
# Skyline Server Startup Script
# Uses a fixed encryption key so profiles persist across restarts

cd "$(dirname "$0")"

# Use a fixed key for this installation
# In production, generate once and store securely
export CONFIG_SERVER_KEY="base64:REDACTED_ENCRYPTION_KEY"

# Start server
./bin/skyline-server --listen :9190 --auth-mode none "$@"
