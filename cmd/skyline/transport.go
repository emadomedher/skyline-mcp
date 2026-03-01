package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"skyline-mcp/internal/codegen"
	"skyline-mcp/internal/config"
	"skyline-mcp/internal/mcp"
	"skyline-mcp/internal/redact"
	"skyline-mcp/internal/runtime"
	"skyline-mcp/internal/spec"
)

// runHTTPWithConfig runs the MCP server in HTTP mode with direct config file (no profiles)
func runHTTPWithConfig(configPathArg, listenAddr string, enableAdmin bool, logger *slog.Logger) error {
	ctx := context.Background()

	// Expand config path
	configPath := configPathArg
	if strings.HasPrefix(configPath, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("get home dir: %w", err)
		}
		configPath = filepath.Join(home, configPath[2:])
	}

	// Load config
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Initialize redactor
	redactor := redact.NewRedactor()
	for _, api := range cfg.APIs {
		if api.Auth != nil {
			if api.Auth.Token != "" {
				redactor.AddSecrets([]string{api.Auth.Token})
			}
			if api.Auth.Password != "" {
				redactor.AddSecrets([]string{api.Auth.Password})
			}
			if api.Auth.Value != "" {
				redactor.AddSecrets([]string{api.Auth.Value})
			}
		}
	}

	// Log startup
	logger.Info("🚀 Skyline MCP Server starting", "version", Version, "mode", "http", "config", configPath, "apis", len(cfg.APIs), "listen", listenAddr, "transport", "HTTP")

	// Load services from API specs
	logger.Info("📚 Loading API specifications...")
	services, err := spec.LoadServices(ctx, cfg, logger, redactor)
	if err != nil {
		return fmt.Errorf("load services: %w", err)
	}
	logger.Info("✓ Loaded services", "count", len(services))

	// Build MCP registry
	logger.Info("🔨 Building MCP tool registry...")
	registry, err := mcp.NewRegistry(services)
	if err != nil {
		return fmt.Errorf("build registry: %w", err)
	}
	logger.Info("✓ Registered tools and resources", "tools", len(registry.Tools), "resources", len(registry.Resources))

	// Initialize executor
	executor, err := runtime.NewExecutor(cfg, services, logger, redactor)
	if err != nil {
		return fmt.Errorf("create executor: %w", err)
	}

	// Create MCP server
	mcpServer := mcp.NewServer(registry, executor, logger, redactor, Version)

	// Set up HTTP server
	mux := http.NewServeMux()

	// MCP endpoint (primary)
	mux.HandleFunc("/mcp/v1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Handle MCP JSON-RPC over HTTP
		var req mcp.RPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// Process request
		resp := mcpServer.HandleRequest(ctx, &req)

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			logger.Error("error encoding response", "error", err)
		}
	})

	// Health check
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Root handler
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Skyline MCP Server\n\nMCP Endpoint: POST /mcp/v1\n"))
	})

	// TLS setup — auto-generate self-signed cert if needed
	tlsHost, _, _ := net.SplitHostPort(listenAddr)
	if tlsHost == "" {
		tlsHost = "localhost"
	}
	tlsCertPath, tlsKeyPath, err := ensureTLSCert("", "", []string{tlsHost, "localhost", "127.0.0.1", "::1"}, logger)
	if err != nil {
		return fmt.Errorf("tls setup: %w", err)
	}

	// Create TCP listener with same-port HTTP→HTTPS redirect
	tcpLn, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	ln := &tlsRedirectListener{Listener: tcpLn, httpsHost: listenAddr}

	// HTTP server config
	httpServer := &http.Server{
		Addr:         listenAddr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	logger.Info("✅ Server initialized successfully", "protocol", "HTTPS", "mcp_endpoint", "https://"+listenAddr+"/mcp/v1", "health_check", "https://"+listenAddr+"/healthz")

	// Start graceful-shutdown listener in the background.
	go shutdownOnSignal([]*http.Server{httpServer}, func() {
		if err := executor.Close(); err != nil {
			logger.Warn("executor cleanup error", "error", err)
		}
		logger.Debug("executor closed")
	})

	// Start server with TLS
	if err := httpServer.ServeTLS(ln, tlsCertPath, tlsKeyPath); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// runSTDIO runs the MCP server in STDIO mode for Claude Desktop integration
func runSTDIO(configPathArg string, logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// STDIO mode requires a config file
	if configPathArg == "" {
		return fmt.Errorf("--config flag required for STDIO mode")
	}

	// Expand config path
	configPath := configPathArg
	if strings.HasPrefix(configPath, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("get home dir: %w", err)
		}
		configPath = filepath.Join(home, configPath[2:])
	}

	// Load config
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Initialize redactor
	redactor := redact.NewRedactor()
	for _, api := range cfg.APIs {
		if api.Auth != nil {
			if api.Auth.Token != "" {
				redactor.AddSecrets([]string{api.Auth.Token})
			}
			if api.Auth.Password != "" {
				redactor.AddSecrets([]string{api.Auth.Password})
			}
			if api.Auth.Value != "" {
				redactor.AddSecrets([]string{api.Auth.Value})
			}
		}
	}

	// Log startup (to stderr, not stdout - stdout is reserved for MCP protocol)
	logger.Info("🚀 Skyline MCP Server starting", "version", Version, "mode", "stdio", "config", configPath, "apis", len(cfg.APIs), "transport", "STDIO")

	// Load services from API specs
	logger.Info("📚 Loading API specifications...")
	services, err := spec.LoadServices(ctx, cfg, logger, redactor)
	if err != nil {
		return fmt.Errorf("load services: %w", err)
	}
	logger.Info("✓ Loaded services", "count", len(services))

	// Build MCP registry
	logger.Info("🔨 Building MCP tool registry...")
	registry, err := mcp.NewRegistry(services)
	if err != nil {
		return fmt.Errorf("build registry: %w", err)
	}
	logger.Info("✓ Registered tools and resources", "tools", len(registry.Tools), "resources", len(registry.Resources))

	// Initialize executor
	executor, err := runtime.NewExecutor(cfg, services, logger, redactor)
	if err != nil {
		return fmt.Errorf("create executor: %w", err)
	}

	// Create MCP server
	mcpServer := mcp.NewServer(registry, executor, logger, redactor, Version)

	// Set up code execution (goja — no external dependencies)
	codeExec, err := codegen.SetupCodeExecution(registry, logger)
	if err != nil {
		logger.Warn("code execution setup failed", "error", err)
	} else if codeExec != nil {
		// Wire direct tool calling (no HTTP server in STDIO mode)
		codeExec.SetDirectCallFunc(func(ctx context.Context, toolName string, args map[string]any) (any, error) {
			tool, ok := registry.Tools[toolName]
			if !ok || tool.Operation == nil {
				return nil, fmt.Errorf("tool not found: %s", toolName)
			}
			result, err := executor.Execute(ctx, tool.Operation, args)
			if err != nil {
				return nil, err
			}
			return result.Body, nil
		})
		mcpServer.SetCodeExecutor(codeExec)
		logger.Info("✓ Code execution enabled", "runtime", "goja")
	}

	logger.Info("✅ Server initialized successfully", "mode", "stdio")

	// Run server in STDIO mode (stdin → stdout)
	serveErr := mcpServer.Serve(ctx, os.Stdin, os.Stdout)

	// Clean up resources
	if err := executor.Close(); err != nil {
		logger.Warn("executor cleanup error", "error", err)
	}

	if serveErr != nil && ctx.Err() == nil {
		return fmt.Errorf("server error: %w", serveErr)
	}

	logger.Info("Shutdown complete")
	return nil
}
