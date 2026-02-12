#!/bin/bash
# Skyline service management wrapper
# This script wraps the skyline binary and adds service management commands

REAL_BINARY="$(dirname "$0")/skyline-bin"

# If first argument is "service", handle service management
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
            echo "🔧 Enabling Skyline services (auto-start on boot)..."
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
            if [ "$SERVICE" = "server" ] || [ "$SERVICE" = "skyline-server" ]; then
                SERVICE="skyline-server"
            else
                SERVICE="skyline"
            fi
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
            echo "  enable   - Enable auto-start on boot"
            echo "  disable  - Disable auto-start"
            echo "  logs [server]  - Follow logs (default: skyline)"
            echo ""
            echo "Examples:"
            echo "  skyline service status"
            echo "  skyline service restart"
            echo "  skyline service logs"
            echo "  skyline service logs server"
            exit 1
            ;;
    esac
else
    # Pass through to real binary
    exec "$REAL_BINARY" "$@"
fi
