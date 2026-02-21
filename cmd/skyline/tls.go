package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"skyline-mcp/internal/serverconfig"
)

// ensureTLSCert returns usable cert and key file paths. If the provided paths
// point to existing files they are returned as-is. Otherwise a self-signed
// certificate is generated, written to ~/.skyline/tls/, and installed into
// the system trust store (best-effort).
func ensureTLSCert(certPath, keyPath string, hosts []string, logger *log.Logger) (string, string, error) {
	// Expand ~ in user-provided paths
	if certPath != "" {
		if p, err := serverconfig.ExpandPath(certPath); err == nil {
			certPath = p
		}
	}
	if keyPath != "" {
		if p, err := serverconfig.ExpandPath(keyPath); err == nil {
			keyPath = p
		}
	}

	// If both files exist, use them directly
	if certPath != "" && keyPath != "" {
		if fileExists(certPath) && fileExists(keyPath) {
			logger.Printf("Using TLS certificate: %s", certPath)
			return certPath, keyPath, nil
		}
		// User provided paths but files missing — warn and fall through to auto-gen
		logger.Printf("WARNING: configured TLS cert/key not found (%s, %s), generating self-signed", certPath, keyPath)
	}

	// Auto-generate into ~/.skyline/tls/
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("get home dir: %w", err)
	}
	tlsDir := filepath.Join(home, ".skyline", "tls")
	autoCert := filepath.Join(tlsDir, "skyline.crt")
	autoKey := filepath.Join(tlsDir, "skyline.key")

	// If auto-generated files already exist, reuse them (but still ensure trusted)
	if fileExists(autoCert) && fileExists(autoKey) {
		logger.Printf("Using auto-generated self-signed certificate (~/.skyline/tls/)")
		ensureCertTrusted(autoCert, logger)
		return autoCert, autoKey, nil
	}

	// Generate new self-signed certificate
	if err := os.MkdirAll(tlsDir, 0o700); err != nil {
		return "", "", fmt.Errorf("create tls dir: %w", err)
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("generate key: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", fmt.Errorf("generate serial: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber:          serialNumber,
		Subject:               pkix.Name{CommonName: "Skyline MCP Server"},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true, // Self-signed CA so trust store accepts it
	}

	// Add SANs
	seen := map[string]bool{}
	for _, h := range hosts {
		if seen[h] {
			continue
		}
		seen[h] = true
		if ip := net.ParseIP(h); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, h)
		}
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		return "", "", fmt.Errorf("create certificate: %w", err)
	}

	// Write cert
	certFile, err := os.OpenFile(autoCert, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", "", fmt.Errorf("write cert: %w", err)
	}
	if err := pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		certFile.Close()
		return "", "", fmt.Errorf("encode cert: %w", err)
	}
	certFile.Close()

	// Write key
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return "", "", fmt.Errorf("marshal key: %w", err)
	}
	keyFile, err := os.OpenFile(autoKey, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", "", fmt.Errorf("write key: %w", err)
	}
	if err := pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}); err != nil {
		keyFile.Close()
		return "", "", fmt.Errorf("encode key: %w", err)
	}
	keyFile.Close()

	logger.Printf("Generated self-signed TLS certificate (~/.skyline/tls/)")

	// Install into system trust store (best-effort)
	ensureCertTrusted(autoCert, logger)

	return autoCert, autoKey, nil
}

// ensureCertTrusted checks whether the certificate is already trusted by the OS
// and installs it if not. Safe to call on every startup — skips if already trusted.
func ensureCertTrusted(certPath string, logger *log.Logger) {
	switch runtime.GOOS {
	case "darwin":
		// Check if already trusted via security verify-cert
		verifyCmd := exec.Command("security", "verify-cert", "-c", certPath, "-p", "ssl")
		if verifyCmd.Run() == nil {
			// Already trusted — nothing to do
			return
		}

		// Not trusted — install to login keychain
		home, err := os.UserHomeDir()
		if err != nil {
			break
		}
		keychain := filepath.Join(home, "Library", "Keychains", "login.keychain-db")
		cmd := exec.Command("security", "add-trusted-cert", "-r", "trustRoot", "-p", "ssl", "-k", keychain, certPath)
		if out, err := cmd.CombinedOutput(); err != nil {
			logger.Printf("Could not auto-trust certificate: %v", err)
			if len(out) > 0 {
				logger.Printf("  %s", strings.TrimSpace(string(out)))
			}
			logger.Printf("  To trust manually:")
			logger.Printf("    security add-trusted-cert -r trustRoot -p ssl -k %s %s", keychain, certPath)
		} else {
			logger.Printf("Installed certificate into macOS login keychain (trusted for SSL)")
		}
		return

	case "linux":
		// Check if cert is already in the system CA bundle
		caDir := "/usr/local/share/ca-certificates"
		dest := filepath.Join(caDir, "skyline-mcp.crt")
		if fileExists(dest) {
			return
		}
		logger.Printf("To trust the self-signed certificate on Linux:")
		logger.Printf("  sudo cp %s %s && sudo update-ca-certificates", certPath, dest)
		return

	case "windows":
		// Windows: add to current user's Root trust store via certutil
		// Check if already trusted by attempting to verify
		verifyCmd := exec.Command("certutil", "-verify", certPath)
		if verifyCmd.Run() == nil {
			return
		}
		// Install to current user's trusted root store (no admin required)
		cmd := exec.Command("certutil", "-user", "-addstore", "Root", certPath)
		if out, err := cmd.CombinedOutput(); err != nil {
			logger.Printf("Could not auto-trust certificate: %v", err)
			if len(out) > 0 {
				logger.Printf("  %s", strings.TrimSpace(string(out)))
			}
			logger.Printf("  To trust manually:")
			logger.Printf("    certutil -user -addstore Root %s", certPath)
		} else {
			logger.Printf("Installed certificate into Windows user trust store")
		}
		return
	}

	logger.Printf("Self-signed certificate at: %s", certPath)
	logger.Printf("Add it to your system trust store for MCP clients to connect without errors.")
}

// tlsRedirectListener wraps a net.Listener and handles both TLS and plain HTTP
// connections on the same port. TLS connections (starting with byte 0x16) are
// passed through to the TLS server. Plain HTTP connections get a 301 redirect
// to the HTTPS URL and are closed.
type tlsRedirectListener struct {
	net.Listener
	httpsHost string
}

func (l *tlsRedirectListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}

		// Peek at first byte to distinguish TLS from plain HTTP
		buf := make([]byte, 1)
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, err := conn.Read(buf)
		conn.SetReadDeadline(time.Time{})
		if err != nil || n == 0 {
			conn.Close()
			continue
		}

		if buf[0] == 0x16 {
			// TLS ClientHello — return connection with the peeked byte prepended
			return &prefixConn{Conn: conn, prefix: buf[:1]}, nil
		}

		// Plain HTTP — read request line, redirect, close (in background)
		go l.redirectHTTP(conn, buf[0])
	}
}

func (l *tlsRedirectListener) redirectHTTP(conn net.Conn, firstByte byte) {
	defer conn.Close()

	// Read the rest of the HTTP request line to preserve the path
	line := []byte{firstByte}
	one := make([]byte, 1)
	for i := 0; i < 4096; i++ {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, err := conn.Read(one)
		if err != nil || n == 0 {
			break
		}
		line = append(line, one[0])
		if one[0] == '\n' {
			break
		}
	}

	// Parse "GET /path HTTP/1.1\r\n"
	path := "/"
	parts := strings.SplitN(string(line), " ", 3)
	if len(parts) >= 2 {
		path = parts[1]
	}

	target := "https://" + l.httpsHost + path
	resp := fmt.Sprintf("HTTP/1.1 301 Moved Permanently\r\nLocation: %s\r\nConnection: close\r\nContent-Length: 0\r\n\r\n", target)
	conn.Write([]byte(resp))
}

// prefixConn wraps a net.Conn with bytes that have already been read,
// replaying them before reading from the underlying connection.
type prefixConn struct {
	net.Conn
	prefix []byte
}

func (c *prefixConn) Read(b []byte) (int, error) {
	if len(c.prefix) > 0 {
		n := copy(b, c.prefix)
		c.prefix = c.prefix[n:]
		return n, nil
	}
	return c.Conn.Read(b)
}
