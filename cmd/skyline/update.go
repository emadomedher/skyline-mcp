package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

const (
	githubAPIURL = "https://api.github.com/repos/emadomedher/skyline-mcp/releases/latest"
)

// Version is set via -ldflags at build time
var Version = "dev"

func currentVersion() string {
	if Version == "dev" {
		return "v" + Version
	}
	if Version[0] != 'v' {
		return "v" + Version
	}
	return Version
}

type GitHubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func runUpdate(logger *log.Logger) error {
	logger.Printf("Checking for updates...")
	
	// Get current executable path
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("resolve symlinks: %w", err)
	}
	
	logger.Printf("Current version: %s", currentVersion())
	logger.Printf("Current binary: %s", exePath)
	
	// Check for latest release
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	req, err := http.NewRequestWithContext(ctx, "GET", githubAPIURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch release info: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("github API returned %d", resp.StatusCode)
	}
	
	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return fmt.Errorf("decode release info: %w", err)
	}
	
	logger.Printf("Latest version: %s", release.TagName)
	
	// Check if update needed
	if release.TagName == currentVersion() {
		logger.Printf("✅ Already up to date!")
		return nil
	}
	
	logger.Printf("🔄 Update available: %s → %s", currentVersion(), release.TagName)
	
	// Determine platform binary name (skyline-server)
	var binaryName string
	switch runtime.GOOS {
	case "linux":
		if runtime.GOARCH == "arm64" {
			binaryName = "skyline-server-linux-arm64"
		} else {
			binaryName = "skyline-server-linux-amd64"
		}
	case "darwin":
		if runtime.GOARCH == "arm64" {
			binaryName = "skyline-server-darwin-arm64"
		} else {
			binaryName = "skyline-server-darwin-amd64"
		}
	default:
		return fmt.Errorf("unsupported platform: %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	
	// Find download URL
	var downloadURL string
	for _, asset := range release.Assets {
		if asset.Name == binaryName {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}
	
	if downloadURL == "" {
		return fmt.Errorf("no binary found for %s", binaryName)
	}
	
	logger.Printf("Downloading %s...", binaryName)
	
	// Download new binary
	req, err = http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}
	
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download binary: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: status %d", resp.StatusCode)
	}
	
	// Create temp file
	tmpFile, err := os.CreateTemp("", "skyline-server-update-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	
	// Download to temp file
	written, err := io.Copy(tmpFile, resp.Body)
	if err != nil {
		tmpFile.Close()
		return fmt.Errorf("download: %w", err)
	}
	tmpFile.Close()
	
	logger.Printf("Downloaded %d bytes", written)
	
	// Make temp file executable
	if err := os.Chmod(tmpPath, 0755); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}
	
	// Verify new binary works
	logger.Printf("Verifying new binary...")
	cmd := exec.Command(tmpPath, "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("verify binary: %w (output: %s)", err, output)
	}
	
	// Backup current binary
	backupPath := exePath + ".backup"
	if err := os.Rename(exePath, backupPath); err != nil {
		return fmt.Errorf("backup current binary: %w", err)
	}
	
	// Move new binary into place
	if err := os.Rename(tmpPath, exePath); err != nil {
		// Restore backup on failure
		os.Rename(backupPath, exePath)
		return fmt.Errorf("install new binary: %w", err)
	}
	
	// Remove backup
	os.Remove(backupPath)
	
	logger.Printf("✅ Successfully updated to %s!", release.TagName)
	logger.Printf("")
	logger.Printf("Restart skyline-server to use the new version")
	
	return nil
}

// showVersion prints version information
func showVersion() {
	fmt.Printf("Skyline MCP Server %s\n", currentVersion())
	fmt.Printf("Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
}
