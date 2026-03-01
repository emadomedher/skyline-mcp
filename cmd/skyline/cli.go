package main

import (
	"bytes"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Skyline MCP Server v%s\n", Version)
		fmt.Fprintf(os.Stderr, "Model Context Protocol server with API discovery and code execution\n\n")
		fmt.Fprintf(os.Stderr, "Usage: skyline [options]\n\n")
		fmt.Fprintf(os.Stderr, "Transport Modes:\n")
		fmt.Fprintf(os.Stderr, "  --transport <mode>          Transport mode: stdio, http (default: http)\n")
		fmt.Fprintf(os.Stderr, "  --admin                     Enable Web UI and admin dashboard (default: true)\n")
		fmt.Fprintf(os.Stderr, "  --bind <addr>               Network interface and port (default: localhost:8191)\n")
		fmt.Fprintf(os.Stderr, "  --config <path>             Server config.yaml path (default: ~/.skyline/config.yaml)\n\n")
		fmt.Fprintf(os.Stderr, "Logging:\n")
		fmt.Fprintf(os.Stderr, "  --log-format <format>       Log output format: text, json (default: text)\n")
		fmt.Fprintf(os.Stderr, "  --log-level <level>         Log level: debug, info, warn, error (default: info)\n\n")
		fmt.Fprintf(os.Stderr, "Encryption & Profiles:\n")
		fmt.Fprintf(os.Stderr, "  --validate [file]           Validate encrypted profiles file can be decrypted\n")
		fmt.Fprintf(os.Stderr, "                              Default file: ~/.skyline/profiles.enc.yaml\n")
		fmt.Fprintf(os.Stderr, "                              Key source: --key flag or SKYLINE_PROFILES_KEY env var\n")
		fmt.Fprintf(os.Stderr, "                              Exit codes: 0=valid, 1=not found, 2=key error, 3=decrypt failed\n\n")
		fmt.Fprintf(os.Stderr, "  --init-profiles [file]      Generate new encrypted profiles file\n")
		fmt.Fprintf(os.Stderr, "                              Default file: ~/.skyline/profiles.enc.yaml\n")
		fmt.Fprintf(os.Stderr, "                              Key source: --key flag or SKYLINE_PROFILES_KEY env var\n")
		fmt.Fprintf(os.Stderr, "                              Exit codes: 0=success, 1=exists, 2=key error, 3=encrypt failed\n\n")
		fmt.Fprintf(os.Stderr, "  --key <key>                 Encryption key for --validate or --init-profiles\n")
		fmt.Fprintf(os.Stderr, "                              If not specified, uses SKYLINE_PROFILES_KEY env var\n")
		fmt.Fprintf(os.Stderr, "                              Format: 64-char hex string (32 bytes)\n\n")
		fmt.Fprintf(os.Stderr, "  --storage <path>            Encrypted profiles storage path (default: ./profiles.enc.yaml)\n")
		fmt.Fprintf(os.Stderr, "  --key-env <name>            Env var name for encryption key (default: SKYLINE_PROFILES_KEY)\n\n")
		fmt.Fprintf(os.Stderr, "Authentication:\n")
		fmt.Fprintf(os.Stderr, "  --auth-mode <mode>          Auth mode: none, bearer (default: bearer)\n\n")
		fmt.Fprintf(os.Stderr, "Other:\n")
		fmt.Fprintf(os.Stderr, "  --env-file <path>           Optional env file to load before startup\n")
		fmt.Fprintf(os.Stderr, "  --version, -v               Show version information\n")
		fmt.Fprintf(os.Stderr, "  --help, -h                  Show this help message\n\n")
		fmt.Fprintf(os.Stderr, "Commands:\n")
		fmt.Fprintf(os.Stderr, "  skyline update              Update Skyline to the latest version\n\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  # Start HTTP server with Web UI (default)\n")
		fmt.Fprintf(os.Stderr, "  skyline\n\n")
		fmt.Fprintf(os.Stderr, "  # Start in STDIO mode (for Claude Desktop)\n")
		fmt.Fprintf(os.Stderr, "  skyline --transport stdio --config config.yaml\n\n")
		fmt.Fprintf(os.Stderr, "  # Validate encrypted profiles\n")
		fmt.Fprintf(os.Stderr, "  skyline --validate\n\n")
		fmt.Fprintf(os.Stderr, "  # Create new encrypted profiles file\n")
		fmt.Fprintf(os.Stderr, "  export SKYLINE_PROFILES_KEY=$(openssl rand -hex 32)\n")
		fmt.Fprintf(os.Stderr, "  skyline --init-profiles\n\n")
		fmt.Fprintf(os.Stderr, "Documentation: https://skyline.projex.cc\n")
		fmt.Fprintf(os.Stderr, "Source: https://github.com/emadomedher/skyline-mcp\n")
	}
}

// runValidate validates that the encrypted profiles file can be decrypted with the provided key
// Exit codes: 0 = valid, 1 = file not found, 2 = key missing, 3 = decryption failed
func runValidate(storagePath, keyFlag, keyEnv string, logger *slog.Logger) int {
	// Expand storage path
	profilesPath := storagePath
	if profilesPath == "./profiles.enc.yaml" {
		home, err := os.UserHomeDir()
		if err == nil {
			profilesPath = filepath.Join(home, ".skyline", "profiles.enc.yaml")
		}
	}

	// Check if file exists
	if !fileExists(profilesPath) {
		logger.Error("profiles file not found", "path", profilesPath)
		return 1
	}

	// Get encryption key
	keyRaw := keyFlag
	if keyRaw == "" {
		keyRaw = os.Getenv(keyEnv)
	}
	if keyRaw == "" {
		logger.Error("encryption key not provided",
			"hint", "use --key flag or set "+keyEnv+" environment variable")
		return 2
	}

	// Decode key
	key, err := decodeKey(keyRaw)
	if err != nil {
		logger.Error("invalid encryption key", "error", err)
		return 2
	}

	// Read encrypted file
	data, err := os.ReadFile(profilesPath)
	if err != nil {
		logger.Error("failed to read file", "error", err)
		return 3
	}

	// Parse envelope
	var env envelope
	if err := yaml.Unmarshal(data, &env); err != nil {
		logger.Error("invalid file format", "error", err)
		return 3
	}

	// Decrypt
	plain, err := decrypt(env, key)
	if err != nil {
		logger.Error("decryption failed", "error", err,
			"hint", "the key may be incorrect or the file may be corrupted")
		return 3
	}

	// Validate it's proper YAML
	var store profileStore
	if err := yaml.Unmarshal(plain, &store); err != nil {
		logger.Error("invalid profiles data", "error", err)
		return 3
	}

	logger.Info("validation successful",
		"path", profilesPath,
		"profiles", len(store.Profiles),
		"key_env", keyEnv,
	)
	return 0
}

// runInitProfiles creates a new encrypted profiles file with the provided key
// Exit codes: 0 = success, 1 = file exists, 2 = key missing, 3 = encryption failed
func runInitProfiles(storagePath, keyFlag, keyEnv string, logger *slog.Logger) int {
	// Expand storage path
	profilesPath := storagePath
	if profilesPath == "./profiles.enc.yaml" {
		home, err := os.UserHomeDir()
		if err == nil {
			profilesPath = filepath.Join(home, ".skyline", "profiles.enc.yaml")
		}
	}

	// Check if file already exists
	if fileExists(profilesPath) {
		logger.Error("profiles file already exists", "path", profilesPath,
			"hint", "delete it first if you want to start fresh")
		return 1
	}

	// Get encryption key
	keyRaw := keyFlag
	if keyRaw == "" {
		keyRaw = os.Getenv(keyEnv)
	}
	if keyRaw == "" {
		logger.Error("encryption key not provided",
			"hint", "use --key flag or set "+keyEnv+" environment variable")
		return 2
	}

	// Decode key
	key, err := decodeKey(keyRaw)
	if err != nil {
		logger.Error("invalid encryption key", "error", err)
		return 2
	}

	// Create empty profiles store
	store := profileStore{
		Profiles: []profile{},
	}

	// Marshal to YAML
	plain, err := yaml.Marshal(&store)
	if err != nil {
		logger.Error("failed to marshal profiles", "error", err)
		return 3
	}

	// Encrypt
	env, err := encrypt(plain, key)
	if err != nil {
		logger.Error("encryption failed", "error", err)
		return 3
	}

	// Marshal envelope
	envData, err := yaml.Marshal(env)
	if err != nil {
		logger.Error("failed to marshal envelope", "error", err)
		return 3
	}

	// Ensure directory exists
	dir := filepath.Dir(profilesPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		logger.Error("failed to create directory", "error", err)
		return 3
	}

	// Write file
	if err := os.WriteFile(profilesPath, envData, 0600); err != nil {
		logger.Error("failed to write file", "error", err)
		return 3
	}

	logger.Info("encrypted profiles file created",
		"path", profilesPath,
		"key_env", keyEnv,
		"profiles", 0,
	)
	return 0
}

func loadEnvFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := bytes.Split(data, []byte{'\n'})
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || bytes.HasPrefix(line, []byte("#")) {
			continue
		}
		parts := bytes.SplitN(line, []byte{'='}, 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(string(parts[0]))
		val := strings.TrimSpace(string(parts[1]))
		if key == "" {
			continue
		}
		if _, ok := os.LookupEnv(key); ok {
			continue
		}
		_ = os.Setenv(key, strings.Trim(val, `"'`))
	}
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
