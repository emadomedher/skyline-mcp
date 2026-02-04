package main

import (
	"context"
	"flag"
	"log"
	"os"

	"mcp-api-bridge/internal/config"
	"mcp-api-bridge/internal/mcp"
	"mcp-api-bridge/internal/redact"
	"mcp-api-bridge/internal/runtime"
	"mcp-api-bridge/internal/spec"
)

func main() {
	configPath := flag.String("config", "./config.yaml", "Path to YAML config")
	configURL := flag.String("config-url", "", "Config server base URL (e.g., http://localhost:9190)")
	profile := flag.String("profile", "", "Config profile name when using --config-url")
	configToken := flag.String("config-token", "", "Bearer token for config profile (defaults to MCP_PROFILE_TOKEN)")
	transport := flag.String("transport", "stdio", "Transport: stdio, sse (legacy), or http (streamable)")
	listen := flag.String("listen", ":8080", "HTTP listen address for sse/http transport")
	sseAuthType := flag.String("sse-auth-type", "", "SSE auth type: bearer, basic, api-key")
	sseAuthToken := flag.String("sse-auth-token", "", "SSE bearer token")
	sseAuthUsername := flag.String("sse-auth-username", "", "SSE basic auth username")
	sseAuthPassword := flag.String("sse-auth-password", "", "SSE basic auth password")
	sseAuthHeader := flag.String("sse-auth-header", "", "SSE api-key header")
	sseAuthValue := flag.String("sse-auth-value", "", "SSE api-key value")
	flag.Parse()

	logger := log.New(os.Stderr, "", log.LstdFlags)
	ctx := context.Background()

	var cfg *config.Config
	if *configURL != "" {
		token := *configToken
		if token == "" {
			token = os.Getenv("MCP_PROFILE_TOKEN")
		}
		if *profile == "" {
			profEnv := os.Getenv("MCP_PROFILE")
			if profEnv != "" {
				*profile = profEnv
			}
		}
		raw, err := config.FetchProfileConfig(ctx, *configURL, *profile, token)
		if err != nil {
			logger.Fatalf("config fetch: %v", err)
		}
		cfg, err = config.LoadFromBytes(raw)
		if err != nil {
			logger.Fatalf("config load: %v", err)
		}
	} else {
		var err error
		cfg, err = config.Load(*configPath)
		if err != nil {
			logger.Fatalf("config load: %v", err)
		}
	}

	redactor := redact.NewRedactor()
	redactor.AddSecrets(cfg.Secrets())

	logger.Printf("loading specs...")
	services, err := spec.LoadServices(ctx, cfg, logger, redactor)
	if err != nil {
		logger.Fatalf("spec load: %v", err)
	}
	logger.Printf("loaded %d services", len(services))

	executor, err := runtime.NewExecutor(cfg, services, logger, redactor)
	if err != nil {
		logger.Fatalf("executor init: %v", err)
	}

	logger.Printf("building registry...")
	registry, err := mcp.NewRegistry(services)
	if err != nil {
		logger.Fatalf("registry init: %v", err)
	}
	logger.Printf("registry ready (%d tools)", len(registry.Tools))

	server := mcp.NewServer(registry, executor, logger, redactor)
	switch *transport {
	case "stdio":
		if err := server.Serve(ctx, os.Stdin, os.Stdout); err != nil {
			logger.Fatalf("server error: %v", err)
		}
	case "sse", "http", "streamable-http":
		var auth *config.AuthConfig
		if *sseAuthType != "" {
			auth = &config.AuthConfig{
				Type:     *sseAuthType,
				Token:    *sseAuthToken,
				Username: *sseAuthUsername,
				Password: *sseAuthPassword,
				Header:   *sseAuthHeader,
				Value:    *sseAuthValue,
			}
			if err := auth.Validate(); err != nil {
				logger.Fatalf("sse auth: %v", err)
			}
			redactor.AddSecrets(authSecrets(auth))
		}
		httpServer := mcp.NewHTTPServer(server, logger, auth)
		if err := httpServer.Serve(ctx, *listen); err != nil {
			logger.Fatalf("server error: %v", err)
		}
	default:
		logger.Fatalf("unknown transport %q", *transport)
	}
}

func authSecrets(auth *config.AuthConfig) []string {
	if auth == nil {
		return nil
	}
	switch auth.Type {
	case "bearer":
		if auth.Token != "" {
			return []string{auth.Token}
		}
	case "basic":
		if auth.Password != "" {
			return []string{auth.Password}
		}
	case "api-key":
		if auth.Value != "" {
			return []string{auth.Value}
		}
	}
	return nil
}
