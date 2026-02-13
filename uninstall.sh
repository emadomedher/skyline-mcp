#!/bin/bash

# Skyline MCP Uninstaller
# Usage: curl -fsSL https://skyline.projex.cc/uninstall | bash

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo "🗑️  Skyline MCP Uninstaller"
echo ""

# Check if skyline is installed
if ! command -v skyline &> /dev/null; then
  echo -e "${YELLOW}⚠️  Skyline not found in PATH${NC}"
  echo ""
  echo "Skyline doesn't appear to be installed, or it's not in your PATH."
  echo "Common install locations:"
  echo "  • /usr/local/bin/skyline"
  echo "  • ~/.local/bin/skyline"
  echo ""
  
  if [ -t 0 ]; then
    # Interactive terminal
    read -p "Continue with cleanup anyway? (y/N): " -n 1 -r
    echo ""
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
      echo "Cancelled."
      exit 0
    fi
  elif [ "$CONFIRM_UNINSTALL" != "yes" ]; then
    # Non-interactive without confirmation
    echo "Skyline not found and CONFIRM_UNINSTALL not set. Exiting."
    exit 1
  fi
else
  SKYLINE_PATH=$(command -v skyline)
  SKYLINE_VERSION=$(skyline --version 2>/dev/null | head -n1 || echo "Unknown version")
  echo "📦 Found Skyline installation:"
  echo "   Location: $SKYLINE_PATH"
  echo "   Version: $SKYLINE_VERSION"
  echo ""
fi

# Confirm uninstallation
echo -e "${YELLOW}⚠️  This will remove:${NC}"
echo "  • Skyline binary"
echo "  • Systemd service (if installed)"
echo "  • Service configuration files"
echo ""
echo -e "${BLUE}Your data will be preserved:${NC}"
echo "  • ~/.skyline/config.yaml (API configurations)"
echo "  • ~/.skyline/profiles.enc.yaml (encrypted profiles)"
echo "  • ~/.skyline/skyline.env (encryption keys)"
echo ""

if [ -t 0 ]; then
  # Interactive terminal
  read -p "Proceed with uninstallation? (y/N): " -n 1 -r
  echo ""
  if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "Cancelled."
    exit 0
  fi
else
  # Non-interactive mode - check for confirmation
  if [ "$CONFIRM_UNINSTALL" = "yes" ]; then
    echo -e "${YELLOW}⚠️  Non-interactive mode: CONFIRM_UNINSTALL=yes detected${NC}"
    echo "Proceeding with uninstallation..."
    echo ""
  else
    echo -e "${RED}❌ Non-interactive mode requires confirmation${NC}"
    echo ""
    echo "To uninstall via curl pipe, use:"
    echo -e "  ${BLUE}curl -fsSL https://skyline.projex.cc/uninstall | CONFIRM_UNINSTALL=yes bash${NC}"
    echo ""
    echo "Or download and run interactively:"
    echo "  curl -fsSL https://skyline.projex.cc/uninstall -o uninstall.sh"
    echo "  chmod +x uninstall.sh"
    echo "  ./uninstall.sh"
    exit 1
  fi
fi

echo ""
echo -e "${BLUE}🗑️  Uninstalling Skyline...${NC}"
echo ""

# Stop and disable systemd service (Linux only)
if [ "$(uname -s)" = "Linux" ] && command -v systemctl &> /dev/null; then
  if systemctl --user is-active --quiet skyline 2>/dev/null; then
    echo "⏹️  Stopping skyline service..."
    systemctl --user stop skyline 2>/dev/null || true
  fi
  
  if systemctl --user is-enabled --quiet skyline 2>/dev/null; then
    echo "🔧 Disabling skyline service..."
    systemctl --user disable skyline 2>/dev/null || true
  fi
  
  # Remove service file
  if [ -f ~/.config/systemd/user/skyline.service ]; then
    echo "🗑️  Removing skyline.service..."
    rm -f ~/.config/systemd/user/skyline.service
  fi
  
  # Reload systemd
  systemctl --user daemon-reload 2>/dev/null || true
  echo -e "${GREEN}✓ Systemd service removed${NC}"
  echo ""
fi

# Remove binary
REMOVED_COUNT=0

for LOCATION in /usr/local/bin ~/.local/bin; do
  FULL_PATH="$LOCATION/skyline"
  if [ -f "$FULL_PATH" ]; then
    echo "🗑️  Removing $FULL_PATH..."
    if [ -w "$FULL_PATH" ]; then
      rm -f "$FULL_PATH"
      REMOVED_COUNT=$((REMOVED_COUNT + 1))
    else
      echo -e "${YELLOW}⚠️  No write permission. Trying with sudo...${NC}"
      if sudo rm -f "$FULL_PATH"; then
        REMOVED_COUNT=$((REMOVED_COUNT + 1))
      else
        echo -e "${RED}❌ Failed to remove $FULL_PATH${NC}"
      fi
    fi
  fi
done

if [ $REMOVED_COUNT -eq 0 ]; then
  echo -e "${YELLOW}⚠️  No binary found to remove${NC}"
else
  echo -e "${GREEN}✓ Removed skyline binary${NC}"
fi

echo ""
echo -e "${GREEN}✅ Skyline MCP uninstalled successfully!${NC}"
echo ""
echo -e "${BLUE}Your data is preserved at:${NC}"
echo "  ~/.skyline/"
echo ""
echo "To completely remove all data:"
echo -e "  ${RED}rm -rf ~/.skyline/${NC}"
echo ""
echo "To reinstall Skyline later:"
echo "  curl -fsSL https://skyline.projex.cc/install | bash"
echo ""
