#!/bin/bash
# Skyline Server Startup Script
# Uses a fixed encryption key so profiles persist across restarts

cd "$(dirname "$0")"

# Use a fixed key for this installation
# In production, generate once and store securely
export CONFIG_SERVER_KEY="base64:PUDZs5yRMx+9TS1HW4ud2p+ZZKcZEPGtcYPVT3Y+8r0="

# Start server
./bin/skyline --listen :9190 --auth-mode none "$@"
