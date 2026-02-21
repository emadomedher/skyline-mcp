#!/bin/bash
# Skyline MCP - CI/CD Test Script
# Tests installation, STDIO mode, HTTP mode, and MCP protocol compliance

set -e  # Exit on error

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Test counters
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

# Cleanup function
cleanup() {
    echo ""
    echo "🧹 Cleaning up test artifacts..."
    rm -f /tmp/skyline-test-*
    if [ -n "$TEST_SERVER_PID" ]; then
        kill $TEST_SERVER_PID 2>/dev/null || true
    fi
}

trap cleanup EXIT

# Test helper functions
test_start() {
    TESTS_RUN=$((TESTS_RUN + 1))
    echo ""
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${BLUE}TEST $TESTS_RUN: $1${NC}"
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
}

test_pass() {
    TESTS_PASSED=$((TESTS_PASSED + 1))
    echo -e "${GREEN}✓ PASS${NC}: $1"
}

test_fail() {
    TESTS_FAILED=$((TESTS_FAILED + 1))
    echo -e "${RED}✗ FAIL${NC}: $1"
    if [ -n "$2" ]; then
        echo -e "${RED}  Error: $2${NC}"
    fi
}

# Check if binary exists
if [ ! -f "bin/skyline" ]; then
    echo -e "${RED}❌ Binary not found: bin/skyline${NC}"
    echo "Run: make build"
    exit 1
fi

SKYLINE_BIN="$(pwd)/bin/skyline"

echo "═══════════════════════════════════════════════════════════════"
echo "  🧪 Skyline MCP - CI/CD Test Suite"
echo "═══════════════════════════════════════════════════════════════"
echo ""
echo "Binary: $SKYLINE_BIN"
echo "Version: $($SKYLINE_BIN --version | head -1)"
echo "Platform: $(uname -s)/$(uname -m)"
echo ""

# ============================================================================
# TEST 1: Binary Execution
# ============================================================================
test_start "Binary Execution"

if $SKYLINE_BIN --version > /dev/null 2>&1; then
    test_pass "Binary executes successfully"
else
    test_fail "Binary execution failed"
fi

VERSION_OUTPUT=$($SKYLINE_BIN --version)
if echo "$VERSION_OUTPUT" | grep -q "Skyline MCP"; then
    test_pass "Version output correct"
else
    test_fail "Version output incorrect" "$VERSION_OUTPUT"
fi

# ============================================================================
# TEST 2: STDIO Mode - Basic Protocol
# ============================================================================
test_start "STDIO Mode - Basic MCP Protocol"

# Create test config
cat > /tmp/skyline-test-config.yaml << 'EOF'
apis:
  - name: petstore
    spec_url: https://petstore3.swagger.io/api/v3/openapi.json
    base_url_override: https://petstore3.swagger.io/api/v3
EOF

# Test initialize
INIT_RESPONSE=$(echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' | \
    timeout 10 $SKYLINE_BIN --transport stdio --config /tmp/skyline-test-config.yaml 2>/dev/null || echo "ERROR")

if echo "$INIT_RESPONSE" | jq -e '.result.protocolVersion' > /dev/null 2>&1; then
    test_pass "STDIO initialize successful"
else
    test_fail "STDIO initialize failed" "$INIT_RESPONSE"
fi

PROTOCOL_VERSION=$(echo "$INIT_RESPONSE" | jq -r '.result.protocolVersion')
if [ "$PROTOCOL_VERSION" = "2025-11-25" ]; then
    test_pass "Protocol version correct: $PROTOCOL_VERSION"
else
    test_fail "Protocol version incorrect" "Expected: 2025-11-25, Got: $PROTOCOL_VERSION"
fi

# ============================================================================
# TEST 3: STDIO Mode - Tools List
# ============================================================================
test_start "STDIO Mode - Tools List"

TOOLS_RESPONSE=$(( \
    echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}'; \
    echo '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' \
) | timeout 10 $SKYLINE_BIN --transport stdio --config /tmp/skyline-test-config.yaml 2>/dev/null | tail -1)

if echo "$TOOLS_RESPONSE" | jq -e '.result.tools' > /dev/null 2>&1; then
    test_pass "Tools list returned successfully"
else
    test_fail "Tools list failed" "$TOOLS_RESPONSE"
fi

TOOL_COUNT=$(echo "$TOOLS_RESPONSE" | jq -r '.result.tools | length')
if [ "$TOOL_COUNT" -gt 0 ]; then
    test_pass "Tools list contains $TOOL_COUNT tools"
else
    test_fail "Tools list empty"
fi

FIRST_TOOL=$(echo "$TOOLS_RESPONSE" | jq -r '.result.tools[0].name')
if [ -n "$FIRST_TOOL" ] && [ "$FIRST_TOOL" != "null" ]; then
    test_pass "First tool name: $FIRST_TOOL"
else
    test_fail "First tool has no name"
fi

# ============================================================================
# TEST 4: STDIO Mode - Resources List
# ============================================================================
test_start "STDIO Mode - Resources List"

RESOURCES_RESPONSE=$(( \
    echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}'; \
    echo '{"jsonrpc":"2.0","id":2,"method":"resources/list","params":{}}' \
) | timeout 10 $SKYLINE_BIN --transport stdio --config /tmp/skyline-test-config.yaml 2>/dev/null | tail -1)

if echo "$RESOURCES_RESPONSE" | jq -e '.result.resources' > /dev/null 2>&1; then
    test_pass "Resources list returned successfully"
else
    test_fail "Resources list failed" "$RESOURCES_RESPONSE"
fi

# ============================================================================
# TEST 5: STDIO Mode - Invalid Method
# ============================================================================
test_start "STDIO Mode - Error Handling"

ERROR_RESPONSE=$(( \
    echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}'; \
    echo '{"jsonrpc":"2.0","id":2,"method":"invalid/method","params":{}}' \
) | timeout 10 $SKYLINE_BIN --transport stdio --config /tmp/skyline-test-config.yaml 2>/dev/null | tail -1)

if echo "$ERROR_RESPONSE" | jq -e '.error.code' > /dev/null 2>&1; then
    test_pass "Invalid method returns error"
else
    test_fail "Invalid method did not return error"
fi

ERROR_CODE=$(echo "$ERROR_RESPONSE" | jq -r '.error.code')
if [ "$ERROR_CODE" = "-32601" ]; then
    test_pass "Error code correct: -32601 (method not found)"
else
    test_fail "Error code incorrect" "Expected: -32601, Got: $ERROR_CODE"
fi

# ============================================================================
# TEST 6: STDIO Mode - Missing Config
# ============================================================================
test_start "STDIO Mode - Missing Config File"

ERROR_OUTPUT=$(echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' | \
    $SKYLINE_BIN --transport stdio --config /tmp/nonexistent.yaml 2>&1 || true)

if echo "$ERROR_OUTPUT" | grep -qi "load config\|no such file"; then
    test_pass "Missing config file handled gracefully"
else
    test_fail "Missing config file error unclear"
fi

# ============================================================================
# TEST 7: STDIO Mode - No Config Flag
# ============================================================================
test_start "STDIO Mode - No Config Flag Required"

NO_CONFIG_OUTPUT=$(echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' | \
    $SKYLINE_BIN --transport stdio 2>&1 || true)

if echo "$NO_CONFIG_OUTPUT" | grep -q "config.*required"; then
    test_pass "Config flag requirement enforced"
else
    test_fail "Config flag requirement not enforced"
fi

# ============================================================================
# TEST 8: HTTP Mode - Server Start
# ============================================================================
test_start "HTTP Mode - Server Startup"

# Start HTTP server in background
$SKYLINE_BIN --transport http --bind localhost:18191 --admin=false \
    --config /tmp/skyline-test-config.yaml > /tmp/skyline-test-http.log 2>&1 &
TEST_SERVER_PID=$!

# Wait for server to start
sleep 3

if kill -0 $TEST_SERVER_PID 2>/dev/null; then
    test_pass "HTTP server started successfully (PID: $TEST_SERVER_PID)"
else
    test_fail "HTTP server failed to start"
    cat /tmp/skyline-test-http.log
fi

# ============================================================================
# TEST 9: HTTP Mode - MCP Endpoint
# ============================================================================
test_start "HTTP Mode - MCP Protocol"

HTTP_INIT=$(curl -s -X POST http://localhost:18191/mcp/v1 \
    -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' || echo "ERROR")

if echo "$HTTP_INIT" | jq -e '.result.protocolVersion' > /dev/null 2>&1; then
    test_pass "HTTP initialize successful"
else
    test_fail "HTTP initialize failed" "$HTTP_INIT"
fi

HTTP_TOOLS=$(curl -s -X POST http://localhost:18191/mcp/v1 \
    -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}')

if echo "$HTTP_TOOLS" | jq -e '.result.tools' > /dev/null 2>&1; then
    test_pass "HTTP tools/list successful"
else
    test_fail "HTTP tools/list failed"
fi

# Kill HTTP server
kill $TEST_SERVER_PID 2>/dev/null || true
TEST_SERVER_PID=""
sleep 1

# ============================================================================
# TEST 10: Multiple APIs Configuration
# ============================================================================
test_start "Multiple APIs Configuration"

# Use two reliable OpenAPI specs
cat > /tmp/skyline-test-multi.yaml << 'EOF'
apis:
  - name: petstore
    spec_url: https://petstore3.swagger.io/api/v3/openapi.json
    base_url_override: https://petstore3.swagger.io/api/v3
  
  - name: petstore2
    spec_url: https://petstore.swagger.io/v2/swagger.json
    base_url_override: https://petstore.swagger.io/v2
EOF

# Increase timeout for loading multiple APIs
MULTI_OUTPUT=$(( \
    echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}'; \
    echo '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' \
) | timeout 45 $SKYLINE_BIN --transport stdio --config /tmp/skyline-test-multi.yaml 2>&1)

# Extract just the JSON response (last line)
MULTI_TOOLS=$(echo "$MULTI_OUTPUT" | grep '^{' | tail -1)

# Check if we got a valid JSON response
if echo "$MULTI_TOOLS" | jq -e '.result.tools' > /dev/null 2>&1; then
    MULTI_TOOL_COUNT=$(echo "$MULTI_TOOLS" | jq -r '.result.tools | length')
    
    if [ "$MULTI_TOOL_COUNT" -gt 10 ]; then
        test_pass "Multiple APIs loaded ($MULTI_TOOL_COUNT tools total)"
    else
        test_fail "Multiple APIs tool count low" "Expected >10 tools, got $MULTI_TOOL_COUNT"
    fi
    
    # Check for tools from first API
    if echo "$MULTI_TOOLS" | jq -r '.result.tools[].name' | grep -q "petstore__"; then
        test_pass "First API (petstore) tools present"
    else
        test_fail "First API tools missing"
    fi
    
    # Check for tools from second API
    if echo "$MULTI_TOOLS" | jq -r '.result.tools[].name' | grep -q "petstore2__"; then
        test_pass "Second API (petstore2) tools present"
    else
        test_fail "Second API tools missing"
    fi
else
    test_fail "Multiple APIs loading failed" "No valid JSON response received"
    test_fail "First API tools check skipped" "Depends on API loading"
    test_fail "Second API tools check skipped" "Depends on API loading"
fi

# ============================================================================
# TEST 11: JSON Schema Validation
# ============================================================================
test_start "JSON Schema Validation"

TOOL_WITH_SCHEMA=$(echo "$TOOLS_RESPONSE" | jq -r '.result.tools[0]')

if echo "$TOOL_WITH_SCHEMA" | jq -e '.inputSchema' > /dev/null 2>&1; then
    test_pass "Tools have inputSchema"
else
    test_fail "Tools missing inputSchema"
fi

if echo "$TOOL_WITH_SCHEMA" | jq -e '.inputSchema.type' > /dev/null 2>&1; then
    test_pass "Input schema has type field"
else
    test_fail "Input schema missing type field"
fi

# ============================================================================
# Summary
# ============================================================================
echo ""
echo "═══════════════════════════════════════════════════════════════"
echo "  📊 Test Summary"
echo "═══════════════════════════════════════════════════════════════"
echo ""
echo "Tests Run:    $TESTS_RUN"
echo -e "Tests Passed: ${GREEN}$TESTS_PASSED${NC}"
echo -e "Tests Failed: ${RED}$TESTS_FAILED${NC}"
echo ""

if [ $TESTS_FAILED -eq 0 ]; then
    echo -e "${GREEN}✅ All tests passed!${NC}"
    exit 0
else
    echo -e "${RED}❌ Some tests failed${NC}"
    exit 1
fi
