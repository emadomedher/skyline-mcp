# Skyline Systemd Services

Run Skyline as a systemd user service (like OpenClaw).

## Quick Install

```bash
cd systemd
./install.sh
```

This will:
- Create `~/.skyline/` directory
- Generate encryption key
- Install systemd service files
- Create default config

## Services

### skyline.service
**MCP Server** - Exposes APIs as MCP tools

- **Port:** 8191 (HTTP transport)
- **Config:** `~/.skyline/config.yaml`
- **Logs:** `journalctl --user -u skyline -f`

### skyline-server.service
**Web UI & Profile Server** - Manage API profiles

- **URL:** http://localhost:19190/ui/
- **Storage:** `~/.skyline/profiles.enc.yaml`
- **Key:** `~/.skyline/skyline.env`
- **Logs:** `journalctl --user -u skyline-server -f`

## Usage

### Enable & Start
```bash
# Start Web UI
systemctl --user enable --now skyline-server

# Start MCP Server
systemctl --user enable --now skyline
```

### Check Status
```bash
systemctl --user status skyline
systemctl --user status skyline-server
```

### View Logs
```bash
# Follow logs
journalctl --user -u skyline -f
journalctl --user -u skyline-server -f

# Last 50 lines
journalctl --user -u skyline -n 50
```

### Restart
```bash
systemctl --user restart skyline
systemctl --user restart skyline-server
```

### Stop
```bash
systemctl --user stop skyline
systemctl --user stop skyline-server
```

### Disable
```bash
systemctl --user disable skyline
systemctl --user disable skyline-server
```

## Configuration

### MCP Server Config
Edit: `~/.skyline/config.yaml`

```yaml
apis:
  - name: example-api
    spec_url: https://api.example.com/openapi.json
    auth:
      type: bearer
      token: ${API_TOKEN}
```

After editing:
```bash
systemctl --user restart skyline
```

### Web UI (Profiles)
**Recommended:** Use the Web UI at http://localhost:19190/ui/

Or edit manually:
1. Export encryption key: `export SKYLINE_PROFILES_KEY=$(grep SKYLINE_PROFILES_KEY ~/.skyline/skyline.env | cut -d= -f2)`
2. Edit profiles (they're encrypted)
3. Restart: `systemctl --user restart skyline-server`

## Environment Variables

Stored in: `~/.skyline/skyline.env`

```bash
SKYLINE_PROFILES_KEY=<32-byte-hex-key>
```

To view:
```bash
cat ~/.skyline/skyline.env
```

## Directory Structure

```
~/.skyline/
├── config.yaml           # MCP server config
├── profiles.enc.yaml     # Encrypted profiles (Web UI)
└── skyline.env          ***REMOVED***

~/.config/systemd/user/
├── skyline.service
└── skyline-server.service
```

## Troubleshooting

### Service won't start
```bash
# Check logs for errors
journalctl --user -u skyline -n 50
journalctl --user -u skyline-server -n 50

# Verify binary exists
which skyline
which skyline-server

# Test manually
/usr/local/bin/skyline --version
/usr/local/bin/skyline-server --version
```

### Can't connect to Web UI
```bash
# Check if service is running
systemctl --user status skyline-server

# Check if port is listening
ss -tlnp | grep 19190

# Check logs
journalctl --user -u skyline-server -f
```

### MCP Server not responding
```bash
# Check status
systemctl --user status skyline

# Test connection
curl http://localhost:8191/health

# Check logs
journalctl --user -u skyline -f
```

### Update after system reboot
```bash
# Enable lingering (services start without login)
loginctl enable-linger $USER

# Verify
loginctl show-user $USER | grep Linger
```

## Uninstall

```bash
# Stop and disable services
systemctl --user disable --now skyline
systemctl --user disable --now skyline-server

# Remove service files
rm ~/.config/systemd/user/skyline.service
rm ~/.config/systemd/user/skyline-server.service

# Reload systemd
systemctl --user daemon-reload

# Optional: Remove data (careful!)
# rm -rf ~/.skyline
```

## Comparison with Manual Run

### Manual (foreground)
```bash
skyline --config config.yaml
# Blocks terminal, stops when you log out
```

### Systemd (background)
```bash
systemctl --user start skyline
# Runs in background, survives logout, auto-restarts
```

## Auto-start on Boot

Services start automatically after installation. To prevent:
```bash
systemctl --user disable skyline
systemctl --user disable skyline-server
```

To re-enable:
```bash
systemctl --user enable skyline
systemctl --user enable skyline-server
```

## Like OpenClaw

Both Skyline and OpenClaw use systemd user services:

```bash
# OpenClaw
systemctl --user status openclaw

# Skyline
systemctl --user status skyline
systemctl --user status skyline-server
```

Same commands, same behavior, same reliability! 🚀
