# Skyline MCP - Test Suite

**Status:** ✅ All 20 tests passing (100%)  
**Script:** `test-ci.sh` (11.7KB)  
**CI/CD:** GitHub Actions workflow (`.github/workflows/test.yml`)

---

## Test Coverage

### 11 Test Scenarios

| # | Test Name | Checks | Transport |
|---|-----------|--------|-----------|
| 1 | Binary Execution | Version output, execution | - |
| 2 | STDIO - Basic Protocol | Initialize, protocol version | STDIO |
| 3 | STDIO - Tools List | Tools retrieval, count | STDIO |
| 4 | STDIO - Resources List | Resources retrieval | STDIO |
| 5 | STDIO - Error Handling | Invalid methods, error codes | STDIO |
| 6 | STDIO - Missing Config | Error messages | STDIO |
| 7 | STDIO - Config Required | Flag validation | STDIO |
| 8 | HTTP - Server Startup | Server starts with config | HTTP |
| 9 | HTTP - MCP Protocol | Initialize, tools/list | HTTP |
| 10 | Multiple APIs Config | Multi-API loading, isolation | STDIO |
| 11 | JSON Schema Validation | Schema presence, structure | STDIO |

**Total checks:** 20 assertions

---

## Running Tests

### Locally

```bash
# Prerequisites
go version  # 1.23+
jq --version

# Run all tests
./test-ci.sh

# Expected output:
# ═══════════════════════════════════════════════════════════════
#   📊 Test Summary
# ═══════════════════════════════════════════════════════════════
# 
# Tests Run:    11
# Tests Passed: 20
# Tests Failed: 0
# 
# ✅ All tests passed!
```

### In CI/CD

Tests run automatically on:
- Every push to `main` or `develop`
- Every pull request to `main`
- Manual trigger via GitHub Actions UI

**GitHub Actions workflow:** `.github/workflows/test.yml`

---

## Test Details

### Test 1: Binary Execution
**Purpose:** Verify binary builds and executes correctly

**Checks:**
- Binary executes without errors
- Version output contains "Skyline MCP"

### Test 2: STDIO - Basic MCP Protocol
**Purpose:** Verify STDIO transport implements MCP spec

**Config:** Single API (Petstore)

**Checks:**
- Initialize method responds with JSON-RPC 2.0
- Protocol version is `2025-11-25`

**Example:**
```bash
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' | \
  skyline --transport stdio --config config.yaml
```

### Test 3: STDIO - Tools List
**Purpose:** Verify tool discovery works

**Checks:**
- tools/list returns array
- Tool count > 0
- First tool has a name

### Test 4: STDIO - Resources List
**Purpose:** Verify resource discovery works

**Checks:**
- resources/list returns array

### Test 5: STDIO - Error Handling
**Purpose:** Verify proper MCP error responses

**Checks:**
- Invalid method returns JSON-RPC error
- Error code is `-32601` (method not found)

### Test 6: STDIO - Missing Config File
**Purpose:** Verify helpful error messages

**Checks:**
- Error message mentions "load config" or "no such file"

### Test 7: STDIO - Config Flag Required
**Purpose:** Verify --config flag is enforced

**Checks:**
- Running without --config shows "config.*required" error

### Test 8: HTTP - Server Startup
**Purpose:** Verify HTTP mode with direct config works

**Checks:**
- Server process starts and stays running
- PID is valid

**Command:**
```bash
skyline --transport http --bind localhost:18191 --admin=false \
  --config config.yaml
```

### Test 9: HTTP - MCP Protocol
**Purpose:** Verify HTTP MCP endpoint works

**Checks:**
- POST /mcp/v1 initialize succeeds
- POST /mcp/v1 tools/list succeeds

**Example:**
```bash
curl -X POST http://localhost:18191/mcp/v1 \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}'
```

### Test 10: Multiple APIs Configuration
**Purpose:** Verify multi-API support

**Config:** Two APIs (Petstore OpenAPI 3, Petstore Swagger 2)

**Checks:**
- Total tool count > 10
- Tools from first API present (petstore__)
- Tools from second API present (petstore2__)

### Test 11: JSON Schema Validation
**Purpose:** Verify tool schemas are valid

**Checks:**
- Tools have `inputSchema` field
- Input schema has `type` field

---

## Test Configuration

### Test APIs Used

**Primary:** Petstore OpenAPI 3.x
```yaml
spec_url: https://petstore3.swagger.io/api/v3/openapi.json
base_url_override: https://petstore3.swagger.io/api/v3
```

**Secondary:** Petstore Swagger 2.0
```yaml
spec_url: https://petstore.swagger.io/v2/swagger.json
base_url_override: https://petstore.swagger.io/v2
```

**Why these APIs:**
- Publicly available (no auth required)
- Reliable uptime
- Fast response times
- Different spec versions (tests compatibility)

### Timeouts

- Single API tests: 10 seconds
- Multi-API tests: 45 seconds
- HTTP server startup: 3 seconds

### Temporary Files

Tests create temporary files in `/tmp/`:
- `/tmp/skyline-test-config.yaml` - Single API config
- `/tmp/skyline-test-multi.yaml` - Multi-API config
- `/tmp/skyline-test-http.log` - HTTP server logs

All cleaned up automatically after test run.

---

## CI/CD Integration

### GitHub Actions Workflow

**File:** `.github/workflows/test.yml`

**Triggers:**
- Push to `main` or `develop` branches
- Pull requests to `main`
- Manual workflow dispatch

**Jobs:**
1. Checkout code
2. Setup Go 1.23
3. Install dependencies (jq, curl)
4. Build binary with `make build`
5. Run `./test-ci.sh`
6. Upload test artifacts (logs)

**Artifacts:**
- Test logs retained for 7 days
- Available in GitHub Actions UI

### Status Badge

Add to README.md:
```markdown
![Tests](https://github.com/emadomedher/skyline-mcp/actions/workflows/test.yml/badge.svg)
```

---

## Extending Tests

### Adding a New Test

1. Add test scenario to `test-ci.sh`:
```bash
# ============================================================================
# TEST X: Your Test Name
# ============================================================================
test_start "Your Test Name"

# Your test logic here
if [ condition ]; then
    test_pass "Check passed"
else
    test_fail "Check failed" "Error details"
fi
```

2. Update this document with test details

3. Run locally: `./test-ci.sh`

4. Commit and push - CI will run automatically

### Test Helper Functions

```bash
test_start "Test name"     # Start new test scenario
test_pass "Message"        # Mark check as passed
test_fail "Msg" "Details"  # Mark check as failed
```

---

## Known Limitations

### External Dependencies

Tests rely on external APIs:
- https://petstore3.swagger.io
- https://petstore.swagger.io

If these are down, tests may fail. This is acceptable for CI/CD (indicates external dependency issue, not code issue).

### Timeout Sensitivity

Multi-API tests use 45-second timeouts. On slow networks or CI runners, this may occasionally fail. Increase timeout if needed.

### Port Conflicts

HTTP tests use port `18191`. If this port is in use, test will fail. Consider randomizing port or checking availability.

---

## Troubleshooting

### Test Failures

**"Binary not found"**
```bash
make build  # Build binary first
```

**"jq: command not found"**
```bash
sudo apt-get install jq  # Ubuntu/Debian
brew install jq          # macOS
```

**"Connection refused" (HTTP tests)**
```bash
# Check if port is already in use
lsof -i :18191

# Kill conflicting process
kill $(lsof -t -i :18191)
```

**"Timeout" (Multi-API test)**
```bash
# Increase timeout in test-ci.sh line ~285
timeout 60 ...  # Instead of 45
```

### Debug Mode

Run tests with verbose output:
```bash
# Keep test artifacts
KEEP_TEMP=1 ./test-ci.sh

# View server logs
cat /tmp/skyline-test-http.log
```

---

## Test Maintenance

### Update Frequency

- Review test coverage quarterly
- Update test APIs if endpoints change
- Add tests for new features
- Keep timeouts realistic for CI runners

### Test Stability

Current stability: **100%** (20/20 passing)

Target: **>95%** (19/20 passing acceptable)

If stability drops below 95%, investigate and fix immediately.

---

**Last Updated:** 2026-02-13  
**Test Suite Version:** 1.0.0  
**Skyline Version:** 0.3.1+
