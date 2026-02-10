# Publishing @skyline-mcp/server to npm

This guide walks through publishing the Skyline MCP Node.js client to npm.

## Package Info

- **Package Name:** `@skyline-mcp/server`
- **Current Version:** 0.1.0
- **Scope:** `@skyline-mcp` (scoped package)
- **Registry:** https://www.npmjs.com

---

## Pre-Publishing Checklist

✅ **Package built successfully** (dist/ folder created)  
✅ **LICENSE file added** (MIT)  
✅ **README.md updated** with usage instructions  
✅ **package.json updated** with metadata (author, repo, keywords)  
✅ **.npmignore configured** (only ship dist/, exclude src/)  
✅ **prepublishOnly script** ensures build before publish  

---

## Step-by-Step Publishing

### 1. Create npm Account (if you don't have one)

Go to https://www.npmjs.com/signup and create an account.

### 2. Login to npm CLI

```bash
cd ~/Documents/skyline-mcp/clients/node-mcp-server
npm login
```

You'll be prompted for:
- **Username:** Your npm username
- **Password:** Your npm password
- **Email:** Your npm email (public)
- **OTP:** One-time password (if 2FA enabled)

Verify login:
```bash
npm whoami
```

### 3. Test Package Locally (Optional but Recommended)

Simulate what will be published:
```bash
npm pack --dry-run
```

Or create a local tarball for testing:
```bash
npm pack
# Creates: skyline-mcp-server-0.1.0.tgz
```

Test installing locally:
```bash
npm install -g ./skyline-mcp-server-0.1.0.tgz
skyline-mcp --help
npm uninstall -g @skyline-mcp/server
```

### 4. Publish to npm

**For scoped packages (@skyline-mcp/server), you must specify `--access public` on first publish:**

```bash
npm publish --access public
```

Output should look like:
```
npm notice
npm notice 📦  @skyline-mcp/server@0.1.0
npm notice === Tarball Contents ===
npm notice 1.1kB LICENSE
npm notice 6.4kB README.md
npm notice 157B  dist/index.d.ts
...
npm notice
npm notice Publishing to https://registry.npmjs.org/
+ @skyline-mcp/server@0.1.0
```

### 5. Verify Publication

Check on npm:
```bash
npm view @skyline-mcp/server
```

Visit the package page:
https://www.npmjs.com/package/@skyline-mcp/server

### 6. Test Installation

```bash
# From anywhere
npm install -g @skyline-mcp/server

# Verify
skyline-mcp --help
```

---

## Publishing Updates (Subsequent Versions)

### Update Version

Use semantic versioning (semver):

```bash
# Patch release (0.1.0 → 0.1.1) - Bug fixes
npm version patch

# Minor release (0.1.0 → 0.2.0) - New features (backward compatible)
npm version minor

# Major release (0.1.0 → 1.0.0) - Breaking changes
npm version major
```

This automatically:
1. Updates package.json version
2. Creates a git commit
3. Creates a git tag

### Publish New Version

```bash
npm publish
```

(No need for `--access public` after first publish)

### Push to Git

```bash
git push && git push --tags
```

---

## Package Scope Management

The `@skyline-mcp` scope is controlled by your npm account. To allow others to publish under this scope:

1. Create an npm organization: https://www.npmjs.com/org/create
2. Name it `skyline-mcp`
3. Add team members

Or use your personal scope (automatically created when you publish scoped packages).

---

## Troubleshooting

### "You do not have permission to publish"

You need to create the `@skyline-mcp` organization first, or use your personal scope like `@yourusername/skyline-mcp`.

### "Package name too similar to existing packages"

Change the package name in package.json to something unique.

### "prepublishOnly script failed"

The TypeScript build failed. Check for compilation errors:
```bash
npm run build
```

### "ENEEDAUTH"

You're not logged in:
```bash
npm login
```

---

## Best Practices

### Before Every Publish

1. **Update CHANGELOG.md** (if you have one)
2. **Test the package** locally
3. **Run linting/tests** (if configured)
4. **Update README** with new features
5. **Bump version** appropriately

### Security

- **Never commit credentials** to the repository
- **Enable 2FA** on your npm account
- **Use `.npmignore`** to prevent secrets from being published

### Versioning

Follow semantic versioning:
- **0.x.y** - Initial development (breaking changes OK)
- **1.0.0** - First stable release
- **MAJOR.MINOR.PATCH** - Standard semver after 1.0.0

---

## Quick Reference Commands

```bash
# Build
npm run build

# Test what will be published
npm pack --dry-run

# Login
npm login

# Publish (first time)
npm publish --access public

# Publish (updates)
npm version patch
npm publish
git push && git push --tags

# View package info
npm view @skyline-mcp/server

# Unpublish (within 72 hours, not recommended)
npm unpublish @skyline-mcp/server@0.1.0
```

---

## Package.json Scripts Reference

- `npm run build` - Compile TypeScript to JavaScript
- `npm run dev` - Watch mode (auto-rebuild on changes)
- `npm start` - Run the MCP server (for testing)
- `npm publish` - Publish to npm (runs prepublishOnly → build first)

---

## Post-Publication

After publishing:

1. **Test installation:**
   ```bash
   npx @skyline-mcp/server
   ```

2. **Update main README:**
   - Add installation instructions
   - Link to npm package

3. **Announce:**
   - Share on GitHub Discussions
   - Update project docs
   - Post on social media if applicable

4. **Monitor:**
   - Check npm download stats
   - Watch for issues on GitHub
   - Respond to user feedback

---

**Ready to publish?**

```bash
cd ~/Documents/skyline-mcp/clients/node-mcp-server
npm login
npm publish --access public
```

🚀 Good luck!
