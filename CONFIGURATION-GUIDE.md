# Skyline Configuration Guide

Complete guide to configuring Skyline MCP for secure API integration.

## 🌐 Recommended: Web UI (Easy & Secure)

**The Web UI is the primary and recommended way to configure Skyline.** All profiles are automatically encrypted with AES-256-GCM.

### Quick Start

```bash
# 1. Generate encryption key (first time only)
openssl rand -hex 32 > .encryption-key

# 2. Export the key
export CONFIG_SERVER_KEY=$(cat .encryption-key)

# 3. Start Web UI
./skyline-server --config=config.yaml --listen=:9190

# 4. Open browser
# http://localhost:9190/ui/
```

### Web UI Features

- ✅ **Automatic encryption** - All profiles encrypted at rest
- ✅ **Visual API testing** - Test endpoints before saving
- ✅ **Syntax validation** - Catch errors before they break
- ✅ **Auth management** - Secure credential storage
- ✅ **Zero CLI knowledge** - Point, click, done

### Security

The Web UI encrypts profiles using:
- **Algorithm:** AES-256-GCM (Galois/Counter Mode)
- **Key size:** 256 bits (32 bytes, 64 hex chars)
- **Authentication:** Built-in MAC for tamper detection
- **Nonce:** Random 96-bit nonce per encryption

**Stored format:**
```json
{
  "version": 1,
  "nonce": "base64_encoded_random_nonce",
  "ciphertext": "base64_encoded_encrypted_data"
}
```

---

## 🔧 Advanced: CLI Configuration (Tech-Savvy Users Only)

For users comfortable with command-line tools and encryption.

### Prerequisites

```bash
# Install required tools
sudo apt install jq openssl  # Debian/Ubuntu
brew install jq openssl      # macOS
```

### Method 1: Helper Script (Easiest)

```bash
# Edit profiles (decrypt → editor → re-encrypt)
./scripts/edit-profiles.sh

# View only (no save)
./scripts/edit-profiles.sh --view

# Custom key file
./scripts/edit-profiles.sh --key-file=/path/to/key
```

**What it does:**
1. Decrypts `profiles.enc.yaml` using your `.encryption-key`
2. Opens in `$EDITOR` (defaults to nano)
3. Re-encrypts on save
4. Validates encryption before writing

### Method 2: Manual Decryption

#### Step 1: Decrypt

```bash
# Load encryption key
KEY=$(cat .encryption-key)

# Decrypt profiles
cat > /tmp/decrypt.go << 'EOF'
package main
import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
)
type envelope struct {
	Version    int
	Nonce      string
	Ciphertext string
}
func main() {
	keyBytes, _ := hex.DecodeString(os.Args[1])
	data, _ := os.ReadFile("profiles.enc.yaml")
	var env envelope
	json.Unmarshal(data, &env)
	nonce, _ := base64.StdEncoding.DecodeString(env.Nonce)
	ciphertext, _ := base64.StdEncoding.DecodeString(env.Ciphertext)
	block, _ := aes.NewCipher(keyBytes)
	gcm, _ := cipher.NewGCM(block)
	plain, _ := gcm.Open(nil, nonce, ciphertext, nil)
	fmt.Print(string(plain))
}
EOF

go run /tmp/decrypt.go "$KEY" > profiles.dec.yaml
```

#### Step 2: Edit

```bash
nano profiles.dec.yaml
```

#### Step 3: Re-encrypt

```bash
cat > /tmp/encrypt.go << 'EOF'
package main
import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
)
type envelope struct {
	Version    int    `json:"version"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}
func main() {
	keyBytes, _ := hex.DecodeString(os.Args[1])
	plain, _ := os.ReadFile("profiles.dec.yaml")
	block, _ := aes.NewCipher(keyBytes)
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	rand.Read(nonce)
	ciphertext := gcm.Seal(nil, nonce, plain, nil)
	env := envelope{
		Version:    1,
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	}
	data, _ := json.MarshalIndent(env, "", "  ")
	fmt.Print(string(data))
}
EOF

go run /tmp/encrypt.go "$KEY" > profiles.enc.yaml

# Cleanup
rm profiles.dec.yaml /tmp/{encrypt,decrypt}.go
```

### Method 3: Direct nano with Process Substitution

The "old-school" way (what you were remembering):

```bash
# Decrypt on open, re-encrypt on save
nano <(go run /tmp/decrypt.go "$(cat .encryption-key)" < profiles.enc.yaml)
```

**⚠️ Warning:** This creates a temporary decrypted file that persists until process ends. Not recommended for production.

---

## 📁 Configuration Files

### Encryption Key (`.encryption-key`)

```bash
# Generate once
openssl rand -hex 32 > .encryption-key

# Protect it
chmod 600 .encryption-key

# Never commit to git
echo ".encryption-key" >> .gitignore
```

**Format:** 64 hexadecimal characters (32 bytes)
**Example:** `a72847b98e92388e1d85c8bf2dbd479cdb069278ae4372600bfa94a6640fe402`

### Encrypted Profiles (`profiles.enc.yaml`)

**Location:** `./profiles.enc.yaml`
**Format:** JSON envelope with encrypted YAML inside
**Encryption:** AES-256-GCM with random nonce per save

**Example structure (decrypted):**
```yaml
version: 1
profiles:
  my-api:
    name: my-api
    token: secret-token-here
    apis:
      - name: users-api
        spec_url: https://api.example.com/openapi.json
        auth:
          type: bearer
          token: ${token}
```

### Main Config (`config.yaml`)

**Location:** `./config.yaml`
**Format:** YAML (unencrypted, no secrets)
**Purpose:** Global settings, logging, code execution

**Example:**
```yaml
enable_code_execution: true
enable_crud_grouping: true

log:
  level: info
  format: text

# No secrets here! Use Web UI profiles instead
```

---

## 🔐 Security Best Practices

### ✅ DO

1. **Always use the Web UI** for profile management (easier + safer)
2. **Generate strong keys**: `openssl rand -hex 32`
3. **Protect your key file**: `chmod 600 .encryption-key`
4. **Use environment variables**: `export CONFIG_SERVER_KEY=$(cat .encryption-key)`
5. **Add to .gitignore**: `.encryption-key` and `profiles.dec.yaml`
6. **Rotate keys periodically** (re-encrypt with new key)
7. **Use different keys** for different environments (dev/staging/prod)

### ❌ DON'T

1. **Don't store keys in config files** - Use environment variables
2. **Don't commit encrypted profiles** without changing keys after
3. **Don't share keys in chat/email** - Use secure channels only
4. **Don't use weak keys** - Always 32 random bytes minimum
5. **Don't leave decrypted files** lying around (`profiles.dec.yaml`)
6. **Don't use the same key** across multiple projects

---

## 🚀 Production Deployment

### Environment Variables

```bash
# Required for skyline-server
export CONFIG_SERVER_KEY=$(cat /secure/path/.encryption-key)

# Optional for skyline CLI with config server
export CONFIG_SERVER_URL=http://localhost:9190
export PROFILE_NAME=production-api
export PROFILE_TOKEN=your-profile-token
```

### Docker

```dockerfile
FROM skyline:latest

# Copy encrypted profiles (key via env var)
COPY profiles.enc.yaml /app/profiles.enc.yaml

# Key passed at runtime
ENV CONFIG_SERVER_KEY=""

CMD ["skyline-server", "--config=/app/config.yaml"]
```

```bash
# Run with key from file
docker run -e CONFIG_SERVER_KEY="$(cat .encryption-key)" skyline:latest
```

### Kubernetes Secret

```bash
# Create secret from key file
kubectl create secret generic skyline-key \
  --from-file=key=.encryption-key \
  --namespace=skyline

# Use in deployment
kubectl create deployment skyline-server \
  --image=skyline:latest \
  --env="CONFIG_SERVER_KEY=$(kubectl get secret skyline-key -o jsonpath='{.data.key}' | base64 -d)"
```

---

## 🆘 Troubleshooting

### "Missing encryption key"

```bash
# Generate new key
openssl rand -hex 32 > .encryption-key

# Set environment variable
export CONFIG_SERVER_KEY=$(cat .encryption-key)
```

### "Invalid key length"

Key must be exactly **64 hexadecimal characters** (32 bytes).

```bash
# Check key length
wc -c < .encryption-key  # Should output: 65 (64 chars + newline)

# Regenerate if wrong
openssl rand -hex 32 > .encryption-key
```

### "Decrypt failed"

1. **Wrong key**: Make sure you're using the same key that encrypted the file
2. **Corrupted file**: Restore from backup
3. **Format changed**: Old profiles may use different encryption

```bash
# View raw encrypted format
cat profiles.enc.yaml | jq

# Check if it's the new format
jq -r '.version' profiles.enc.yaml  # Should output: 1
```

### "Permission denied"

```bash
# Fix key file permissions
chmod 600 .encryption-key

# Fix ownership (if needed)
chown $USER:$USER .encryption-key
```

---

## 📖 Related Documentation

- [README.md](README.md) - Quick start guide
- [CHANGELOG.md](CHANGELOG.md) - Version history
- [CODE-EXECUTION.md](CODE-EXECUTION.md) - Code execution feature
- [STREAMABLE-HTTP.md](STREAMABLE-HTTP.md) - HTTP transport

---

## 💡 Quick Reference

**Generate key:**
```bash
openssl rand -hex 32 > .encryption-key
```

**Start Web UI (recommended):**
```bash
export CONFIG_SERVER_KEY=$(cat .encryption-key)
./skyline-server --config=config.yaml
```

**Edit via CLI (advanced):**
```bash
./scripts/edit-profiles.sh
```

**Test configuration:**
```bash
# Web UI has built-in testing
# Or use skyline CLI:
./skyline --config=config.yaml
```

---

**Remember:** When in doubt, use the Web UI. It's designed to be secure by default! 🔒
