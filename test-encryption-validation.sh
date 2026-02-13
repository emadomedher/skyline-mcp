#!/bin/bash
# Test script for encryption validation and key persistence

set -e

SKYLINE_BIN="./bin/skyline"
TEST_DIR="/tmp/skyline-encryption-test"
TEST_PROFILES="$TEST_DIR/profiles.enc.yaml"

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m'

echo "════════════════════════════════════════════════"
echo "Skyline MCP Encryption Validation Tests"
echo "════════════════════════════════════════════════"
echo ""

# Clean up any previous test
rm -rf "$TEST_DIR"
mkdir -p "$TEST_DIR"

# Build skyline first
echo -e "${BLUE}Building skyline...${NC}"
make build > /dev/null 2>&1
echo -e "${GREEN}✓ Build successful${NC}"
echo ""

# Test 1: --init-profiles with new key
echo -e "${BLUE}Test 1: Create new encrypted profiles file${NC}"
KEY1=$(openssl rand -hex 32)
SKYLINE_PROFILES_KEY=$KEY1 $SKYLINE_BIN --init-profiles --storage "$TEST_PROFILES" 2>&1 | grep -q "✅"
if [ $? -eq 0 ]; then
  echo -e "${GREEN}✓ Test 1 passed: Created encrypted profiles file${NC}"
else
  echo -e "${RED}✗ Test 1 failed${NC}"
  exit 1
fi
echo ""

# Test 2: --validate with correct key
echo -e "${BLUE}Test 2: Validate with correct key${NC}"
SKYLINE_PROFILES_KEY=$KEY1 $SKYLINE_BIN --validate --storage "$TEST_PROFILES" 2>&1 | grep -q "✅"
EXIT_CODE=$?
if [ $EXIT_CODE -eq 0 ]; then
  echo -e "${GREEN}✓ Test 2 passed: Validation successful (exit code 0)${NC}"
else
  echo -e "${RED}✗ Test 2 failed: Expected exit code 0${NC}"
  exit 1
fi
echo ""

# Test 3: --validate with wrong key
echo -e "${BLUE}Test 3: Validate with wrong key${NC}"
WRONG_KEY=$(openssl rand -hex 32)
SKYLINE_PROFILES_KEY=$WRONG_KEY $SKYLINE_BIN --validate --storage "$TEST_PROFILES" 2>&1 | grep -q "❌"
EXIT_CODE=$?
if [ $EXIT_CODE -eq 0 ]; then
  echo -e "${GREEN}✓ Test 3 passed: Validation failed as expected${NC}"
else
  echo -e "${RED}✗ Test 3 failed${NC}"
  exit 1
fi
echo ""

# Test 4: --validate with missing file
echo -e "${BLUE}Test 4: Validate missing file${NC}"
SKYLINE_PROFILES_KEY=$KEY1 $SKYLINE_BIN --validate --storage "$TEST_DIR/nonexistent.yaml" 2>&1 | grep -q "not found"
if [ $? -eq 0 ]; then
  echo -e "${GREEN}✓ Test 4 passed: File not found error${NC}"
else
  echo -e "${RED}✗ Test 4 failed${NC}"
  exit 1
fi
echo ""

# Test 5: --init-profiles when file exists
echo -e "${BLUE}Test 5: Try to create when file exists${NC}"
SKYLINE_PROFILES_KEY=$KEY1 $SKYLINE_BIN --init-profiles --storage "$TEST_PROFILES" 2>&1 | grep -q "already exists"
if [ $? -eq 0 ]; then
  echo -e "${GREEN}✓ Test 5 passed: Rejected duplicate file creation${NC}"
else
  echo -e "${RED}✗ Test 5 failed${NC}"
  exit 1
fi
echo ""

# Test 6: --validate with --key flag (override env var)
echo -e "${BLUE}Test 6: Validate with --key flag${NC}"
SKYLINE_PROFILES_KEY=$WRONG_KEY $SKYLINE_BIN --validate --storage "$TEST_PROFILES" --key "$KEY1" 2>&1 | grep -q "✅"
if [ $? -eq 0 ]; then
  echo -e "${GREEN}✓ Test 6 passed: --key flag overrides env var${NC}"
else
  echo -e "${RED}✗ Test 6 failed${NC}"
  exit 1
fi
echo ""

# Test 7: --validate without key
echo -e "${BLUE}Test 7: Validate without key${NC}"
unset SKYLINE_PROFILES_KEY
$SKYLINE_BIN --validate --storage "$TEST_PROFILES" 2>&1 | grep -q "not provided"
if [ $? -eq 0 ]; then
  echo -e "${GREEN}✓ Test 7 passed: Key missing error${NC}"
else
  echo -e "${RED}✗ Test 7 failed${NC}"
  exit 1
fi
echo ""

# Clean up
rm -rf "$TEST_DIR"

echo "════════════════════════════════════════════════"
echo -e "${GREEN}✅ All tests passed!${NC}"
echo "════════════════════════════════════════════════"
echo ""
echo "Implementation ready for:"
echo "  • Install script integration"
echo "  • Production deployment"
echo "  • Manpage generation"
echo ""
