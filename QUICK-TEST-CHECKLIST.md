# Skyline MCP - Quick Test Checklist

**Version:** 0.3.1 | **1-Page Reference**

---

## ✅ Priority 1: Fresh Install Test (15 min)

**Start:** Clean system (no skyline, optionally no deno)

### Installation
```bash
curl -fsSL https://skyline.projex.cc/install | bash
```

- [ ] Installer runs without errors
- [ ] Deno prompt appears (if not installed) ✓ or skipped
- [ ] Deno installs successfully (if chosen) ✓
- [ ] Binary installed: `which skyline` → path shown
- [ ] Version correct: `skyline --version` → v0.3.1
- [ ] Systemd prompt (Linux): asks to install service
- [ ] Config created: `ls ~/.skyline/` → config.yaml, skyline.env
- [ ] Service started: `systemctl --user status skyline` → active

### Web UI Access
```bash
open http://localhost:19190/ui/
```

- [ ] UI loads (Skyline logo, blue theme)
- [ ] Can navigate: Dashboard → Profiles → Settings
- [ ] No JavaScript errors (check browser console)

### Create Test Profile

**Profile Config:**
```yaml
Profile Name: test
Token: test-token-123
Config:
apis:
  - name: jsonplaceholder
    spec_url: https://jsonplaceholder.typicode.com/
    base_url_override: https://jsonplaceholder.typicode.com
```

- [ ] "Create Profile" button works
- [ ] Form accepts YAML input
- [ ] Save succeeds
- [ ] Profile appears in list
- [ ] Token shown/copyable

### First MCP Call
```bash
curl -X POST http://localhost:8191/mcp/v1 \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer test-token-123" \
  -d '{"jsonrpc":"2.0","method":"tools/list","id":1}'
```

- [ ] HTTP 200 response
- [ ] Has `jsonrpc: "2.0"`
- [ ] Has `result.tools` array
- [ ] Tools include: `jsonplaceholder__*` operations

### Call an API
```bash
curl -X POST http://localhost:8191/mcp/v1 \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer test-token-123" \
  -d '{
    "jsonrpc":"2.0",
    "method":"tools/call",
    "params":{
      "name":"jsonplaceholder__GET_posts",
      "arguments":{}
    },
    "id":2
  }'
```

- [ ] HTTP 200 response
- [ ] Has `result` with array of posts
- [ ] Posts have: id, title, body, userId

**✅ PASS CRITERIA:** All checkboxes ✓ = Fresh install works end-to-end

---

## ✅ Priority 2: Update Test (5 min)

**Start:** Existing v0.1.x installation

### Backup
```bash
cp ~/.skyline/config.yaml ~/.skyline/config.yaml.backup
cp ~/.skyline/profiles.enc.yaml ~/.skyline/profiles.enc.yaml.backup
```

### Update
```bash
skyline update
```

- [ ] Detects current version: v0.1.x
- [ ] Downloads v0.3.1
- [ ] Verifies new binary
- [ ] Replaces old binary
- [ ] Success message shown

### Verify
```bash
skyline --version
systemctl --user restart skyline
systemctl --user status skyline
curl http://localhost:19190/ui/
```

- [ ] Version = v0.3.1
- [ ] Service restarts OK
- [ ] Web UI accessible
- [ ] Existing profiles still work (test MCP call)

**✅ PASS CRITERIA:** Update succeeds, existing data intact

---

## ✅ Priority 3: Systemd Service (3 min)

```bash
systemctl --user status skyline
journalctl --user -u skyline -n 50
systemctl --user restart skyline
systemctl --user is-enabled skyline
```

- [ ] Status: active (running)
- [ ] Logs: no errors, shows "Listening on..."
- [ ] Restart works
- [ ] Enabled: yes (auto-start on boot)

**✅ PASS CRITERIA:** Service management works

---

## ✅ Priority 4: Profile Management (5 min)

### Via Web UI

**Create:**
- [ ] Click "Add Profile"
- [ ] Fill name, token, config
- [ ] Save succeeds
- [ ] Appears in list

**Edit:**
- [ ] Click profile or "Edit"
- [ ] Modify config (add/change API)
- [ ] Save succeeds
- [ ] Changes reflected (test MCP call)

**Delete:**
- [ ] Click "Delete" on test profile
- [ ] Confirm
- [ ] Removed from list
- [ ] Token no longer works (401)

### Encryption
```bash
cat ~/.skyline/profiles.enc.yaml
```

- [ ] File is encrypted (not plaintext YAML)
- [ ] Has `version`, `nonce`, `ciphertext` fields

**✅ PASS CRITERIA:** CRUD operations work, data encrypted

---

## ✅ Priority 5: Code Execution (2 min)

**Requires:** Deno installed

```bash
curl -X POST http://localhost:8191/mcp/v1 \
  -H "Authorization: Bearer test-token-123" \
  -d '{
    "jsonrpc":"2.0",
    "method":"tools/list",
    "params":{"cursor":"__code_execution_hint"},
    "id":1
  }'
```

- [ ] Response includes code execution docs
- [ ] Has TypeScript interfaces
- [ ] Instructions for writing scripts

**✅ PASS CRITERIA:** Code execution hints available (if Deno present)

---

## 🔧 Quick Smoke Test (2 min)

**Run after any change:**

```bash
# 1. Check binary
skyline --version

# 2. Check service
systemctl --user status skyline

# 3. Check Web UI
curl -s http://localhost:19190/ui/ | grep -q Skyline && echo "✅ UI"

# 4. Check MCP endpoint
curl -s -X POST http://localhost:8191/mcp/v1 \
  -H "Authorization: Bearer test-token-123" \
  -d '{"jsonrpc":"2.0","method":"tools/list","id":1}' \
  | jq -e '.result.tools' > /dev/null && echo "✅ MCP"

# 5. Check logs (no errors)
journalctl --user -u skyline --since "5 min ago" | grep -i error
```

**✅ PASS CRITERIA:** All 4 checks pass, no errors in logs

---

## ⚠️ Known Limitations (v0.3.1)

- [ ] **STDIO transport:** Placeholder only (not implemented)
- [ ] **Claude Desktop:** Cannot integrate until STDIO works
- [ ] **Manual YAML + STDIO:** Blocked by STDIO limitation

**Workaround:** Use HTTP transport for all testing

---

## 🐛 Troubleshooting Quick Fixes

| Issue | Fix |
|-------|-----|
| Deno not found | `source ~/.bashrc && deno --version` |
| UI connection refused | `systemctl --user restart skyline` |
| Encryption key error | `source ~/.skyline/skyline.env` |
| Profile token 401 | Verify token exact match (copy from UI) |
| Empty tools list | Check logs: `journalctl --user -u skyline -n 50` |

---

## 📊 Test Status Tracker

**Date:** __________ **Tester:** __________

| Priority | Test | Time | Status | Notes |
|----------|------|------|--------|-------|
| P1 | Fresh Install | 15m | ⬜ | |
| P2 | Update | 5m | ⬜ | |
| P3 | Systemd | 3m | ⬜ | |
| P4 | Profiles | 5m | ⬜ | |
| P5 | Code Exec | 2m | ⬜ | |
| - | Smoke Test | 2m | ⬜ | |

**Total Time:** ~30 minutes  
**Pass Rate:** ___/6  
**Blockers:** ________________________________

---

**For detailed scenarios:** See USER-JOURNEY-TEST.md  
**Last Updated:** 2026-02-13
