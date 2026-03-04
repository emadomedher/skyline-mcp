package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

func runUpdate(logger *slog.Logger) error {
	logger.Info("checking for updates...")

	// Get current executable path
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("resolve symlinks: %w", err)
	}

	logger.Info("current version info", "version", currentVersion(), "binary", exePath)

	// Check for latest release
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
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
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil { //nolint:govet // intentional err shadow
		return fmt.Errorf("decode release info: %w", err)
	}

	logger.Info("latest version found", "latest", release.TagName)

	// Check if update needed
	if release.TagName == currentVersion() {
		logger.Info("already up to date")
		return nil
	}

	logger.Info("update available", "current", currentVersion(), "latest", release.TagName)

	// Determine platform identifier
	platformKey := runtime.GOOS + "-" + runtime.GOARCH
	switch runtime.GOOS {
	case "linux", "darwin", "windows":
		// supported
	default:
		return fmt.Errorf("unsupported platform: %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	// Find download URL — try archive format first (new), then bare binary (old)
	var downloadURL, assetName string
	for _, asset := range release.Assets {
		// New format: skyline-v0.9.17-linux-amd64.tar.gz or .zip
		if strings.Contains(asset.Name, platformKey) && (strings.HasSuffix(asset.Name, ".tar.gz") || strings.HasSuffix(asset.Name, ".zip")) {
			downloadURL = asset.BrowserDownloadURL
			assetName = asset.Name
			break
		}
	}
	if downloadURL == "" {
		// Old format: skyline-linux-amd64 (bare binary)
		bareName := "skyline-" + platformKey
		if runtime.GOOS == "windows" {
			bareName += ".exe"
		}
		for _, asset := range release.Assets {
			if asset.Name == bareName {
				downloadURL = asset.BrowserDownloadURL
				assetName = asset.Name
				break
			}
		}
	}

	if downloadURL == "" {
		return fmt.Errorf("no binary found for %s", platformKey)
	}

	logger.Info("downloading update", "asset", assetName)

	// Download asset
	req, err = http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: status %d", resp.StatusCode)
	}

	// Download to temp file
	tmpArchive, err := os.CreateTemp("", "skyline-update-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpArchivePath := tmpArchive.Name()
	defer os.Remove(tmpArchivePath)

	written, err := io.Copy(tmpArchive, resp.Body)
	if err != nil {
		tmpArchive.Close()
		return fmt.Errorf("download: %w", err)
	}
	tmpArchive.Close()

	logger.Info("download complete", "bytes", written)

	// Extract binary from archive (or use directly if bare binary)
	var binaryPath string
	if strings.HasSuffix(assetName, ".tar.gz") {
		binaryPath, err = extractFromTarGz(tmpArchivePath, "skyline")
		if err != nil {
			return fmt.Errorf("extract from tar.gz: %w", err)
		}
		defer os.Remove(binaryPath)
	} else if strings.HasSuffix(assetName, ".zip") {
		binaryName := "skyline"
		if runtime.GOOS == "windows" {
			binaryName = "skyline.exe"
		}
		binaryPath, err = extractFromZip(tmpArchivePath, binaryName)
		if err != nil {
			return fmt.Errorf("extract from zip: %w", err)
		}
		defer os.Remove(binaryPath)
	} else {
		// Bare binary — use the downloaded file directly
		binaryPath = tmpArchivePath
	}

	// Make binary executable
	if err := os.Chmod(binaryPath, 0755); err != nil { //nolint:govet // intentional err shadow
		return fmt.Errorf("chmod: %w", err)
	}

	// Verify new binary works
	logger.Info("verifying new binary...")
	cmd := exec.Command(binaryPath, "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("verify binary: %w (output: %s)", err, output)
	}

	// Backup current binary
	backupPath := exePath + ".backup"
	if err := os.Rename(exePath, backupPath); err != nil {
		return fmt.Errorf("backup current binary: %w", err)
	}

	// Move new binary into place (copy if cross-device)
	if err := moveFile(binaryPath, exePath); err != nil {
		// Restore backup on failure
		_ = os.Rename(backupPath, exePath)
		return fmt.Errorf("install new binary: %w", err)
	}

	// Remove backup
	_ = os.Remove(backupPath)

	logger.Info("successfully updated — restart skyline to use the new version", "version", release.TagName)

	return nil
}

// extractFromTarGz extracts a named file from a .tar.gz archive and returns
// the path to the extracted file in a temp directory.
func extractFromTarGz(archivePath, targetName string) (string, error) {
	f, err := os.Open(archivePath) //nolint:gosec // path from our own temp file
	if err != nil {
		return "", err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("tar next: %w", err)
		}

		// Match by base name (the archive may have just "skyline" at the root)
		if filepath.Base(hdr.Name) == targetName && hdr.Typeflag == tar.TypeReg {
			tmpFile, err := os.CreateTemp("", "skyline-extracted-*")
			if err != nil {
				return "", err
			}
			// Limit extraction size to 200MB to prevent decompression bombs
			limited := io.LimitReader(tr, 200*1024*1024)
			if _, err := io.Copy(tmpFile, limited); err != nil { //nolint:gosec // size-limited above
				tmpFile.Close()
				os.Remove(tmpFile.Name())
				return "", fmt.Errorf("extract: %w", err)
			}
			tmpFile.Close()
			return tmpFile.Name(), nil
		}
	}
	return "", fmt.Errorf("file %q not found in archive", targetName)
}

// extractFromZip extracts a named file from a .zip archive and returns
// the path to the extracted file in a temp directory.
func extractFromZip(archivePath, targetName string) (string, error) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", fmt.Errorf("open zip: %w", err)
	}
	defer zr.Close()

	for _, f := range zr.File {
		if filepath.Base(f.Name) == targetName {
			rc, err := f.Open()
			if err != nil {
				return "", fmt.Errorf("open file in zip: %w", err)
			}
			defer rc.Close()

			tmpFile, err := os.CreateTemp("", "skyline-extracted-*")
			if err != nil {
				return "", err
			}
			// Limit extraction size to 200MB to prevent decompression bombs
			limited := io.LimitReader(rc, 200*1024*1024)
			if _, err := io.Copy(tmpFile, limited); err != nil {
				tmpFile.Close()
				os.Remove(tmpFile.Name())
				return "", fmt.Errorf("extract: %w", err)
			}
			tmpFile.Close()
			return tmpFile.Name(), nil
		}
	}
	return "", fmt.Errorf("file %q not found in archive", targetName)
}

// moveFile moves src to dst, falling back to copy+remove if rename fails
// (e.g., across filesystem boundaries).
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}

	// Rename failed (likely cross-device) — fall back to copy
	in, err := os.Open(src) //nolint:gosec // path from our own temp file
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	out.Close()

	return os.Remove(src)
}

// showVersion prints version information
func showVersion() {
	fmt.Printf("Skyline MCP Server %s\n", currentVersion())
	fmt.Printf("Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
}
