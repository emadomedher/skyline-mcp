# Skyline Systemd Services

Run Skyline as systemd user services with easy management via `skyline service` commands.

## Features

- ✅ Interactive installation
- ✅ Auto-start on boot
- ✅ Auto-restart on failure
- ✅ Simple service management (`skyline service start/stop/restart`)
- ✅ Background operation (survives logout)
- ✅ Clean logs via journalctl

---

## Quick Install

```bash
cd ~/code/skyline-mcp/systemd
./install.sh
```

**The installer will:**
1. Check if skyline is installed
2. Ask if you want to install systemd services
3. Create config directory (`~/.skyline/`)
4. Generate encryption key
5. Install service files
6. Ask if you want to start services now

---

## Service Management

### Check Status
```bash
skyline service status
```

### Start Services
```bash
skyline service start
```

### Stop Services
```bash
skyline service stop
```

### Restart Services
```bash
skyline service restart
```

### Enable Auto-Start (on boot)
```bash
skyline service enable
```

### Disable Auto-Start
```bash
skyline service disable
```

### View Logs
```bash
# Follow skyline logs
skyline service logs

# Follow skyline-server logs
skyline service logs server
```

---

## What Gets Installed

### Services

**skyline.service** - MCP Server
- Port: 8191 (HTTP transport)
- Config: `~/.skyline/config.yaml`

**skyline-server.service** - Web UI
- URL: http://localhost:19190/ui/
- Storage: `~/.skyline/profiles.enc.yaml`

### Files

```
~/.skyline/
├── config.yaml           # MCP server config
├── profiles.enc.yaml     # Encrypted profiles (Web UI)
└── skyline.env          ***REMOVED***

~/.config/systemd/user/
├── skyline.service
└── skyline-server.service

~/.local/bin/  (or /usr/local/bin/)
├── skyline              # Service wrapper script
└── skyline-bin          # Original binary
```

---

## How It Works

The installer creates a wrapper script that intercepts `skyline service` commands:

```bash
skyline service start     # → Handled by wrapper (systemctl)
skyline --version         # → Passed to real binary (skyline-bin)
skyline --config=...      # → Passed to real binary
```

All normal skyline commands work as before!

---

## Configuration

### MCP Server Config

Edit: `~/.skyline/config.yaml`

```yaml
apis:
  - name: github
    spec_url: https://raw.githubusercontent.com/github/rest-api-description/main/descriptions/api.github.com/api.github.com.yaml
    auth:
      type: bearer
      token: ${GITHUB_TOKEN}
```

After editing:
```bash
skyline service restart
```

### Web UI (Recommended)

Open: http://localhost:19190/ui/

Profiles are encrypted automatically with the key in `~/.skyline/skyline.env`

---

## Troubleshooting

### Service Won't Start

```bash
# Check logs
skyline service logs
skyline service logs server

# Verify binary
which skyline
skyline --version

# Check systemd status
systemctl --user status skyline
systemctl --user status skyline-server
```

### Port Already in Use

```bash
# Check what's using port 8191
ss -tlnp | grep 8191

# Check what's using port 19190
ss -tlnp | grep 19190

# Kill old process if needed
kill <pid>
skyline service restart
```

### Can't Connect to Web UI

```bash
# Verify service is running
skyline service status

# Test connection
curl http://localhost:19190/healthz

# Check logs
skyline service logs server
```

### Services Don't Start on Boot

```bash
# Enable lingering (services start without login)
loginctl enable-linger $USER

# Verify
loginctl show-user $USER | grep Linger
# Should show: Linger=yes

# Enable services
skyline service enable
```

---

## Advanced Usage

### Manual systemctl Commands

You can still use systemctl directly:

```bash
systemctl --user status skyline
systemctl --user restart skyline
journalctl --user -u skyline -f
```

### Edit Service Files

```bash
# Edit skyline service
nano ~/.config/systemd/user/skyline.service

# Reload after editing
systemctl --user daemon-reload
skyline service restart
```

### Custom Port for MCP Server

Edit `~/.config/systemd/user/skyline.service`:

```ini
ExecStart=%h/.local/bin/skyline-bin --config=%h/.skyline/config.yaml --transport=http --listen=:9999
```

Then:
```bash
systemctl --user daemon-reload
skyline service restart
```

---

## Uninstall

### Stop and Disable Services

```bash
skyline service stop
skyline service disable
```

### Remove Service Files

```bash
rm ~/.config/systemd/user/skyline.service
rm ~/.config/systemd/user/skyline-server.service
systemctl --user daemon-reload
```

### Remove Wrapper (Restore Original Binary)

```bash
# If skyline-bin exists
if [ -f ~/.local/bin/skyline-bin ]; then
    mv ~/.local/bin/skyline-bin ~/.local/bin/skyline
fi
```

### Optional: Remove Data

```bash
# ⚠️  This deletes your config and profiles!
rm -rf ~/.skyline
```

---

## Comparison

### Before (Manual)
```bash
skyline --config config.yaml
# ❌ Blocks terminal
# ❌ Stops when you log out
# ❌ No auto-restart
```

### After (Systemd Service)
```bash
skyline service start
# ✅ Runs in background
# ✅ Survives logout
# ✅ Auto-restarts on failure
# ✅ Auto-starts on boot
# ✅ Easy management
```

---

## Examples

### First Time Setup

```bash
# 1. Install skyline binary
curl -fsSL https://skyline.projex.cc/install | bash

# 2. Install as service (interactive)
cd ~/code/skyline-mcp/systemd
./install.sh
# Answer: y (install services)
# Answer: y (start now)

# 3. Configure via Web UI
# Open: http://localhost:19190/ui/

# 4. Check status
skyline service status
```

### Daily Usage

```bash
# Check status
skyline service status

# View logs
skyline service logs

# Restart after config changes
skyline service restart
```

### Update Skyline

```bash
# 1. Update binary
skyline update

# 2. Restart services
skyline service restart

# 3. Verify new version
skyline --version
```

---

## Like OpenClaw

Both use systemd user services:

```bash
# OpenClaw
systemctl --user status openclaw

# Skyline
skyline service status
```

**Same reliability, same convenience!** 🚀

---

## Support

- **Docs:** https://skyline.projex.cc/docs
- **Source:** https://github.com/emadomedher/skyline-mcp
- **Issues:** https://github.com/emadomedher/skyline-mcp/issues
