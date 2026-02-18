package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"skyline-mcp/internal/codegen"
	"skyline-mcp/internal/config"
	"skyline-mcp/internal/mcp"
	"skyline-mcp/internal/redact"
	"skyline-mcp/internal/runtime"
	"skyline-mcp/internal/spec"
)

// runHTTPWithConfig runs the MCP server in HTTP mode with direct config file (no profiles)
func runHTTPWithConfig(configPathArg, listenAddr string, enableAdmin bool, logger *log.Logger) error {
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
	logger.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	logger.Printf("🚀 Skyline MCP Server v%s - HTTP Mode (Direct Config)", Version)
	logger.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	logger.Printf("Config: %s", configPath)
	logger.Printf("APIs: %d configured", len(cfg.APIs))
	logger.Printf("Listen: %s", listenAddr)
	logger.Printf("Transport: HTTP (no authentication)")
	logger.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	logger.Printf("")

	// Load services from API specs
	logger.Printf("📚 Loading API specifications...")
	services, err := spec.LoadServices(ctx, cfg, logger, redactor)
	if err != nil {
		return fmt.Errorf("load services: %w", err)
	}
	logger.Printf("✓ Loaded %d services", len(services))

	// Build MCP registry
	logger.Printf("🔨 Building MCP tool registry...")
	registry, err := mcp.NewRegistry(services)
	if err != nil {
		return fmt.Errorf("build registry: %w", err)
	}
	logger.Printf("✓ Registered %d tools", len(registry.Tools))
	logger.Printf("✓ Registered %d resources", len(registry.Resources))

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
			logger.Printf("Error encoding response: %v", err)
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

	// HTTP server config
	httpServer := &http.Server{
		Addr:         listenAddr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	logger.Printf("")
	logger.Printf("✅ Server initialized successfully")
	logger.Printf("📡 MCP Endpoint: http://%s/mcp/v1", listenAddr)
	logger.Printf("🏥 Health Check: http://%s/healthz", listenAddr)
	logger.Printf("")
	logger.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	logger.Printf("")

	// Start server
	return httpServer.ListenAndServe()
}

// runSTDIO runs the MCP server in STDIO mode for Claude Desktop integration
func runSTDIO(configPathArg string, logger *log.Logger) error {
	ctx := context.Background()

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
	logger.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	logger.Printf("🚀 Skyline MCP Server v%s - STDIO Mode", Version)
	logger.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	logger.Printf("Config: %s", configPath)
	logger.Printf("APIs: %d configured", len(cfg.APIs))
	logger.Printf("Transport: STDIO (stdin/stdout)")
	logger.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	logger.Printf("")

	// Load services from API specs
	logger.Printf("📚 Loading API specifications...")
	services, err := spec.LoadServices(ctx, cfg, logger, redactor)
	if err != nil {
		return fmt.Errorf("load services: %w", err)
	}
	logger.Printf("✓ Loaded %d services", len(services))

	// Build MCP registry
	logger.Printf("🔨 Building MCP tool registry...")
	registry, err := mcp.NewRegistry(services)
	if err != nil {
		return fmt.Errorf("build registry: %w", err)
	}
	logger.Printf("✓ Registered %d tools", len(registry.Tools))
	logger.Printf("✓ Registered %d resources", len(registry.Resources))

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
		logger.Printf("Warning: code execution setup failed: %v", err)
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
		logger.Printf("✓ Code execution enabled (goja)")
	}

	logger.Printf("")
	logger.Printf("✅ Server initialized successfully")
	logger.Printf("📡 Ready for MCP protocol over STDIO")
	logger.Printf("")
	logger.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	logger.Printf("")

	// Run server in STDIO mode (stdin → stdout)
	if err := mcpServer.Serve(ctx, os.Stdin, os.Stdout); err != nil {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}
