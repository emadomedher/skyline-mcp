# STDIO Mode Implementation - Complete

**Status:** ✅ Fully Implemented (v0.3.1+)  
**Date:** 2026-02-13  
**Protocol:** MCP 2025-11-25

---

## Overview

STDIO transport is now **fully functional** in Skyline MCP, enabling direct integration with Claude Desktop, Cursor IDE, and any MCP-compatible client.

### What Changed

**Before (v0.3.0):**
```bash
$ skyline --transport stdio
⚠️  STDIO transport not yet implemented
```

**After (v0.3.1+):**
```bash
$ skyline --transport stdio --config config.yaml
✅ Server initialized successfully
📡 Ready for MCP protocol over STDIO
```

---

## Implementation Details

### Architecture

```
┌──────────────────┐
│   MCP Client     │
│ (Claude Desktop) │
└────────┬─────────┘
         │
         │ stdin/stdout
         │ (JSON-RPC 2.0)
         │
         ▼
┌──────────────────┐
│  skyline binary  │
│  --transport     │
│    stdio         │
└────────┬─────────┘
         │
         │ internal.mcp.Server.Serve()
         │
         ▼
┌──────────────────┐
│   API Backends   │
│ (REST, GraphQL,  │
│  SOAP, gRPC...)  │
└──────────────────┘
```

### Code Changes

**File:** `cmd/skyline/main.go`

**1. Early STDIO Check** (lines 117-125)
```go
// Handle STDIO transport mode early (before profile/encryption logic)
if *transport == "stdio" {
    if err := runSTDIO(*configPath, logger); err != nil {
        logger.Fatalf("STDIO mode error: %v", err)
    }
    return
}
```

**Why early?** STDIO mode doesn't need profile encryption or HTTP server setup. Checking early skips unnecessary initialization.

**2. runSTDIO Function** (lines 1920-2000, ~70 lines)
```go
func runSTDIO(configPathArg string, logger *log.Logger) error {
    ctx := context.Background()
    
    // 1. Require --config flag
    if configPathArg == "" {
        return fmt.Errorf("--config flag required for STDIO mode")
    }
    
    // 2. Load config file
    cfg, err := config.Load(configPath)
    
    // 3. Initialize redactor (with nil check for auth)
    redactor := redact.NewRedactor()
    for _, api := range cfg.APIs {
        if api.Auth != nil {
            // Add secrets to redactor...
        }
    }
    
    // 4. Load API services (OpenAPI, GraphQL, etc.)
    services, err := spec.LoadServices(ctx, cfg, logger, redactor)
    
    // 5. Build MCP registry (tools + resources)
    registry, err := mcp.NewRegistry(services)
    
    // 6. Initialize executor
    executor, err := runtime.NewExecutor(cfg, services, logger, redactor)
    
    // 7. Create MCP server
    mcpServer := mcp.NewServer(registry, executor, logger, redactor)
    
    // 8. Run server in STDIO mode
    return mcpServer.Serve(ctx, os.Stdin, os.Stdout)
}
```

**Key points:**
- All logs go to `stderr` (not `stdout`)
- `stdout` reserved exclusively for MCP JSON-RPC protocol
- Uses existing `mcp.Server.Serve(io.Reader, io.Writer)` method
- No HTTP, no networking, no ports

### Nil Auth Bug Fix

**Issue:** Config files without auth fields caused nil pointer dereference.

**Fix:**
```go
for _, api := range cfg.APIs {
    if api.Auth != nil {  // ← Added nil check
        if api.Auth.Token != "" {
            redactor.AddSecrets([]string{api.Auth.Token})
        }
        // ...
    }
}
```

This allows APIs without authentication (e.g., public APIs) to work in STDIO mode.

---

## Usage

### Basic Command

```bash
skyline --transport stdio --config config.yaml
```

**Requirements:**
- `--config` flag is **required** (no profiles mode for STDIO)
- Config file must be valid YAML with at least one API

### Example Config

```yaml
# config.yaml
apis:
  - name: petstore
    spec_url: https://petstore3.swagger.io/api/v3/openapi.json
    base_url_override: https://petstore3.swagger.io/api/v3

  - name: github
    spec_url: https://raw.githubusercontent.com/github/rest-api-description/main/descriptions/api.github.com/api.github.com.json
    base_url_override: https://api.github.com
    auth:
      type: bearer
      token: ${GITHUB_TOKEN}
```

### Manual Test

```bash
# Send MCP initialize
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' | \
  skyline --transport stdio --config config.yaml

# Expected response (on stdout):
# {"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-11-25",...}}
```

---

## Claude Desktop Integration

### Config File

**macOS:** `~/Library/Application Support/Claude/claude_desktop_config.json`  
**Linux:** `~/.config/Claude/claude_desktop_config.json`  
**Windows:** `%APPDATA%\Claude\claude_desktop_config.json`

### Example Configuration

```json
{
  "mcpServers": {
    "skyline-apis": {
      "command": "/usr/local/bin/skyline",
      "args": [
        "--transport",
        "stdio",
        "--config",
        "/path/to/config.yaml"
      ],
      "env": {
        "GITHUB_TOKEN": "ghp_your_token_here"
      }
    }
  }
}
```

### Multiple Profiles

```json
{
  "mcpServers": {
    "skyline-dev": {
      "command": "/usr/local/bin/skyline",
      "args": ["--transport", "stdio", "--config", "/configs/dev.yaml"]
    },
    "skyline-prod": {
      "command": "/usr/local/bin/skyline",
      "args": ["--transport", "stdio", "--config", "/configs/prod.yaml"]
    }
  }
}
```

Each profile runs as a separate MCP server instance with its own tools.

---

## Protocol Compliance

### Supported Methods

| Method | Status | Description |
|--------|--------|-------------|
| `initialize` | ✅ | Protocol handshake |
| `tools/list` | ✅ | List all available tools |
| `tools/call` | ✅ | Execute a tool |
| `resources/list` | ✅ | List available resources |
| `resources/read` | ✅ | Read a resource |
| `resources/templates` | ✅ | List resource templates (empty) |
| `ping` | ✅ | Health check |

### Protocol Version

**Current:** `2025-11-25` (MCP specification)

### Message Format

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/list",
  "params": {}
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "tools": [
      {
        "name": "petstore__addPet",
        "description": "Add a new pet to the store",
        "inputSchema": { ... },
        "outputSchema": { ... }
      }
    ]
  }
}
```

**Error:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "error": {
    "code": -32601,
    "message": "method not found"
  }
}
```

---

## Testing

### Automated Tests

**File:** `test-ci.sh` (11 test scenarios)

```bash
# Run all tests
./test-ci.sh

# Output:
# TEST 1: Binary Execution ✓
# TEST 2: STDIO Mode - Basic MCP Protocol ✓
# TEST 3: STDIO Mode - Tools List ✓
# TEST 4: STDIO Mode - Resources List ✓
# TEST 5: STDIO Mode - Error Handling ✓
# ...
```

**STDIO-specific tests:**
- Initialize handshake
- Tools list retrieval
- Resources list retrieval
- Error handling (invalid methods)
- Missing config detection
- Config flag requirement

### Manual Testing

**Test 1: Initialize**
```bash
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' | \
  skyline --transport stdio --config config.yaml 2>/dev/null
```

**Test 2: List Tools**
```bash
(
  echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}';
  echo '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'
) | skyline --transport stdio --config config.yaml 2>/dev/null
```

**Test 3: Call Tool**
```bash
(
  echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}';
  echo '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"petstore__listPets","arguments":{}}}'
) | skyline --transport stdio --config config.yaml 2>/dev/null
```

---

## Troubleshooting

### Issue: No output

**Symptom:**
```bash
$ echo '...' | skyline --transport stdio --config config.yaml
# No output
```

**Causes:**
1. Logs are on stderr (redirect: `2>/dev/null` to see only JSON)
2. Server waiting for more input (STDIO is session-based)
3. Config file loading error (check stderr output)

**Solution:**
```bash
# Redirect stderr to see logs
echo '...' | skyline --transport stdio --config config.yaml 2>&1 | grep -v "^20"
```

### Issue: "config flag required"

**Symptom:**
```
STDIO mode error: --config flag required for STDIO mode
```

**Solution:**
STDIO mode **always** requires `--config`:
```bash
skyline --transport stdio --config /path/to/config.yaml
```

### Issue: API spec not loading

**Symptom:**
```
STDIO mode error: load services: apis[0]: no supported spec detected
```

**Causes:**
- Invalid `spec_url` (404, network error)
- Spec format not recognized (not OpenAPI/GraphQL/etc.)
- Missing `base_url_override` for some API types

**Solution:**
1. Test spec URL directly: `curl $spec_url`
2. Verify spec format (OpenAPI should have `openapi: "3.x.x"`)
3. Add `base_url_override` if spec doesn't include server URLs

---

## Performance

### Comparison: HTTP vs STDIO

| Metric | HTTP | STDIO |
|--------|------|-------|
| Latency | ~5-10ms (TCP overhead) | ~1-2ms (IPC) |
| Startup | Port binding, HTTP stack | Direct process spawn |
| Security | Network exposure | Process isolation |
| Complexity | Middleware, routing | Simple pipe |

**Recommendation:** Use STDIO for local clients (Claude Desktop), HTTP for networked clients (remote AI agents).

---

## Changelog

**v0.3.1 (2026-02-13):**
- ✅ STDIO transport fully implemented
- ✅ Added `runSTDIO()` function
- ✅ Fixed nil auth pointer bug
- ✅ Added automated tests (test-ci.sh)
- ✅ Updated documentation

**v0.3.0 and earlier:**
- ❌ STDIO transport placeholder (not implemented)

---

## References

- **MCP Specification:** https://spec.modelcontextprotocol.io/specification/basic/transports/#stdio
- **STDIO-MODE.md:** Complete usage guide
- **test-ci.sh:** Automated test suite
- **USER-JOURNEY-TEST.md:** Scenario 5 (STDIO mode)

---

**Implementation Status:** ✅ **COMPLETE**  
**Ready for Production:** ✅ **YES**  
**Claude Desktop Compatible:** ✅ **YES**
