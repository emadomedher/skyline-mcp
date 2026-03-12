package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"skyline-mcp/internal/serverconfig"
)

// validateKeyResponse is the response from the cloud key validation endpoint.
type validateKeyResponse struct {
	Valid   bool   `json:"valid"`
	Email   string `json:"email"`
	Org     string `json:"org"`
	Plan    string `json:"plan"`
	Tunnels struct {
		Used  int `json:"used"`
		Quota int `json:"quota"`
	} `json:"tunnels"`
	Error string `json:"error,omitempty"`
}

// runAuth dispatches the auth subcommand: login, status, logout.
func runAuth(logger *slog.Logger, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: skyline auth <login|status|logout>")
	}

	switch args[0] {
	case "login":
		return authLogin(logger, args[1:])
	case "status":
		return authStatus(logger)
	case "logout":
		return authLogout(logger)
	default:
		return fmt.Errorf("unknown auth command %q — expected login, status, or logout", args[0])
	}
}

// authLogin prompts for an API key (or accepts --api-key flag), validates it,
// and stores it in ~/.skyline/config.yaml.
func authLogin(logger *slog.Logger, args []string) error {
	// Check for --api-key flag
	var apiKey string
	for i, arg := range args {
		if arg == "--api-key" && i+1 < len(args) {
			apiKey = args[i+1]
			break
		}
		if strings.HasPrefix(arg, "--api-key=") {
			apiKey = strings.TrimPrefix(arg, "--api-key=")
			break
		}
	}

	// If no flag, prompt interactively
	if apiKey == "" {
		fmt.Print("Enter your Skyline Cloud API key: ")
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			apiKey = strings.TrimSpace(scanner.Text())
		}
		if apiKey == "" {
			return fmt.Errorf("no API key provided")
		}
	}

	// Load config to get cloud endpoint
	configPath := defaultConfigPath()
	cfg, err := serverconfig.Load(configPath)
	if err != nil {
		cfg = serverconfig.Default()
	}

	endpoint := cfg.Cloud.Endpoint
	if endpoint == "" {
		endpoint = "https://cloud.xskyline.com"
	}

	// Validate key against cloud API
	fmt.Printf("Validating API key against %s...\n", endpoint)
	resp, err := validateAPIKey(endpoint, apiKey)
	if err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	if !resp.Valid {
		msg := "invalid API key"
		if resp.Error != "" {
			msg = resp.Error
		}
		return fmt.Errorf("%s", msg)
	}

	// Store API key in config
	if err := serverconfig.SetCloudAPIKey(configPath, apiKey); err != nil {
		return fmt.Errorf("save API key: %w", err)
	}

	fmt.Println("")
	fmt.Println("Successfully authenticated!")
	fmt.Printf("  Account: %s\n", resp.Email)
	fmt.Printf("  Org:     %s\n", resp.Org)
	fmt.Printf("  Plan:    %s\n", resp.Plan)
	fmt.Println("")
	fmt.Println("API key saved to ~/.skyline/config.yaml")
	fmt.Println("Run 'skyline gateway start' to connect to Skyline Cloud.")

	return nil
}

// authStatus shows the current authentication status and account info.
func authStatus(logger *slog.Logger) error {
	configPath := defaultConfigPath()
	cfg, err := serverconfig.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if cfg.Cloud.APIKey == "" {
		fmt.Println("Not authenticated.")
		fmt.Println("")
		fmt.Println("Run 'skyline auth login' to connect to Skyline Cloud.")
		return nil
	}

	endpoint := cfg.Cloud.Endpoint
	if endpoint == "" {
		endpoint = "https://cloud.xskyline.com"
	}

	resp, err := validateAPIKey(endpoint, cfg.Cloud.APIKey)
	if err != nil {
		fmt.Println("Authentication status: error")
		return fmt.Errorf("could not reach cloud API: %w", err)
	}

	if !resp.Valid {
		fmt.Println("Authentication status: invalid key")
		fmt.Println("")
		fmt.Println("Your stored API key is no longer valid.")
		fmt.Println("Run 'skyline auth login' to re-authenticate.")
		return nil
	}

	fmt.Println("Authentication status: authenticated")
	fmt.Println("")
	fmt.Printf("  Account:  %s\n", resp.Email)
	fmt.Printf("  Org:      %s\n", resp.Org)
	fmt.Printf("  Plan:     %s\n", resp.Plan)
	fmt.Printf("  Tunnels:  %d/%d used\n", resp.Tunnels.Used, resp.Tunnels.Quota)
	fmt.Printf("  Endpoint: %s\n", endpoint)

	return nil
}

// authLogout removes the stored API key from config.
func authLogout(logger *slog.Logger) error {
	configPath := defaultConfigPath()
	cfg, err := serverconfig.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if cfg.Cloud.APIKey == "" {
		fmt.Println("Not authenticated — nothing to do.")
		return nil
	}

	if err := serverconfig.RemoveCloudAPIKey(configPath); err != nil {
		return fmt.Errorf("remove API key: %w", err)
	}

	fmt.Println("Logged out. API key removed from ~/.skyline/config.yaml")
	return nil
}

// validateAPIKey calls the cloud validation endpoint and returns the response.
func validateAPIKey(endpoint, apiKey string) (*validateKeyResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	url := strings.TrimRight(endpoint, "/") + "/api/v1/auth/validate-key"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return &validateKeyResponse{Valid: false, Error: "invalid or expired API key"}, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var result validateKeyResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &result, nil
}

// defaultConfigPath returns the default config.yaml path (~/.skyline/config.yaml).
func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "config.yaml"
	}
	return filepath.Join(home, ".skyline", "config.yaml")
}
