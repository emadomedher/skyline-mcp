#!/bin/bash
set -e

# Skyline Profile Editor
# Decrypt → Edit with $EDITOR → Re-encrypt profiles.enc.yaml
#
# Usage:
#   ./scripts/edit-profiles.sh                    # Edit profiles
#   ./scripts/edit-profiles.sh --view             # View only (no save)
#   ./scripts/edit-profiles.sh --key-file=path    # Custom key file

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
PROFILES_FILE="$PROJECT_ROOT/profiles.enc.yaml"
KEY_FILE="$PROJECT_ROOT/.encryption-key"
TEMP_DECRYPTED=$(mktemp --suffix=.yaml)
TEMP_REENCRYPTED=$(mktemp)

VIEW_ONLY=false

# Parse arguments
while [[ $# -gt 0 ]]; do
  case $1 in
    --view)
      VIEW_ONLY=true
      shift
      ;;
    --key-file=*)
      KEY_FILE="${1#*=}"
      shift
      ;;
    *)
      echo "Unknown option: $1"
      echo "Usage: $0 [--view] [--key-file=path]"
      exit 1
      ;;
  esac
done

# Check requirements
if ! command -v jq &> /dev/null; then
  echo "❌ Error: jq is required. Install with: sudo apt install jq"
  exit 1
fi

if ! command -v openssl &> /dev/null; then
  echo "❌ Error: openssl is required"
  exit 1
fi

# Load encryption key
if [ ! -f "$KEY_FILE" ]; then
  echo "❌ Error: Encryption key not found at $KEY_FILE"
  echo ""
  echo "Generate a key first:"
  echo "  openssl rand -hex 32 > .encryption-key"
  exit 1
fi

KEY=$(cat "$KEY_FILE" | tr -d '\n')

if [ ${#KEY} -ne 64 ]; then
  echo "❌ Error: Invalid key length. Expected 64 hex characters (32 bytes)"
  exit 1
fi

# Check if profiles file exists
if [ ! -f "$PROFILES_FILE" ]; then
  echo "⚠️  Profiles file not found. Creating empty file..."
  echo "version: 1" > "$TEMP_DECRYPTED"
  echo "profiles: {}" >> "$TEMP_DECRYPTED"
  
  if [ "$VIEW_ONLY" = true ]; then
    cat "$TEMP_DECRYPTED"
    rm -f "$TEMP_DECRYPTED"
    exit 0
  fi
else
  # Decrypt profiles
  echo "🔓 Decrypting profiles..."
  
  # Extract nonce and ciphertext from YAML
  NONCE=$(yq eval '.nonce' "$PROFILES_FILE" 2>/dev/null || jq -r '.nonce' "$PROFILES_FILE")
  CIPHERTEXT=$(yq eval '.ciphertext' "$PROFILES_FILE" 2>/dev/null || jq -r '.ciphertext' "$PROFILES_FILE")
  
  if [ -z "$NONCE" ] || [ -z "$CIPHERTEXT" ]; then
    echo "❌ Error: Invalid encrypted file format"
    exit 1
  fi
  
  # Convert hex key to binary
  KEY_BIN=$(echo "$KEY" | xxd -r -p)
  
  # Decrypt using OpenSSL
  NONCE_BIN=$(echo "$NONCE" | base64 -d)
  CIPHERTEXT_BIN=$(echo "$CIPHERTEXT" | base64 -d)
  
  # Build GCM decryption command
  # Note: OpenSSL GCM requires special handling
  echo "$CIPHERTEXT_BIN" | openssl enc -aes-256-gcm -d -K "$KEY" -iv "$(echo -n "$NONCE_BIN" | xxd -p)" > "$TEMP_DECRYPTED" 2>/dev/null || {
    # Fallback: Use Go binary for decryption (more reliable)
    echo "⚠️  Using Go decryption fallback..."
    
    # Create temporary Go script
    cat > /tmp/decrypt-skyline.go << 'GOEOF'
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
	Version    int    `json:"version" yaml:"version"`
	Nonce      string `json:"nonce" yaml:"nonce"`
	Ciphertext string `json:"ciphertext" yaml:"ciphertext"`
}
func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "Usage: decrypt-skyline <hex-key> <encrypted-file>\n")
		os.Exit(1)
	}
	keyHex := os.Args[1]
	keyBytes, err := hex.DecodeString(keyHex)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid key: %v\n", err)
		os.Exit(1)
	}
	data, err := os.ReadFile(os.Args[2])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Read file: %v\n", err)
		os.Exit(1)
	}
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		fmt.Fprintf(os.Stderr, "Parse JSON: %v\n", err)
		os.Exit(1)
	}
	nonce, err := base64.StdEncoding.DecodeString(env.Nonce)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Decode nonce: %v\n", err)
		os.Exit(1)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(env.Ciphertext)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Decode ciphertext: %v\n", err)
		os.Exit(1)
	}
	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Create cipher: %v\n", err)
		os.Exit(1)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Create GCM: %v\n", err)
		os.Exit(1)
	}
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Decrypt: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(string(plain))
}
GOEOF
    
    go run /tmp/decrypt-skyline.go "$KEY" "$PROFILES_FILE" > "$TEMP_DECRYPTED"
    rm -f /tmp/decrypt-skyline.go
  }
  
  if [ ! -s "$TEMP_DECRYPTED" ]; then
    echo "❌ Decryption failed"
    rm -f "$TEMP_DECRYPTED"
    exit 1
  fi
fi

# View-only mode
if [ "$VIEW_ONLY" = true ]; then
  echo "📄 Decrypted profiles:"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  cat "$TEMP_DECRYPTED"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  rm -f "$TEMP_DECRYPTED"
  exit 0
fi

# Open in editor
EDITOR="${EDITOR:-nano}"
echo "✏️  Opening in $EDITOR..."
echo "   Save and exit to re-encrypt, or exit without saving to cancel."
echo ""

$EDITOR "$TEMP_DECRYPTED"

# Check if file was modified
if ! cmp -s "$TEMP_DECRYPTED" <(go run /tmp/decrypt-skyline.go "$KEY" "$PROFILES_FILE" 2>/dev/null || echo ""); then
  echo ""
  echo "🔒 Re-encrypting..."
  
  # Use skyline-server binary to re-encrypt (most reliable)
  # For now, use Go script
  cat > /tmp/encrypt-skyline.go << 'GOEOF'
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
	Version    int    `json:"version" yaml:"version"`
	Nonce      string `json:"nonce" yaml:"nonce"`
	Ciphertext string `json:"ciphertext" yaml:"ciphertext"`
}
func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "Usage: encrypt-skyline <hex-key> <plaintext-file>\n")
		os.Exit(1)
	}
	keyHex := os.Args[1]
	keyBytes, err := hex.DecodeString(keyHex)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid key: %v\n", err)
		os.Exit(1)
	}
	plain, err := os.ReadFile(os.Args[2])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Read file: %v\n", err)
		os.Exit(1)
	}
	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Create cipher: %v\n", err)
		os.Exit(1)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Create GCM: %v\n", err)
		os.Exit(1)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		fmt.Fprintf(os.Stderr, "Generate nonce: %v\n", err)
		os.Exit(1)
	}
	ciphertext := gcm.Seal(nil, nonce, plain, nil)
	env := envelope{
		Version:    1,
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	}
	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Marshal JSON: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(string(data))
}
GOEOF
  
  go run /tmp/encrypt-skyline.go "$KEY" "$TEMP_DECRYPTED" > "$PROFILES_FILE"
  rm -f /tmp/encrypt-skyline.go
  
  echo "✅ Profiles encrypted and saved!"
else
  echo ""
  echo "⚠️  No changes detected. Profiles unchanged."
fi

# Cleanup
rm -f "$TEMP_DECRYPTED" "$TEMP_REENCRYPTED"

echo ""
echo "💡 Tip: Use the Web UI for easier editing: skyline-server --config=config.yaml"
