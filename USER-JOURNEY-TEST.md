# Skyline MCP - Complete User Journey Test Plan

**Version:** 0.3.1  
**Date:** 2026-02-13  
**Purpose:** Comprehensive testing checklist from zero to first successful MCP call

---

## 🎯 Test Scenarios Overview

This document covers **12 critical user paths** from installation to first LLM call:

| # | Scenario | Deno | Existing Config | Transport | Priority |
|---|----------|------|----------------|-----------|----------|
| 1 | Fresh install, no Deno, HTTP | ❌ | ❌ | HTTP | 🔴 HIGH |
| 2 | Fresh install, has Deno, HTTP | ✅ | ❌ | HTTP | 🔴 HIGH |
| 3 | Update existing, HTTP | ✅ | ✅ | HTTP | 🟡 MED |
| 4 | Fresh install, Web UI profile | ❌ | ❌ | HTTP | 🔴 HIGH |
| 5 | Fresh install, STDIO mode | ✅ | ❌ | STDIO | 🔴 HIGH |
| 6 | Encrypted profiles exist | ✅ | ✅ | HTTP | 🟢 LOW |
| 7 | Lost encryption key | ✅ | ✅ | HTTP | 🟢 LOW |
| 8 | Manual YAML config | ✅ | ❌ | HTTP | 🟡 MED |
| 9 | Manual YAML config | ✅ | ❌ | STDIO | 🟡 MED |
| 10 | Systemd service install | ✅ | ❌ | HTTP | 🟡 MED |
| 11 | Build from source | ✅ | ❌ | HTTP | 🟢 LOW |
| 12 | Multi-profile setup | ✅ | ✅ | HTTP | 🟢 LOW |

---

## 📋 Scenario 1: Fresh Install (No Deno) → HTTP + Web UI

**Context:** Brand new user, no Deno, wants to use Web UI  
**Priority:** 🔴 HIGH (most common path)

### Phase 1: Installation

- [ ] **1.1** Start from clean system (no skyline, no deno)
  ```bash
  which skyline   # should return: not found
  which deno      # should return: not found
  ```

- [ ] **1.2** Run installer
  ```bash
  curl -fsSL https://skyline.projex.cc/install | bash
  ```

- [ ] **1.3** Verify Deno prompt appears
  - [ ] Sees: "⚠️ Deno not found"
  - [ ] Sees: "Would you like to install Deno now?"
  - [ ] Default: yes

- [ ] **1.4** Confirm Deno install (press Enter or Y)

- [ ] **1.5** Verify Deno installation success
  - [ ] Sees: "✓ Deno installed: vX.X.X"
  - [ ] Sees: "Code execution enabled (98% cost reduction)"
  - [ ] Sees: "✓ Added Deno to ~/.bashrc" (or ~/.zshrc)

- [ ] **1.6** Verify binary installation
  - [ ] Sees: "✅ Installed to /usr/local/bin/" or "~/.local/bin/"
  - [ ] Sees version: "Version: v0.3.1"

- [ ] **1.7** Verify systemd prompt (Linux only)
  - [ ] Sees: "Would you like to install Skyline as a systemd service?"

- [ ] **1.8** Confirm systemd install (Y)

- [ ] **1.9** Verify config generation
  - [ ] Sees: "✓ Created ~/.skyline/"
  - [ ] Sees: "✓ Generated encryption key"
  - [ ] Sees: "✓ Created default config.yaml"
  - [ ] File exists: `~/.skyline/skyline.env`
  - [ ] File exists: `~/.skyline/config.yaml`

- [ ] **1.10** Confirm service start (Y)

- [ ] **1.11** Verify service running
  - [ ] Sees: "✅ Service started!"
  - [ ] Sees systemd status output
  - [ ] Service active: `systemctl --user status skyline`

### Phase 2: First Access

- [ ] **2.1** Open Web UI in browser
  ```
  http://localhost:19190/ui/
  ```

- [ ] **2.2** Verify UI loads
  - [ ] Skyline logo visible
  - [ ] Navigation: Dashboard, Profiles, Settings
  - [ ] Blue gradient theme

- [ ] **2.3** Navigate to Profiles page
  - [ ] Click "Profiles" in nav
  - [ ] Sees empty state or "Add Profile" button

### Phase 3: Create First Profile

- [ ] **3.1** Click "Add Profile" (or "Create Profile")

- [ ] **3.2** Fill in JSONPlaceholder test API
  ```yaml
  Profile Name: jsonplaceholder
  Token: test-token-123
  Config YAML:
  apis:
    - name: jsonplaceholder
      spec_url: https://jsonplaceholder.typicode.com/
      base_url_override: https://jsonplaceholder.typicode.com
      auth:
        type: bearer
        token: ${JSONPLACEHOLDER_TOKEN}
  ```

- [ ] **3.3** Click "Save" or "Create"

- [ ] **3.4** Verify profile created
  - [ ] Sees success message
  - [ ] Profile appears in list
  - [ ] Profile has name "jsonplaceholder"
  - [ ] Token shown (masked or visible)

- [ ] **3.5** Verify encrypted storage
  ```bash
  cat ~/.skyline/profiles.enc.yaml
  # Should see encrypted envelope (not plaintext)
  ```

### Phase 4: First MCP Call (HTTP Transport)

- [ ] **4.1** Get profile token from UI
  - [ ] Copy token value (e.g., "test-token-123")

- [ ] **4.2** Test tools/list endpoint
  ```bash
  curl -X POST http://localhost:8191/mcp/v1 \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer test-token-123" \
    -d '{
      "jsonrpc": "2.0",
      "method": "tools/list",
      "id": 1
    }'
  ```

- [ ] **4.3** Verify response structure
  - [ ] Status: 200 OK
  - [ ] Has `jsonrpc: "2.0"`
  - [ ] Has `result` object
  - [ ] Has `result.tools` array
  - [ ] Tools include JSONPlaceholder endpoints (posts, users, etc.)

- [ ] **4.4** Test tools/call endpoint
  ```bash
  curl -X POST http://localhost:8191/mcp/v1 \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer test-token-123" \
    -d '{
      "jsonrpc": "2.0",
      "method": "tools/call",
      "params": {
        "name": "jsonplaceholder__getPosts",
        "arguments": {}
      },
      "id": 2
    }'
  ```

- [ ] **4.5** Verify API call response
  - [ ] Status: 200 OK
  - [ ] Has `result` array
  - [ ] Contains post objects with `id`, `title`, `body`, `userId`

### Phase 5: Code Execution Test (if Deno installed)

- [ ] **5.1** Test code execution hint
  ```bash
  curl -X POST http://localhost:8191/mcp/v1 \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer test-token-123" \
    -d '{
      "jsonrpc": "2.0",
      "method": "tools/list",
      "params": {
        "cursor": "__code_execution_hint"
      },
      "id": 3
    }'
  ```

- [ ] **5.2** Verify code execution available
  - [ ] Response includes code execution instructions
  - [ ] Has TypeScript interface definitions

### ✅ Scenario 1 Success Criteria

- [x] Skyline installed
- [x] Deno installed automatically
- [x] Service running
- [x] Web UI accessible
- [x] Profile created & encrypted
- [x] HTTP MCP calls successful
- [x] Code execution available

---

## 📋 Scenario 2: Fresh Install (Has Deno) → HTTP + Web UI

**Context:** User already has Deno installed  
**Priority:** 🔴 HIGH

### Phase 1: Installation

- [ ] **1.1** Verify Deno exists
  ```bash
  deno --version   # should show version
  ```

- [ ] **1.2** Run installer
  ```bash
  curl -fsSL https://skyline.projex.cc/install | bash
  ```

- [ ] **1.3** Verify Deno detection
  - [ ] Sees: "✓ Deno found: vX.X.X"
  - [ ] Sees: "Code execution enabled (98% cost reduction)"
  - [ ] **Does NOT see** Deno installation prompt

- [ ] **1.4** Continue from Scenario 1, Phase 1, Step 1.6
  - [ ] Follow same verification steps

### ✅ Scenario 2 Success Criteria

Same as Scenario 1, but faster (no Deno install)

---

## 📋 Scenario 3: Update Existing Installation

**Context:** User has v0.1.x, updating to v0.3.1  
**Priority:** 🟡 MEDIUM

### Phase 1: Pre-Update State

- [ ] **1.1** Verify current version
  ```bash
  skyline --version
  # Should show: v0.1.x or earlier
  ```

- [ ] **1.2** Backup existing config
  ```bash
  cp ~/.skyline/config.yaml ~/.skyline/config.yaml.backup
  cp ~/.skyline/profiles.enc.yaml ~/.skyline/profiles.enc.yaml.backup
  ```

- [ ] **1.3** Note existing profiles
  ```bash
  # Remember profile tokens for testing after update
  ```

### Phase 2: Update

- [ ] **2.1** Run update command
  ```bash
  skyline update
  ```

- [ ] **2.2** Verify update process
  - [ ] Sees: "Checking for updates..."
  - [ ] Sees: "Latest version: v0.3.1"
  - [ ] Sees: "Update available: v0.1.x → v0.3.1"
  - [ ] Sees: "Downloading skyline-linux-amd64..." (or platform)
  - [ ] Sees: "✅ Successfully updated to v0.3.1!"

- [ ] **2.3** Verify new version
  ```bash
  skyline --version
  # Should show: v0.3.1
  ```

### Phase 3: Compatibility Check

- [ ] **3.1** Verify service still works (if systemd)
  ```bash
  systemctl --user restart skyline
  systemctl --user status skyline
  # Should show: active (running)
  ```

- [ ] **3.2** Verify Web UI accessible
  ```
  http://localhost:19190/ui/
  ```

- [ ] **3.3** Verify existing profiles work
  - [ ] Can see existing profiles in UI
  - [ ] Profiles decrypt correctly (key still works)

- [ ] **3.4** Test existing profile MCP call
  ```bash
  curl -X POST http://localhost:8191/mcp/v1 \
    -H "Authorization: Bearer <old-token>" \
    -d '{"jsonrpc":"2.0","method":"tools/list","id":1}'
  ```

### ✅ Scenario 3 Success Criteria

- [x] Update completed
- [x] Version = v0.3.1
- [x] Existing profiles still work
- [x] Encryption key still valid
- [x] Service runs with new binary

---

## 📋 Scenario 4: Web UI Profile Management

**Context:** User wants to use Web UI exclusively (no YAML editing)  
**Priority:** 🔴 HIGH (recommended workflow)

### Phase 1: Profile CRUD Operations

- [ ] **1.1** Create profile via UI (done in Scenario 1)

- [ ] **1.2** Edit existing profile
  - [ ] Click profile name or "Edit" button
  - [ ] Change API config (e.g., add new endpoint)
  - [ ] Click "Save"
  - [ ] Verify changes saved

- [ ] **1.3** Test edited profile
  ```bash
  curl -X POST http://localhost:8191/mcp/v1 \
    -H "Authorization: Bearer <token>" \
    -d '{"jsonrpc":"2.0","method":"tools/list","id":1}'
  ```
  - [ ] New endpoints appear in tools list

- [ ] **1.4** Create second profile (different API)
  ```yaml
  Profile Name: httpbin
  Token: httpbin-test-456
  Config YAML:
  apis:
    - name: httpbin
      spec_url: https://httpbin.org/spec.json
      base_url_override: https://httpbin.org
  ```

- [ ] **1.5** Verify multi-profile isolation
  ```bash
  # Call with first token
  curl ... -H "Authorization: Bearer test-token-123"
  # Should only see jsonplaceholder tools
  
  # Call with second token
  curl ... -H "Authorization: Bearer httpbin-test-456"
  # Should only see httpbin tools
  ```

- [ ] **1.6** Delete profile
  - [ ] Click "Delete" on test profile
  - [ ] Confirm deletion
  - [ ] Verify removed from list

- [ ] **1.7** Verify deleted profile token invalid
  ```bash
  curl -X POST http://localhost:8191/mcp/v1 \
    -H "Authorization: Bearer <deleted-token>" \
    -d '{"jsonrpc":"2.0","method":"tools/list","id":1}'
  ```
  - [ ] Status: 401 Unauthorized

### ✅ Scenario 4 Success Criteria

- [x] Can create profiles via UI
- [x] Can edit profiles via UI
- [x] Can delete profiles via UI
- [x] Profile isolation works (tokens separate configs)
- [x] Changes persist across restarts

---

## 📋 Scenario 5: STDIO Mode (Claude Desktop)

**Context:** User wants to integrate with Claude Desktop  
**Priority:** 🔴 HIGH (STDIO mode fully implemented in v0.3.1+)

### Phase 1: Installation (same as Scenario 1 or 2)

### Phase 2: Configuration File

- [ ] **2.1** Create manual config file
  ```bash
  mkdir -p ~/skyline-configs
  cat > ~/skyline-configs/jsonplaceholder.yaml << 'EOF'
  apis:
    - name: jsonplaceholder
      spec_url: https://jsonplaceholder.typicode.com/
      base_url_override: https://jsonplaceholder.typicode.com
  EOF
  ```

### Phase 3: Claude Desktop Config

- [ ] **3.1** Open Claude Desktop config
  ```bash
  # macOS
  code ~/Library/Application\ Support/Claude/claude_desktop_config.json
  
  # Linux
  code ~/.config/Claude/claude_desktop_config.json
  ```

- [ ] **3.2** Add Skyline MCP server
  ```json
  {
    "mcpServers": {
      "skyline-jsonplaceholder": {
        "command": "/usr/local/bin/skyline",
        "args": [
          "--config",
          "/Users/you/skyline-configs/jsonplaceholder.yaml",
          "--transport",
          "stdio"
        ]
      }
    }
  }
  ```

- [ ] **3.3** Restart Claude Desktop

- [ ] **3.4** Check for MCP server in Claude
  - [ ] Open Claude Desktop
  - [ ] Look for server connection status (usually in settings or status bar)
  - [ ] Should show "skyline-jsonplaceholder" as connected

### Phase 4: Verify Integration

- [ ] **4.1** Test tool availability
  - [ ] Type a message mentioning JSONPlaceholder in Claude
  - [ ] Claude should have access to API tools
  - [ ] Example: "List the posts from the JSONPlaceholder API"

- [ ] **4.2** Verify tool execution
  - [ ] Claude should call the appropriate MCP tool
  - [ ] Should return actual data from JSONPlaceholder API
  - [ ] Check Claude's response includes post titles, IDs, etc.

- [ ] **4.3** Check logs (optional)
  - [ ] STDIO mode logs to stderr
  - [ ] Check Claude Desktop logs for MCP communication
  - [ ] No errors about protocol or tool execution

### ✅ Scenario 5 Success Criteria (v0.3.1+)

- [x] Config file works
- [x] STDIO transport implemented ✅
- [x] Claude Desktop integration working ✅
- [x] MCP tools accessible in Claude
- [x] Tool calls execute successfully

---

## 📋 Scenario 6: Encrypted Profiles Exist

**Context:** User returns, encrypted profiles already exist  
**Priority:** 🟢 LOW (happy path after first setup)

### Phase 1: Load Existing Key

- [ ] **1.1** Verify profiles file exists
  ```bash
  ls -la ~/.skyline/profiles.enc.yaml
  # Should exist
  ```

- [ ] **1.2** Verify encryption key file exists
  ```bash
  ls -la ~/.skyline/skyline.env
  # Should exist
  ```

- [ ] **1.3** Start Skyline
  ```bash
  # Load key from env file
  source ~/.skyline/skyline.env
  skyline
  ```

- [ ] **1.4** Verify successful decryption
  - [ ] No error about missing key
  - [ ] Service starts normally
  - [ ] Web UI shows existing profiles

- [ ] **1.5** Test profile access
  ```bash
  curl -X POST http://localhost:8191/mcp/v1 \
    -H "Authorization: Bearer <existing-token>" \
    -d '{"jsonrpc":"2.0","method":"tools/list","id":1}'
  ```

### ✅ Scenario 6 Success Criteria

- [x] Key loads from skyline.env
- [x] Profiles decrypt successfully
- [x] MCP calls work with existing tokens

---

## 📋 Scenario 7: Lost Encryption Key

**Context:** User lost encryption key, has encrypted profiles  
**Priority:** 🟢 LOW (error handling)

### Phase 1: Reproduction

- [ ] **1.1** Simulate lost key
  ```bash
  unset SKYLINE_PROFILES_KEY
  rm ~/.skyline/skyline.env  # Don't do this in real scenario!
  ```

- [ ] **1.2** Try to start Skyline
  ```bash
  skyline
  ```

- [ ] **1.3** Verify helpful error message
  - [ ] Sees: "🔐 Encrypted profiles file found"
  - [ ] Sees: "❌ SKYLINE_PROFILES_KEY environment variable is not set"
  - [ ] Sees instructions for recovery
  - [ ] Sees: "If you lost the key: ⚠️ Your profiles are permanently encrypted"

### Phase 2: Recovery (Nuclear Option)

- [ ] **2.1** Delete encrypted profiles
  ```bash
  rm ~/.skyline/profiles.enc.yaml
  ```

- [ ] **2.2** Start Skyline
  ```bash
  skyline
  ```

- [ ] **2.3** Verify fresh key generation
  - [ ] Sees: "✅ Generated new encryption key"
  - [ ] New skyline.env created
  - [ ] Empty profiles.enc.yaml created

- [ ] **2.4** Recreate profiles from scratch

### ✅ Scenario 7 Success Criteria

- [x] Clear error when key missing
- [x] Instructions for recovery
- [x] Can start fresh after deleting old profiles

---

## 📋 Scenario 8: Manual YAML Config (HTTP)

**Context:** Power user prefers YAML files over Web UI  
**Priority:** 🟡 MEDIUM

### Phase 1: Create Config File

- [ ] **1.1** Create config directory
  ```bash
  mkdir -p ~/skyline-manual
  ```

- [ ] **1.2** Create config.yaml
  ```bash
  cat > ~/skyline-manual/config.yaml << 'EOF'
  apis:
    - name: jsonplaceholder
      spec_url: https://jsonplaceholder.typicode.com/
      base_url_override: https://jsonplaceholder.typicode.com
      
    - name: httpbin
      spec_url: https://httpbin.org/spec.json
      base_url_override: https://httpbin.org
  EOF
  ```

### Phase 2: Start Skyline

- [ ] **2.1** Start with config file (no profiles mode)
  ```bash
  skyline --config ~/skyline-manual/config.yaml --bind localhost:8192
  ```

- [ ] **2.2** Verify startup
  - [ ] Sees: "Loading config from ~/skyline-manual/config.yaml"
  - [ ] Sees: "Loaded 2 APIs"
  - [ ] Sees: "Listening on localhost:8192"

### Phase 3: Test MCP Calls (No Auth)

- [ ] **3.1** Test tools/list (no auth token needed)
  ```bash
  curl -X POST http://localhost:8192/mcp/v1 \
    -H "Content-Type: application/json" \
    -d '{
      "jsonrpc": "2.0",
      "method": "tools/list",
      "id": 1
    }'
  ```

- [ ] **3.2** Verify both APIs present
  - [ ] Tools from jsonplaceholder
  - [ ] Tools from httpbin

### ✅ Scenario 8 Success Criteria

- [x] Can use YAML config files
- [x] No Web UI/profiles needed
- [x] MCP calls work without auth tokens

---

## 📋 Scenario 9: Manual YAML Config (STDIO)

**Context:** Power user wants STDIO with YAML config  
**Priority:** 🟡 MEDIUM

### Phase 1-2: Same as Scenario 8

### Phase 3: STDIO Mode

- [ ] **3.1** Start in STDIO mode
  ```bash
  skyline --config ~/skyline-manual/config.yaml --transport stdio
  ```

- [ ] **3.2** Verify startup (check stderr)
  - [ ] Sees: "🚀 Skyline MCP Server - STDIO Mode"
  - [ ] Sees: "Config: ~/skyline-manual/config.yaml"
  - [ ] Sees: "APIs: 2 configured"
  - [ ] Sees: "✓ Loaded 2 services"
  - [ ] Sees: "📡 Ready for MCP protocol over STDIO"

- [ ] **3.3** Test MCP protocol manually
  ```bash
  echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' | \
    skyline --config ~/skyline-manual/config.yaml --transport stdio 2>/dev/null
  ```
  - [ ] Response: JSON with `protocolVersion: "2025-11-25"`

- [ ] **3.4** Test tools/list
  ```bash
  (
    echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}';
    echo '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'
  ) | skyline --config ~/skyline-manual/config.yaml --transport stdio 2>/dev/null
  ```
  - [ ] Response: JSON with tools array
  - [ ] Tools from both APIs present

### ✅ Scenario 9 Success Criteria (v0.3.1+)

- [x] Config file loads
- [x] STDIO transport implemented ✅
- [x] MCP protocol working over STDIO
- [x] Multiple APIs accessible via STDIO

---

## 📋 Scenario 10: Systemd Service Install

**Context:** User wants background service  
**Priority:** 🟡 MEDIUM

### Phase 1: Installation (Scenario 1, steps with systemd)

### Phase 2: Service Management

- [ ] **2.1** Check service status
  ```bash
  systemctl --user status skyline
  ```
  - [ ] Shows: active (running)

- [ ] **2.2** View logs
  ```bash
  journalctl --user -u skyline -f
  ```
  - [ ] Sees startup logs
  - [ ] Sees "Listening on localhost:19190"

- [ ] **2.3** Restart service
  ```bash
  systemctl --user restart skyline
  ```

- [ ] **2.4** Verify auto-start on boot
  ```bash
  systemctl --user is-enabled skyline
  ```
  - [ ] Shows: enabled

- [ ] **2.5** Stop service
  ```bash
  systemctl --user stop skyline
  ```

- [ ] **2.6** Verify Web UI unreachable
  ```bash
  curl http://localhost:19190/ui/
  # Should fail (connection refused)
  ```

- [ ] **2.7** Start service again
  ```bash
  systemctl --user start skyline
  ```

### ✅ Scenario 10 Success Criteria

- [x] Service installs correctly
- [x] Can start/stop/restart
- [x] Auto-starts on boot
- [x] Logs accessible via journalctl

---

## 📋 Scenario 11: Build from Source

**Context:** User wants latest development version  
**Priority:** 🟢 LOW

### Phase 1: Prerequisites

- [ ] **1.1** Verify Go installed
  ```bash
  go version
  # Should be 1.23+
  ```

- [ ] **1.2** Install Go if needed
  ```bash
  # Follow https://go.dev/dl/
  ```

### Phase 2: Clone & Build

- [ ] **2.1** Clone repository
  ```bash
  git clone https://github.com/emadomedher/skyline-mcp.git
  cd skyline-mcp
  ```

- [ ] **2.2** Build binary
  ```bash
  make build
  # OR
  go build -o bin/skyline ./cmd/skyline
  ```

- [ ] **2.3** Verify binary created
  ```bash
  ls -lh bin/skyline
  # Should exist, ~15-20MB
  ```

- [ ] **2.4** Test binary
  ```bash
  ./bin/skyline --version
  # Should show version
  ```

### Phase 3: Install

- [ ] **3.1** Install system-wide
  ```bash
  sudo make install
  # OR
  sudo cp bin/skyline /usr/local/bin/skyline
  ```

- [ ] **3.2** Verify installation
  ```bash
  which skyline
  skyline --version
  ```

### Phase 4: Continue with Scenario 1, Phase 2+

### ✅ Scenario 11 Success Criteria

- [x] Built from source
- [x] Binary works identically to release binary
- [x] Can install system-wide

---

## 📋 Scenario 12: Multi-Profile Production Setup

**Context:** Team environment with multiple API profiles  
**Priority:** 🟢 LOW (advanced usage)

### Phase 1: Create Multiple Profiles

- [ ] **1.1** Development profile
  ```yaml
  Name: dev-api
  Token: dev-token-abc123
  Config:
  apis:
    - name: dev-api
      spec_url: https://dev.api.example.com/openapi.json
      base_url_override: https://dev.api.example.com
      auth:
        type: bearer
        token: ${DEV_API_KEY}
  ```

- [ ] **1.2** Staging profile
  ```yaml
  Name: staging-api
  Token: staging-token-xyz789
  Config:
  apis:
    - name: staging-api
      spec_url: https://staging.api.example.com/openapi.json
      base_url_override: https://staging.api.example.com
      auth:
        type: bearer
        token: ${STAGING_API_KEY}
  ```

- [ ] **1.3** Production profile
  ```yaml
  Name: prod-api
  Token: prod-token-secure-456
  Config:
  apis:
    - name: prod-api
      spec_url: https://api.example.com/openapi.json
      base_url_override: https://api.example.com
      auth:
        type: bearer
        token: ${PROD_API_KEY}
  ```

### Phase 2: Token Distribution

- [ ] **2.1** Share encryption key (secure channel)
  ```bash
  # Share ~/.skyline/skyline.env via 1Password/Vault
  ```

- [ ] **2.2** Share profile tokens per team member
  - [ ] Junior dev: dev-token-abc123 only
  - [ ] Senior dev: dev + staging tokens
  - [ ] DevOps: all three tokens

### Phase 3: Test Isolation

- [ ] **3.1** Test dev token can't access staging
  ```bash
  curl -X POST http://localhost:8191/mcp/v1 \
    -H "Authorization: Bearer dev-token-abc123" \
    -d '{"jsonrpc":"2.0","method":"tools/list","id":1}'
  ```
  - [ ] Only sees dev-api tools

- [ ] **3.2** Test staging token isolation
  ```bash
  curl -X POST http://localhost:8191/mcp/v1 \
    -H "Authorization: Bearer staging-token-xyz789" \
    -d '{"jsonrpc":"2.0","method":"tools/list","id":1}'
  ```
  - [ ] Only sees staging-api tools

- [ ] **3.3** Test prod token (DevOps only)
  ```bash
  curl -X POST http://localhost:8191/mcp/v1 \
    -H "Authorization: Bearer prod-token-secure-456" \
    -d '{"jsonrpc":"2.0","method":"tools/list","id":1}'
  ```
  - [ ] Only sees prod-api tools

### ✅ Scenario 12 Success Criteria

- [x] Multiple profiles created
- [x] Token-based access control works
- [x] Team members have appropriate access
- [x] Production isolated from dev/staging

---

## 🧪 Quick Smoke Test (5 minutes)

Run this after any code change:

```bash
# 1. Install/update
curl -fsSL https://skyline.projex.cc/install | bash

# 2. Verify version
skyline --version

# 3. Start service
systemctl --user restart skyline

# 4. Check Web UI
curl -s http://localhost:19190/ui/ | grep -q "Skyline" && echo "✅ UI OK"

# 5. Create test profile via UI (manual)
# Open http://localhost:19190/ui/

# 6. Test MCP call
curl -X POST http://localhost:8191/mcp/v1 \
  -H "Authorization: Bearer <your-token>" \
  -d '{"jsonrpc":"2.0","method":"tools/list","id":1}' \
  | jq -r '.result.tools[0].name' && echo "✅ MCP OK"

# 7. Check logs
journalctl --user -u skyline --since "5 minutes ago" | tail -20
```

---

## 📊 Test Coverage Matrix

| Feature | Scenario | Status |
|---------|----------|--------|
| Fresh install (no Deno) | 1 | ⬜ |
| Fresh install (with Deno) | 2 | ⬜ |
| Update existing | 3 | ⬜ |
| Web UI profile CRUD | 4 | ⬜ |
| STDIO mode | 5 | ⬜ |
| Encrypted profiles | 6 | ⬜ |
| Lost key recovery | 7 | ⬜ |
| Manual YAML (HTTP) | 8 | ⬜ |
| Manual YAML (STDIO) | 9 | ⬜ |
| Systemd service | 10 | ⬜ |
| Build from source | 11 | ⬜ |
| Multi-profile team | 12 | ⬜ |

---

## 🐛 Common Issues & Solutions

### Issue: "Deno not found" after install

**Solution:**
```bash
# Reload shell profile
source ~/.bashrc  # or ~/.zshrc

# Verify
deno --version
```

### Issue: "Connection refused" to Web UI

**Solution:**
```bash
# Check service status
systemctl --user status skyline

# Check port binding
ss -tlnp | grep 19190

# Restart service
systemctl --user restart skyline
```

### Issue: "Encryption key required"

**Solution:**
```bash
# Load key from env file
source ~/.skyline/skyline.env

# Or set manually
export SKYLINE_PROFILES_KEY="<your-key>"

# Restart
systemctl --user restart skyline
```

### Issue: Profile token not working (401)

**Solution:**
- Verify token matches exactly (copy from UI)
- Check Bearer prefix: `Authorization: Bearer <token>`
- Verify profile exists in Web UI
- Check service logs: `journalctl --user -u skyline`

### Issue: MCP call returns empty tools

**Solution:**
- Verify API spec_url is accessible
- Check service logs for spec loading errors
- Test API spec directly: `curl <spec_url>`
- Verify base_url_override is correct

---

## 📝 Test Log Template

```markdown
## Test Run: YYYY-MM-DD HH:MM

**Tester:** [Your Name]
**Version:** v0.3.1
**Platform:** Linux/macOS (architecture)
**Scenarios Tested:** [List numbers]

### Results

#### Scenario X: [Name]
- Status: ✅ PASS / ❌ FAIL / ⏭️ SKIP
- Duration: X minutes
- Issues: [None / List issues]
- Notes: [Any observations]

### Summary

- Total scenarios: X
- Passed: X
- Failed: X
- Skipped: X
- Blocker issues: [None / List]

### Action Items

- [ ] Fix issue #1
- [ ] Update docs for scenario Y
- [ ] Retest scenarios Z
```

---

**Last Updated:** 2026-02-13  
**Next Review:** After STDIO transport implementation
