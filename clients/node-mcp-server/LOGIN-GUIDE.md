# npm Login Guide - Step by Step

## Step 1: Do you have an npm account?

### ✅ If YES - Skip to Step 2

### ❌ If NO - Create one now:

**Option A: Web signup (Recommended)**
1. Go to: https://www.npmjs.com/signup
2. Fill in:
   - **Username:** Choose a unique username (letters, numbers, hyphens)
   - **Email:** Your email address (will be public on packages)
   - **Password:** Strong password
3. Verify your email (check inbox)
4. **Optional but recommended:** Enable 2FA in account settings

**Option B: CLI signup**
```bash
npm adduser
# Follow prompts - same info as web signup
```

---

## Step 2: Login via CLI

Run this command:

```bash
cd ~/Documents/skyline-mcp/clients/node-mcp-server
npm login
```

You'll see prompts like this:

```
npm notice Log in on https://registry.npmjs.org/
Login at:
https://www.npmjs.com/login?next=/login/cli/xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx

Press ENTER to open in the browser...
```

**Two login methods available:**

### Method A: Browser Login (Easier)

1. Press **ENTER**
2. Browser opens to npm login page
3. Login with your username/password
4. Click "Sign in"
5. You'll see: "Successfully logged in as [username]"
6. Return to terminal - it will confirm login

### Method B: Terminal Login (Classic)

If browser doesn't open, you'll see prompts:

```
Username: [type your npm username]
Password: [type your npm password - hidden]
Email: (this IS public) [your@email.com]
```

**If you have 2FA enabled:**
```
Enter one-time password: [enter 6-digit code from authenticator app]
```

---

## Step 3: Verify Login

After login, verify you're authenticated:

```bash
npm whoami
```

Should output your npm username:
```
yourusername
```

---

## Step 4: Check Package Access

Verify you can publish scoped packages:

```bash
npm access ls-packages
```

(This might be empty if you haven't published anything yet - that's OK!)

---

## Common Issues

### "Invalid credentials"
- Double-check username and password
- Username is case-sensitive
- Try resetting password at: https://www.npmjs.com/forgot

### "401 Unauthorized" after login
- You're logged in but session expired
- Run `npm logout` then `npm login` again

### "EOTP" (2FA code required but not provided)
- Enter the 6-digit code from your authenticator app
- Make sure your device time is synced correctly

### "ENEEDAUTH"
- Not logged in yet
- Run `npm login`

### Browser doesn't open
- Copy the URL from terminal and open manually
- Or use classic terminal login (choose option when prompted)

---

## Security Tips

### ✅ DO:
- Use a strong, unique password
- Enable 2FA (two-factor authentication)
- Keep your credentials private
- Use environment variables for automation (not credentials in scripts)

### ❌ DON'T:
- Share your npm password
- Commit .npmrc files with tokens
- Disable 2FA for convenience
- Use the same password across services

---

## After Login

Once logged in, you can:

1. **Publish packages:**
   ```bash
   npm publish --access public
   ```

2. **Manage packages:**
   ```bash
   npm unpublish <package>@<version>
   npm deprecate <package>@<version> "message"
   ```

3. **View profile:**
   ```bash
   npm profile get
   ```

4. **Logout (when done):**
   ```bash
   npm logout
   ```

---

## Next Steps

After successful login, you're ready to publish:

```bash
# From the node-mcp-server directory
npm publish --access public
```

---

**Ready?** Run `npm login` and follow the prompts!
