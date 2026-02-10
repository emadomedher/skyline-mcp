# GitHub Release Publishing Guide

**No npm required!** This guide shows how to publish the Node MCP client via GitHub Releases.

---

## How It Works

1. **Push a git tag** → Triggers GitHub Actions
2. **GitHub Actions builds** the package automatically
3. **Creates a Release** with downloadable `.tgz` file
4. **Users install** directly from GitHub

---

## Publishing a New Release (3 Commands)

```bash
# 1. Commit your changes (if any)
cd ~/Documents/skyline-mcp
git add clients/node-mcp-server/
git commit -m "Update Node MCP client to v0.1.0"

# 2. Create and push a tag
git tag node-client-v0.1.0
git push origin main
git push origin node-client-v0.1.0

# 3. Done! GitHub Actions does the rest
```

**That's it!** In ~2 minutes, check:
https://github.com/emadomedher/skyline-mcp/releases

---

## Tag Naming Convention

Use this format: `node-client-vX.Y.Z`

Examples:
- `node-client-v0.1.0` - First release
- `node-client-v0.1.1` - Bug fix
- `node-client-v0.2.0` - New features
- `node-client-v1.0.0` - Stable release

---

## What GitHub Actions Does

When you push a tag, the workflow:

1. ✅ Checks out the code
2. ✅ Installs Node.js 18
3. ✅ Runs `npm ci` (clean install)
4. ✅ Runs `npm run build` (compiles TypeScript)
5. ✅ Runs `npm pack` (creates tarball)
6. ✅ Creates GitHub Release with the tarball attached
7. ✅ Uploads artifacts (kept for 90 days)

**Build time:** ~2 minutes

---

## How Users Install

### Option 1: Direct URL (Recommended)

```bash
npm install https://github.com/emadomedher/skyline-mcp/releases/download/node-client-v0.1.0/emadomedher-skyline-mcp-server-0.1.0.tgz
```

### Option 2: Global Installation

```bash
npm install -g https://github.com/emadomedher/skyline-mcp/releases/download/node-client-v0.1.0/emadomedher-skyline-mcp-server-0.1.0.tgz
skyline-mcp
```

### Option 3: Download Manually

1. Go to: https://github.com/emadomedher/skyline-mcp/releases
2. Download `emadomedher-skyline-mcp-server-X.Y.Z.tgz`
3. Install: `npm install ./emadomedher-skyline-mcp-server-X.Y.Z.tgz`

---

## Updating the Package

### For Bug Fixes (Patch Version)

```bash
# Update package.json version
cd ~/Documents/skyline-mcp/clients/node-mcp-server
npm version patch  # 0.1.0 → 0.1.1

# Commit, tag, and push
cd ~/Documents/skyline-mcp
git add clients/node-mcp-server/package.json
git commit -m "Bump Node MCP client to v0.1.1"
git tag node-client-v0.1.1
git push origin main
git push origin node-client-v0.1.1
```

### For New Features (Minor Version)

```bash
cd ~/Documents/skyline-mcp/clients/node-mcp-server
npm version minor  # 0.1.0 → 0.2.0

cd ~/Documents/skyline-mcp
git add clients/node-mcp-server/package.json
git commit -m "Bump Node MCP client to v0.2.0"
git tag node-client-v0.2.0
git push origin main
git push origin node-client-v0.2.0
```

### For Breaking Changes (Major Version)

```bash
cd ~/Documents/skyline-mcp/clients/node-mcp-server
npm version major  # 0.1.0 → 1.0.0

cd ~/Documents/skyline-mcp
git add clients/node-mcp-server/package.json
git commit -m "Bump Node MCP client to v1.0.0"
git tag node-client-v1.0.0
git push origin main
git push origin node-client-v1.0.0
```

---

## Monitoring Releases

### Check Build Status

Go to: https://github.com/emadomedher/skyline-mcp/actions

- ✅ Green checkmark = Build succeeded
- ❌ Red X = Build failed (check logs)

### View Releases

Go to: https://github.com/emadomedher/skyline-mcp/releases

You'll see:
- Release title
- Tag name
- Downloadable `.tgz` file
- Installation instructions
- Changelog

---

## Troubleshooting

### "Workflow not found"

Make sure you pushed the `.github/workflows/release-node-client.yml` file:

```bash
cd ~/Documents/skyline-mcp
git add .github/workflows/release-node-client.yml
git commit -m "Add GitHub Actions release workflow"
git push origin main
```

### "Permission denied" during release

Check that GitHub Actions has write permissions:
1. Go to: https://github.com/emadomedher/skyline-mcp/settings/actions
2. Under "Workflow permissions", select "Read and write permissions"
3. Save

### Build failed

Click on the failed workflow run to see logs:
https://github.com/emadomedher/skyline-mcp/actions

Common issues:
- TypeScript compilation errors → Fix in `clients/node-mcp-server/src/`
- Missing dependencies → Update `package.json`

---

## Advantages Over npm

✅ **No authentication hassles** - Uses GitHub token automatically  
✅ **No 2FA required** - GitHub handles security  
✅ **Full control** - Own your distribution  
✅ **Free hosting** - GitHub releases are free  
✅ **Automated builds** - CI/CD does the work  
✅ **Transparent** - Users see source code + builds  

---

## Optional: Add npm as Backup Later

Once GitHub Releases is working, you can always publish to npm later:

```bash
npm login
npm publish --access public --otp=XXXXXX
```

But **GitHub Releases is enough** for most users!

---

## Quick Reference

```bash
# Publish new release
git tag node-client-v0.1.0
git push origin node-client-v0.1.0

# Update version and release
npm version patch  # or minor, or major
git push origin main
git push origin node-client-v$(node -p "require('./package.json').version")

# Check releases
open https://github.com/emadomedher/skyline-mcp/releases

# Check build status
open https://github.com/emadomedher/skyline-mcp/actions
```

---

**Ready to publish?**

```bash
cd ~/Documents/skyline-mcp
git add .github/workflows/release-node-client.yml GITHUB-RELEASE-GUIDE.md
git commit -m "Add GitHub Actions release workflow for Node MCP client"
git push origin main

# Create first release
git tag node-client-v0.1.0
git push origin node-client-v0.1.0
```

Then watch the magic happen at:
https://github.com/emadomedher/skyline-mcp/actions

🚀 **No npm headaches!**
