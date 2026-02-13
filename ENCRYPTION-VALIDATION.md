# Encryption Validation & Key Persistence

## Overview
Skyline MCP now includes validation and initialization commands for encrypted profiles, along with automatic key persistence in both systemd environment and shell profiles.

## New CLI Flags

### `--validate [file]`
Validates that an encrypted profiles file can be decrypted with the available key.

**Usage:**
```bash
# Validate default profiles file with key from environment
skyline --validate

# Validate specific file with explicit key
skyline --validate --storage /path/to/profiles.enc.yaml --key <64-char-hex-key>
```

**Exit Codes:**
- `0` = Valid (file exists and can be decrypted)
- `1` = File not found
- `2` = Key missing or invalid format
- `3` = Decryption failed (wrong key or corrupted file)

### `--init-profiles [file]`
Creates a new encrypted profiles file with the provided key.

**Usage:**
```bash
# Generate random key and create profiles file
export SKYLINE_PROFILES_KEY=$(openssl rand -hex 32)
skyline --init-profiles

# Create with explicit key
skyline --init-profiles --key <64-char-hex-key> --storage /path/to/profiles.enc.yaml
```

**Exit Codes:**
- `0` = Success
- `1` = File already exists
- `2` = Key missing or invalid
- `3` = Encryption failed

### `--key <key>`
Provides encryption key directly (overrides `SKYLINE_PROFILES_KEY` env var).

**Format:** 64-character hexadecimal string (32 bytes)

**Example:**
```bash
skyline --validate --key REDACTED_HEX_KEY_1
```

## Install Script Logic

The install script now validates encryption setup **before** starting the systemd service, handling 4 cases:

### Case 1: File exists + Key valid ✅
```bash
skyline --validate
# Exit 0 → Success
```
- Ensures key is persisted in both locations
- Starts service normally

### Case 2: File exists + Key missing/invalid ⚠️
```bash
skyline --validate
# Exit 2 or 3 → Key error
```
- Installs service but **does NOT start it**
- Shows error message with fix instructions
- User must set correct key and manually start service

### Case 3: Key exists + File missing 🔧
```bash
# SKYLINE_PROFILES_KEY set but no profiles.enc.yaml
skyline --init-profiles  # Uses key from env
```
- Creates encrypted file with existing key
- Persists key in both locations
- Starts service

### Case 4: NEITHER exists (fresh install) 🆕
```bash
KEY=$(openssl rand -hex 32)
export SKYLINE_PROFILES_KEY="$KEY"
skyline --init-profiles
```
- Generates random 32-byte key
- Creates encrypted profiles file
- Displays key to user (SAVE IT!)
- Persists key in both locations
- Starts service

## Dual-Location Key Persistence

The install script writes the encryption key to **two locations** to ensure it's available in all contexts:

### 1. Systemd Environment (REQUIRED) ✅
**Location:** `~/.config/environment.d/skyline.conf`

```bash
SKYLINE_PROFILES_KEY=bc06cdd668eefd59...
```

**Why:** User systemd services inherit environment from `environment.d`, NOT from `~/.bashrc` or `~/.zshrc`.

**Permissions:** `chmod 600` (only owner can read/write)

### 2. Shell Profile (convenience) ✅
**Location:** `~/.bashrc` or `~/.zshrc` (if file exists)

```bash
# Skyline MCP encryption key (added 2026-02-13)
export SKYLINE_PROFILES_KEY="bc06cdd668eefd59..."
```

**Why:** Makes key available in interactive shells (for manual `skyline` commands).

**Behavior:** Only adds to profile if file already exists (doesn't create new files).

## Key Security Best Practices

1. **NEVER commit keys to version control**
2. **Store keys in password manager** (1Password, Bitwarden, etc.)
3. **Backup keys securely** before updating systems
4. **Use unique keys per environment** (dev, staging, prod)
5. **Rotate keys periodically** (every 90 days recommended)

## Manual Key Management

### Generate New Key
```bash
openssl rand -hex 32
```

### Set Key for Current Session
```bash
export SKYLINE_PROFILES_KEY=<your-key>
```

### Set Key for Systemd Service
```bash
mkdir -p ~/.config/environment.d
echo "SKYLINE_PROFILES_KEY=<your-key>" > ~/.config/environment.d/skyline.conf
chmod 600 ~/.config/environment.d/skyline.conf
systemctl --user set-environment SKYLINE_PROFILES_KEY=<your-key>
systemctl --user restart skyline
```

### Set Key in Shell Profile (Optional)
```bash
echo 'export SKYLINE_PROFILES_KEY="<your-key>"' >> ~/.bashrc
source ~/.bashrc
```

### Verify Key is Set
```bash
echo $SKYLINE_PROFILES_KEY
systemctl --user show-environment | grep SKYLINE_PROFILES_KEY
```

## Troubleshooting

### Service fails to start with "encryption key required"
**Cause:** Key not available in systemd environment

**Fix:**
```bash
# Check if key is in environment.d
cat ~/.config/environment.d/skyline.conf

# If missing, add it
mkdir -p ~/.config/environment.d
echo "SKYLINE_PROFILES_KEY=<your-key>" > ~/.config/environment.d/skyline.conf
chmod 600 ~/.config/environment.d/skyline.conf

# Reload systemd and restart
systemctl --user daemon-reload
systemctl --user restart skyline
```

### "Decryption failed" error
**Cause:** Wrong key or corrupted file

**Fix Option 1:** Set correct key
```bash
export SKYLINE_PROFILES_KEY=<correct-key>
skyline --validate  # Test if it works
```

**Fix Option 2:** Start fresh (if key is lost)
```bash
rm ~/.skyline/profiles.enc.yaml
skyline --init-profiles  # Will prompt for/generate new key
```

### Key shows in `~/.bashrc` but not in service
**Cause:** Systemd doesn't source shell profiles

**Fix:**
```bash
# Key must be in environment.d, not just .bashrc
cat ~/.bashrc | grep SKYLINE_PROFILES_KEY
# Copy that value to environment.d
echo "SKYLINE_PROFILES_KEY=<value-from-bashrc>" > ~/.config/environment.d/skyline.conf
chmod 600 ~/.config/environment.d/skyline.conf
systemctl --user restart skyline
```

## Help Text

Run `skyline --help` or `skyline -h` to see full usage documentation, including:
- Transport modes (HTTP, STDIO)
- Encryption & profiles management
- Authentication options
- Configuration paths
- Examples

**For manpage generation:** This help text is ready for conversion to manpage format when packaging for Linux distros and macOS DMG.

## Implementation Details

**Files Modified:**
- `cmd/skyline/main.go` - Added --validate, --init-profiles, --key flags + handlers
- `cmd/skyline/main.go` - Added custom `flag.Usage` with comprehensive help text
- `install.sh` - Added validation logic + dual-location key persistence

**Functions Added:**
- `runValidate()` - Validates encrypted profiles file
- `runInitProfiles()` - Creates new encrypted profiles file
- `ensure_key_in_systemd_env()` - Writes key to environment.d
- `ensure_key_in_shell_profile()` - Writes key to shell profile (if exists)

**Exit Codes Standardized:**
All encryption-related commands follow consistent exit code conventions for scriptability.

## Related Documentation
- [STDIO Mode Implementation](STDIO-IMPLEMENTATION.md)
- [HTTP Mode Direct Config](STREAMABLE-HTTP.md)
- [User Journey Testing](USER-JOURNEY-TEST.md)
- [Installation Guide](https://skyline.projex.cc/docs)
