# Skyline MCP - Documentation Audit Report

**Date:** 2026-02-13  
**Total References Found:** 99  
**Search Command:** `grep -r "skyline-bin\|skyline-server" --include="*.md" --include="*.sh" --include="*.service"`

---

## Summary by File Type

| Category | Files | References | Priority |
|----------|-------|------------|----------|
| Scripts | 5 | ~30 | HIGH |
| Systemd | 4 | ~35 | MEDIUM |
| Documentation | 7 | ~30 | HIGH |
| CI/CD | 1 | 4 | MEDIUM |
| Other | 2 | ~5 | LOW |

---

## 🔴 HIGH PRIORITY - User-Facing Documentation

### STDIO-MODE.md (23 references)
**Impact:** HIGH - User-facing documentation for Claude Desktop integration

**Lines with issues:**
- Line 35: `./skyline-server --config config.yaml --transport stdio`
- Line 44: `./skyline-server --config config.yaml --transport stdio`
- Line 50: `./skyline-server --config my-config.yaml --transport stdio`
- Line 67: `"command": "/path/to/skyline-server"`
- Line 83: `command = "/path/to/skyline-server"`
- Line 98: `const serverProcess = spawn("/path/to/skyline-server"`
- Lines 134, 167, 185, 194, 270, 336, 343, 351, 363, 398, 417, 429, 432, 438, 567, 600, 604

**Fix Strategy:**
- Global replace: `skyline-server` → `skyline`
- Add warning: "STDIO transport is in development"

### CONFIGURATION-GUIDE.md (5 references)
**Impact:** HIGH - Web UI setup instructions

**Lines with issues:**
- Line 19: `./skyline-server --config=config.yaml --bind=localhost:19190`
- Line 308: `# Required for skyline-server`
- Line 328: `CMD ["skyline-server", "--config=/app/config.yaml"]`
- Line 345: `kubectl create deployment skyline-server \`
- Line 421: `./skyline-server --config=config.yaml`

**Fix Strategy:**
- Replace `skyline-server` → `skyline`
- Update Kubernetes deployment name (keep as `skyline-server` or rename?)

### STREAMABLE-HTTP.md (8 references)
**Impact:** MEDIUM - HTTP transport documentation

**Lines with issues:**
- Lines 37, 40, 44, 161, 171, 181, 365, 370

**Fix Strategy:**
- Replace `./skyline-server` → `skyline`

### CHANGELOG.md (1 reference)
**Impact:** LOW - Historical record

**Line 21:**
- `- **Web UI**: Profile management and API testing interface (skyline-server)`

**Fix Strategy:**
- Keep as historical reference (accurate for that version)
- Or update to clarify it's now unified

### IMPLEMENTATION-COMPLETE.md (3 references)
**Impact:** LOW - Implementation notes

**Lines with issues:**
- Lines 241, 242, 380

**Fix Strategy:**
- Replace `skyline-server` → `skyline`

### MONITORING.md (1 reference)
**Impact:** LOW - Development monitoring guide

**Line 21:**
- `go run ./cmd/skyline-server --listen :9190`

**Fix Strategy:**
- Update to `go run ./cmd/skyline --listen :9190`

---

## 🟡 MEDIUM PRIORITY - Scripts & Systemd

### scripts/edit-profiles.sh (2 references)
**Impact:** MEDIUM - Profile editing utility

**Lines with issues:**
- Line 205: `# Use skyline-server binary to re-encrypt (most reliable)`
- Line 283: `echo "💡 Tip: Use the Web UI for easier editing: skyline-server --config=config.yaml"`

**Fix Strategy:**
```bash
# Line 205
- # Use skyline-server binary to re-encrypt (most reliable)
+ # Use skyline binary to re-encrypt (most reliable)

# Line 283
- skyline-server --config=config.yaml
+ skyline --config=config.yaml
```

### systemd/skyline-server.service (1 reference)
**Impact:** MEDIUM - Systemd service file

**Line 9:**
- `ExecStart=%h/.local/bin/skyline-server --listen=localhost:19190 --storage=%h/.skyline/profiles.enc.yaml`

**DECISION NEEDED:**
- Option 1: Delete this file (deprecated with unified binary)
- Option 2: Rename to match unified binary
- Option 3: Keep for backward compatibility, update ExecStart

**Recommended:** Delete and use only skyline.service

### systemd/skyline-service-wrapper.sh (10 references)
**Impact:** MEDIUM - Wrapper script for service management

**Lines with issues:**
- Line 5: `REAL_BINARY="$(dirname "$0")/skyline-bin"`
- Lines 18, 23, 26, 31, 37, 40, 45, 51, 56, 57: References to `skyline-server` service

**DECISION NEEDED:**
- Is this wrapper still needed with unified binary?
- If yes: update all references to use `skyline` binary
- If no: delete this file

**Recommended:** Delete - unified binary has service management built-in

### systemd/install.sh (19 references)
**Impact:** MEDIUM - Systemd installation script (DIFFERENT from main install.sh)

**Lines with issues:**
- Lines 82-84: `skyline-bin` backup logic
- Line 95: `REAL_BINARY="$(dirname "$0")/skyline-bin"`
- Lines 107-145: Service management for `skyline-server`
- Line 186: `ExecStart=$SKYLINE_DIR/skyline-bin`
- Lines 203-213: `skyline-server.service` generation

**DECISION NEEDED:**
- Is systemd/install.sh still used?
- Main install.sh already handles systemd services
- Merge or delete?

**Recommended:** Delete - main install.sh is canonical

### systemd/README.md (10 references)
**Impact:** MEDIUM - Systemd documentation

**Lines with issues:**
- Lines 70, 84, 98, 102, 113, 164, 238, 262, 269, 270, 271

**Fix Strategy:**
- If keeping systemd directory: update all references
- If deleting systemd directory: remove file

### start-webui.sh (5 references)
**Impact:** LOW - Dev helper script

**Lines with issues:**
- Lines 15, 16, 20, 26, 30, 31

**Fix Strategy:**
- Replace `skyline-server` → `skyline`
- Update process name for pkill

### start-skyline.sh (1 reference)
**Impact:** LOW - Dev helper script

**Line 12:**
- `./bin/skyline-server --listen :9190 --auth-mode none "$@"`

**Fix Strategy:**
- Replace `./bin/skyline-server` → `./bin/skyline`

---

## 🟢 LOW PRIORITY - CI/CD & Kubernetes

### .github/workflows/release.yml (4 references)
**Impact:** MEDIUM - Release builds

**Lines with issues:**
- Line 75: `- name: Build skyline-server binary`
- Line 81: `go build ... -o skyline-server-${{ matrix.os }}-${{ matrix.arch }} ./cmd/skyline-server`
- Line 89: `skyline-server-${{ matrix.os }}-${{ matrix.arch }}`
- Line 147: `- Both 'skyline' and 'skyline-server'`

**Fix Strategy:**
```yaml
# Line 75
- name: Build skyline binary

# Line 81
go build ... -o skyline-${{ matrix.os }}-${{ matrix.arch }} ./cmd/skyline

# Line 89
skyline-${{ matrix.os }}-${{ matrix.arch }}

# Line 147
- 'skyline' binary with configurable transport modes
```

### k8s/skyline-prebuilt.yaml (4 references)
**Impact:** LOW - Kubernetes deployment example

**Lines with issues:**
- Lines 20, 32, 33, 58

**Fix Strategy:**
- Update paths to use `skyline` instead of `skyline-bin`

---

## 📋 File-by-File Action Plan

### Delete These Files (Deprecated)
```
systemd/skyline-server.service      # Use skyline.service instead
systemd/skyline-service-wrapper.sh  # Unified binary has service cmds
systemd/install.sh                  # Main install.sh is canonical
systemd/README.md                   # Outdated systemd docs
```

### Update These Files (Keep)
```
✅ HIGH PRIORITY (User-Facing)
├── STDIO-MODE.md (23 refs) → Global replace skyline-server → skyline
├── CONFIGURATION-GUIDE.md (5 refs) → Update Web UI examples
├── STREAMABLE-HTTP.md (8 refs) → Update HTTP examples
└── README.md (audit needed - not in grep results but likely has refs)

🟡 MEDIUM PRIORITY (Dev Tools)
├── scripts/edit-profiles.sh (2 refs) → Update binary name + tip
├── start-webui.sh (5 refs) → Update process name
├── start-skyline.sh (1 ref) → Update binary path
└── k8s/skyline-prebuilt.yaml (4 refs) → Update paths

🟢 LOW PRIORITY (Historical)
├── IMPLEMENTATION-COMPLETE.md (3 refs) → Update for accuracy
├── MONITORING.md (1 ref) → Update cmd path
└── CHANGELOG.md (1 ref) → Keep as historical or clarify

⚠️ CRITICAL (CI/CD)
└── .github/workflows/release.yml (4 refs) → Update build targets
```

### Files Already Correct
```
✅ Main install.sh → Already builds cmd/skyline (correct)
✅ uninstall.sh → Already removes skyline binary (correct)
✅ cmd/skyline/ → Unified binary (correct)
✅ systemd/skyline.service → Uses skyline binary (needs verification)
```

---

## 🎯 Recommended Execution Order

### Phase 1: Critical User-Facing (Do First)
1. [ ] Update STDIO-MODE.md (23 fixes)
2. [ ] Update CONFIGURATION-GUIDE.md (5 fixes)
3. [ ] Update README.md (audit + fix)
4. [ ] Update STREAMABLE-HTTP.md (8 fixes)

### Phase 2: CI/CD (Blocks Releases)
5. [ ] Update .github/workflows/release.yml (4 fixes)
6. [ ] Test release build process

### Phase 3: Dev Scripts (Quality of Life)
7. [ ] Update scripts/edit-profiles.sh (2 fixes)
8. [ ] Update start-webui.sh (5 fixes)
9. [ ] Update start-skyline.sh (1 fix)

### Phase 4: Deprecation (Cleanup)
10. [ ] Delete systemd/skyline-server.service
11. [ ] Delete systemd/skyline-service-wrapper.sh
12. [ ] Delete systemd/install.sh (or mark deprecated)
13. [ ] Delete systemd/README.md (or rewrite for new arch)

### Phase 5: Historical Docs (Optional)
14. [ ] Update IMPLEMENTATION-COMPLETE.md (3 fixes)
15. [ ] Update MONITORING.md (1 fix)
16. [ ] Update k8s/skyline-prebuilt.yaml (4 fixes)
17. [ ] Decide on CHANGELOG.md (keep historical or update)

---

## 🔍 Quick Fixes (Automated)

### Safe Global Replacements
Run these in files marked as "safe":

```bash
# STDIO-MODE.md
sed -i 's/skyline-server/skyline/g' STDIO-MODE.md

# CONFIGURATION-GUIDE.md
sed -i 's/skyline-server/skyline/g' CONFIGURATION-GUIDE.md

# STREAMABLE-HTTP.md
sed -i 's/skyline-server/skyline/g' STREAMABLE-HTTP.md

# scripts/edit-profiles.sh
sed -i 's/skyline-server/skyline/g' scripts/edit-profiles.sh

# start-webui.sh
sed -i 's/skyline-server/skyline/g' start-webui.sh

# start-skyline.sh
sed -i 's/skyline-server/skyline/g' start-skyline.sh
```

### Manual Review Required
These need human verification:
- .github/workflows/release.yml (build targets)
- k8s/skyline-prebuilt.yaml (deployment config)
- README.md (complex structure)

---

## ✅ Verification Checklist

After cleanup, verify:

- [ ] No user-facing docs reference `skyline-server` (except historical notes)
- [ ] No user-facing docs reference `skyline-bin`
- [ ] CI/CD builds only `skyline` binary
- [ ] Install script builds only `cmd/skyline`
- [ ] Systemd service uses `skyline` binary
- [ ] README.md transport section reflects unified binary
- [ ] All example commands use `skyline`

---

**Next Action:** Start Phase 1 (Critical User-Facing) when ready.
