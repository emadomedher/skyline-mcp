package oauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
)

// VerifyPKCE verifies a PKCE code_verifier against the stored code_challenge.
// Only S256 is supported (required by ChatGPT / OAuth 2.1).
func VerifyPKCE(verifier, challenge, method string) bool {
	if verifier == "" || challenge == "" {
		return false
	}
	switch method {
	case "S256":
		h := sha256.Sum256([]byte(verifier))
		computed := base64.RawURLEncoding.EncodeToString(h[:])
		return subtle.ConstantTimeCompare([]byte(computed), []byte(challenge)) == 1
	default:
		// "plain" and unknown methods are rejected per OAuth 2.1
		return false
	}
}
