# Skyline MCP - Cleanup & Documentation Consistency Plan

**Date:** 2026-02-13  
**Context:** After merging skyline-bin and skyline-server into ONE unified binary, we need to clean up outdated references and ensure documentation consistency.

---

## 🎯 Current State (✅ Completed)

### Binary Architecture
- ✅ Deleted `cmd/skyline` (old MCP-only binary)
- ✅ Renamed `cmd/skyline-server` → `cmd/skyline` (unified binary)
- ✅ Added `--transport` flag (stdio/http, default: http)
- ✅ Added `--admin` flag (enable/disable Web UI, default: true)
- ✅ Unified codebase compiles successfully

### Default Behavior
```bash
# Default: HTTP server + Admin UI enabled
skyline

# Equivalent to:
skyline --transport http --admin --bind localhost:19190
```

---

## 🧹 Cleanup Tasks

### 1. Documentation Files - HIGH PRIORITY

#### README.md
**Status:** ⚠️ References old architecture (skyline-bin, skyline-server, stdio transport)

**Changes needed:**
- [ ] Update "Transport Modes" section
  - Remove references to separate skyline-bin/skyline-server binaries
  - Document unified binary with `--transport` and `--admin` flags
  - Mark STDIO mode as "Coming Soon" (placeholder implementation)
  
- [ ] Update "Quick Start" examples
  - Change `./bin/skyline` or `skyline-server` → `skyline`
  - Update systemd service examples
  
- [ ] Update "Admin UI & Profile Management" section
  - Change `./skyline-server` → `skyline`
  
- [ ] Update "Architecture" section
  - Document unified binary approach
  - Remove references to separate binaries
  
- [ ] Update "Building" section
  - `go build -o ./bin/skyline ./cmd/skyline` (single binary)

#### STDIO-MODE.md
**Status:** ⚠️ All examples use `skyline-server --transport stdio`

**Changes needed:**
- [ ] Update all command examples: `skyline-server` → `skyline`
- [ ] Update Claude Desktop config examples
- [ ] Update Cursor config examples
- [ ] Update programmatic usage examples (Node.js spawn)
- [ ] Add note: "STDIO transport is in development. Use HTTP transport for now."

#### CONFIGURATION-GUIDE.md
**Status:** ⚠️ References `skyline-server --bind`

**Changes needed:**
- [ ] Update Web UI startup commands: `skyline-server` → `skyline`
- [ ] Update all CLI examples

#### CODE-EXECUTION.md
**Status:** ⚠️ Likely references old binary names

**Check and update:**
- [ ] Search for `skyline-bin` or `skyline-server`
- [ ] Update to `skyline`

#### Other .md files
**Check these files for outdated references:**
- [ ] CHATGPT-SETUP.md
- [ ] CODE-EXECUTION-DESIGN.md
- [ ] CODE-EXECUTION-DISCOVERY.md
- [ ] MONITORING.md
- [ ] TEST-RESULTS.md
- [ ] TONIGHT-SUMMARY.md
- [ ] IMPLEMENTATION-COMPLETE.md
- [ ] docs/JENKINS-2.545-SUPPORT.md
- [ ] docs/README.md
- [ ] systemd/README.md

### 2. Scripts - MEDIUM PRIORITY

#### scripts/edit-profiles.sh
**Status:** ⚠️ References `skyline-server binary`

**Changes needed:**
- [ ] Line: `# Use skyline-server binary to re-encrypt`
  - Update comment: `# Use skyline binary to re-encrypt`
- [ ] Line: `echo "💡 Tip: Use the Web UI for easier editing: skyline-server --config=config.yaml"`
  - Update: `skyline --config=config.yaml`

### 3. Systemd Files - MEDIUM PRIORITY

#### systemd/skyline.service
**Status:** ❓ May reference old binary

**Check and update:**
- [ ] ExecStart line should use `skyline` (unified binary)

#### systemd/skyline-server.service
**Status:** ⚠️ DEPRECATED - should be removed or merged

**Action:**
- [ ] **DECISION NEEDED:** Delete or merge with skyline.service?
- [ ] If keeping: rename to match unified binary approach
- [ ] Update ExecStart to use `skyline` binary

#### systemd/skyline-service-wrapper.sh
**Status:** ⚠️ References both `skyline-bin` and `skyline-server`

**Action:**
- [ ] **DECISION NEEDED:** Still needed with unified binary?
- [ ] If keeping: update all references to `skyline`
- [ ] Remove logic for separate skyline-bin/skyline-server services

#### systemd/install.sh
**Status:** ⚠️ Complex script with many references

**Changes needed:**
- [ ] Remove `skyline-bin` backup logic
- [ ] Update service file templates to use `skyline`
- [ ] Remove `skyline-server.service` generation (if deprecating)
- [ ] Update all command examples

#### systemd/README.md
**Status:** ❓ Likely references old architecture

**Check and update:**
- [ ] Search for `skyline-bin`, `skyline-server`
- [ ] Update systemd service instructions

### 4. Website Documentation - HIGH PRIORITY

**Files to check (if website repo has docs):**
- [ ] Any setup guides
- [ ] Quick start tutorials
- [ ] API documentation pages
- [ ] Installation guides

**Note:** This may be in the skyline-website repo, not skyline-mcp repo.

### 5. Examples Directory - LOW PRIORITY

**Check these files:**
- [ ] examples/config.yaml.example
- [ ] examples/config.mock.yaml
- [ ] Any shell scripts or docs in examples/

---

## 📝 Documentation Standards

### Binary Name
- ✅ **Use:** `skyline` (unified binary)
- ❌ **Don't use:** `skyline-bin`, `skyline-server`

### Default Flags
When showing the default behavior, you can omit flags:
```bash
# This is enough (uses defaults)
skyline

# But you can be explicit:
skyline --transport http --admin --bind localhost:19190
```

### Transport Mode Examples
```bash
# HTTP + Admin UI (default)
skyline

# HTTP only (no UI)
skyline --transport http --admin=false

# STDIO (coming soon)
skyline --transport stdio
```

### Service Files
Systemd services should use the unified binary:
```ini
[Service]
ExecStart=/usr/local/bin/skyline --bind=localhost:19190 --storage=%h/.skyline/profiles.enc.yaml
```

---

## 🔍 Search & Replace Strategy

### Phase 1: Safe Replacements (Low Risk)
Run these across all .md, .sh, .service files:

```bash
# In documentation and comments
sed -i 's/skyline-server/skyline/g'
sed -i 's/skyline-bin/skyline/g'
sed -i 's/\.\/bin\/skyline-server/skyline/g'
sed -i 's/\.\/bin\/skyline-bin/skyline/g'
```

### Phase 2: Manual Review (High Risk)
These need human verification:
- Systemd service files (ExecStart lines)
- Shell scripts (wrapper logic)
- Config examples (paths)

### Phase 3: Deprecation Decisions
**Need to decide:**
1. Keep `systemd/skyline-server.service` or delete?
2. Keep `systemd/skyline-service-wrapper.sh` or delete?
3. Keep separate systemd files or merge into one?

---

## 🎯 Priority Order

### Must Do (Before v1.1 Release)
1. ✅ Unified binary implementation (DONE)
2. [ ] README.md cleanup (user-facing)
3. [ ] install.sh cleanup (user-facing)
4. [ ] STDIO-MODE.md update (user-facing)
5. [ ] CONFIGURATION-GUIDE.md update (user-facing)

### Should Do (Before Website Update)
6. [ ] Website documentation sync
7. [ ] Example configs update
8. [ ] Other .md files cleanup

### Nice to Have (Before v2.0)
9. [ ] Systemd files refactor/cleanup
10. [ ] Script cleanup (edit-profiles.sh, etc.)
11. [ ] Remove all deprecated files

---

## 🚀 Execution Plan

### Step 1: Update Memory (NOW)
- [x] Update MEMORY.md with current unified binary state
- [x] Create this CLEANUP-PLAN.md for tracking

### Step 2: Documentation Audit (30 min)
- [ ] Run grep search across all files
- [ ] Generate list of all references
- [ ] Categorize by priority

### Step 3: High Priority Updates (1 hour)
- [ ] README.md
- [ ] install.sh
- [ ] STDIO-MODE.md
- [ ] CONFIGURATION-GUIDE.md

### Step 4: Medium Priority Updates (1 hour)
- [ ] Systemd files
- [ ] Scripts
- [ ] Other docs

### Step 5: Testing (30 min)
- [ ] Test install.sh from scratch
- [ ] Verify README examples work
- [ ] Check systemd service starts

### Step 6: Website Sync (30 min)
- [ ] Update skyline-website docs
- [ ] Deploy updated website

---

## ✅ Verification Checklist

Before marking cleanup as complete:

- [ ] No files reference `skyline-bin` (except historical docs)
- [ ] No files reference `skyline-server` as a binary name (except old systemd files marked deprecated)
- [ ] All examples use `skyline` (unified binary)
- [ ] README.md reflects current architecture
- [ ] install.sh builds and installs correctly
- [ ] systemd services work with unified binary
- [ ] Website docs match code reality
- [ ] All transport modes documented correctly (HTTP default, STDIO coming soon)

---

## 📊 Current File Status

```
✅ Done (unified binary code)
└── cmd/skyline/main.go
└── cmd/skyline/ui/

⚠️ Needs Update (references old architecture)
├── README.md (HIGH)
├── STDIO-MODE.md (HIGH)
├── CONFIGURATION-GUIDE.md (HIGH)
├── install.sh (HIGH)
├── systemd/ (MEDIUM)
├── scripts/edit-profiles.sh (MEDIUM)
└── [Other .md files] (LOW)

❓ Unknown (needs audit)
├── CODE-EXECUTION.md
├── CHATGPT-SETUP.md
├── CODE-EXECUTION-DESIGN.md
├── MONITORING.md
├── TEST-RESULTS.md
├── TONIGHT-SUMMARY.md
├── IMPLEMENTATION-COMPLETE.md
├── docs/*.md
└── examples/
```

---

**Next Action:** Start with Step 2 (Documentation Audit) when ready.
