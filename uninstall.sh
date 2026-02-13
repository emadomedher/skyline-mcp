#!/bin/bash
set -e

# Skyline MCP Uninstaller
# Usage: curl -fsSL https://skyline.projex.cc/uninstall | bash

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}🧹 Uninstalling Skyline MCP...${NC}"
echo ""

# Stop and disable services
if systemctl --user is-active skyline &>/dev/null || systemctl --user is-active skyline-server &>/dev/null; then
  echo -e "${BLUE}Stopping services...${NC}"
  systemctl --user stop skyline skyline-server 2>/dev/null || true
  systemctl --user disable skyline skyline-server 2>/dev/null || true
  echo -e "${GREEN}✓ Services stopped${NC}"
fi

# Remove systemd service files
if [ -f "$HOME/.config/systemd/user/skyline.service" ] || [ -f "$HOME/.config/systemd/user/skyline-server.service" ]; then
  echo -e "${BLUE}Removing service files...${NC}"
  rm -f "$HOME/.config/systemd/user/skyline.service"
  rm -f "$HOME/.config/systemd/user/skyline-server.service"
  systemctl --user daemon-reload
  echo -e "${GREEN}✓ Service files removed${NC}"
fi

# Remove binaries
REMOVED_COUNT=0
BINS=()
[ -f "/usr/local/bin/skyline" ] && BINS+=("/usr/local/bin/skyline")
[ -f "/usr/local/bin/skyline-server" ] && BINS+=("/usr/local/bin/skyline-server")
[ -f "/usr/local/bin/skyline-bin" ] && BINS+=("/usr/local/bin/skyline-bin")
[ -f "$HOME/.local/bin/skyline" ] && BINS+=("$HOME/.local/bin/skyline")
[ -f "$HOME/.local/bin/skyline-server" ] && BINS+=("$HOME/.local/bin/skyline-server")
[ -f "$HOME/.local/bin/skyline-bin" ] && BINS+=("$HOME/.local/bin/skyline-bin")

if [ ${#BINS[@]} -gt 0 ]; then
  echo -e "${BLUE}Removing binaries...${NC}"
  for bin in "${BINS[@]}"; do
    # Check if we need sudo
    if [[ $bin == /usr/local/bin/* ]]; then
      if [ -w /usr/local/bin ]; then
        rm -f "$bin" && echo -e "  ${GREEN}✓${NC} Removed $bin" && ((REMOVED_COUNT++))
      else
        sudo rm -f "$bin" && echo -e "  ${GREEN}✓${NC} Removed $bin" && ((REMOVED_COUNT++))
      fi
    else
      rm -f "$bin" && echo -e "  ${GREEN}✓${NC} Removed $bin" && ((REMOVED_COUNT++))
    fi
  done
fi

# Remove config directory (ask first)
if [ -d "$HOME/.skyline" ]; then
  echo ""
  echo -e "${YELLOW}Config directory found at: ~/.skyline/${NC}"
  
  # Check if we're in interactive mode
  if [ -t 0 ]; then
    read -p "Remove config directory? (y/N): " -n 1 -r
    echo ""
  else
    echo "Non-interactive mode: keeping config directory"
    REPLY="n"
  fi
  
  if [[ $REPLY =~ ^[Yy]$ ]]; then
    rm -rf "$HOME/.skyline"
    echo -e "${GREEN}✓ Config directory removed${NC}"
  else
    echo -e "${YELLOW}⏭️  Keeping config directory${NC}"
    echo "   Remove manually with: rm -rf ~/.skyline/"
  fi
fi

# Verify removal
echo ""
if command -v skyline &> /dev/null; then
  REMAINING_PATH=$(command -v skyline)
  echo -e "${YELLOW}⚠️  Skyline still found at: $REMAINING_PATH${NC}"
  echo "   You may need to remove it manually"
else
  echo -e "${GREEN}✅ Skyline MCP uninstalled successfully!${NC}"
fi

echo ""
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${BLUE}Removed: $REMOVED_COUNT file(s)${NC}"
echo ""
echo "To reinstall:"
echo -e "  ${BLUE}curl -fsSL https://skyline.projex.cc/install | bash${NC}"
echo ""
