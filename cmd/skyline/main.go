package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"skyline-mcp/internal/canonical"
	"skyline-mcp/internal/codegen"
	"skyline-mcp/internal/config"
	"skyline-mcp/internal/executor"
	"skyline-mcp/internal/gateway"
	"skyline-mcp/internal/mcp"
	"skyline-mcp/internal/redact"
	"skyline-mcp/internal/runtime"
	"skyline-mcp/internal/spec"
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

	// Create debug log file
	debugLog, err := os.OpenFile("/tmp/mcp-debug.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		defer debugLog.Close()
		fmt.Fprintf(debugLog, "\n=== MCP Server Started at %s ===\n", time.Now().Format(time.RFC3339))
		fmt.Fprintf(debugLog, "PID: %d\n", os.Getpid())
	}

	logger := log.New(os.Stderr, "", log.LstdFlags)
	ctx := context.Background()

	// Check for gateway mode (env vars take precedence)
	gatewayURL := os.Getenv("CONFIG_SERVER_URL")
	gatewayProfile := os.Getenv("PROFILE_NAME")
	gatewayToken := os.Getenv("PROFILE_TOKEN")
	gatewayMode := gatewayURL != "" && gatewayProfile != "" && gatewayToken != ""

	var registry *mcp.Registry
	var executor mcp.Executor
	redactor := redact.NewRedactor()

	if gatewayMode {
		// GATEWAY MODE: Connect to config server via WebSocket
		logger.Printf("gateway mode: connecting to %s (profile: %s)", gatewayURL, gatewayProfile)

		gatewayClient := gateway.NewClient(gatewayURL, gatewayProfile, gatewayToken)

		// Establish WebSocket connection
		if err := gatewayClient.ConnectWebSocket(ctx); err != nil {
			logger.Fatalf("failed to connect websocket: %v", err)
		}
		logger.Printf("websocket connected to gateway")

		// Fetch tools from gateway via WebSocket
		tools, err := gatewayClient.FetchToolsWebSocket(ctx)
		if err != nil {
			logger.Fatalf("failed to fetch tools from gateway: %v", err)
		}
		logger.Printf("loaded %d tools from gateway", len(tools))

		// Convert gateway.Tool to mcp.GatewayTool
		mcpTools := make([]mcp.GatewayTool, len(tools))
		for i, t := range tools {
			mcpTools[i] = mcp.GatewayTool{
				Name:         t.Name,
				Description:  t.Description,
				InputSchema:  t.InputSchema,
				OutputSchema: t.OutputSchema,
			}
		}

		// Build registry from gateway tools
		registry = mcp.NewRegistryFromGatewayTools(mcpTools)
		logger.Printf("registry ready (%d tools)", len(registry.Tools))

		// Create gateway executor with WebSocket
		executor = &gatewayExecutor{
			client:    gatewayClient,
			logger:    logger,
			useWebSocket: true,
		}

		if debugLog != nil {
			fmt.Fprintf(debugLog, "Gateway mode initialized, WebSocket connected\n")
		}

	} else {
		// STANDALONE MODE: Direct API access (backward compatible)
		logger.Printf("standalone mode: loading specs directly")

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
				// If config file doesn't exist, start with empty config
				if os.IsNotExist(err) {
					logger.Printf("config file not found (%s), starting with empty profile", *configPath)
					cfg = &config.Config{APIs: []config.APIConfig{}}
					cfg.ApplyDefaults()
				} else {
					logger.Fatalf("config load: %v", err)
				}
			}
		}

		redactor.AddSecrets(cfg.Secrets())

		logger.Printf("loading specs...")
		services, err := spec.LoadServices(ctx, cfg, logger, redactor)
		if err != nil {
			logger.Fatalf("spec load: %v", err)
		}
		logger.Printf("loaded %d services", len(services))

		standaloneExecutor, err := runtime.NewExecutor(cfg, services, logger, redactor)
		if err != nil {
			logger.Fatalf("executor init: %v", err)
		}
		executor = standaloneExecutor

		logger.Printf("building registry...")
		registry, err = mcp.NewRegistry(services)
		if err != nil {
			logger.Fatalf("registry init: %v", err)
		}
		logger.Printf("registry ready (%d tools)", len(registry.Tools))
	}

	server := mcp.NewServer(registry, executor, logger, redactor)
	
	// Setup code execution (optional, only if Deno is available)
	if codeExec, err := setupCodeExecution(registry, logger); err != nil {
		logger.Printf("WARNING: Code execution setup failed: %v", err)
	} else if codeExec != nil {
		server.SetCodeExecutor(codeExec)
	}
	
	if debugLog != nil {
		fmt.Fprintf(debugLog, "Starting MCP server with transport=%s\n", *transport)
	}
	switch *transport {
	case "stdio":
		if debugLog != nil {
			fmt.Fprintf(debugLog, "About to call server.Serve() on stdio\n")
		}
		if err := server.Serve(ctx, os.Stdin, os.Stdout); err != nil {
			if debugLog != nil {
				fmt.Fprintf(debugLog, "server.Serve() returned with error: %v\n", err)
			}
			logger.Fatalf("server error: %v", err)
		}
		if debugLog != nil {
			fmt.Fprintf(debugLog, "server.Serve() returned successfully (EOF on stdin)\n")
		}
	case "http", "streamable-http":
		// New Streamable HTTP transport (MCP spec 2025-11-25)
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
				logger.Fatalf("auth validation: %v", err)
			}
			redactor.AddSecrets(authSecrets(auth))
		}
		logger.Printf("Skyline MCP server (Streamable HTTP) listening on http://%s", *listen)
		streamableServer := mcp.NewStreamableHTTPServer(server, logger, auth)
		httpServer := &http.Server{
			Addr:    *listen,
			Handler: streamableServer.Handler(),
		}
		go func() {
			<-ctx.Done()
			_ = httpServer.Shutdown(context.Background())
		}()
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("server error: %v", err)
		}
	case "sse":
		// Legacy HTTP+SSE transport (deprecated, backwards compatibility only)
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
				logger.Fatalf("auth validation: %v", err)
			}
			redactor.AddSecrets(authSecrets(auth))
		}
		logger.Printf("Skyline MCP server (legacy HTTP+SSE) listening on http://%s", *listen)
		logger.Printf("WARNING: HTTP+SSE transport is deprecated, use --transport=http for Streamable HTTP")
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

// gatewayExecutor implements mcp.Executor for gateway mode
type gatewayExecutor struct {
	client       *gateway.Client
	logger       *log.Logger
	useWebSocket bool
}

func (e *gatewayExecutor) Execute(ctx context.Context, op *canonical.Operation, args map[string]any) (*runtime.Result, error) {
	if op == nil {
		return nil, fmt.Errorf("operation is nil")
	}
	e.logger.Printf("executing via gateway: %s", op.ToolName)

	var gwResult *gateway.Result
	var err error

	if e.useWebSocket {
		// Call gateway via WebSocket
		gwResult, err = e.client.ExecuteWebSocket(ctx, op.ToolName, args)
	} else {
		// Call gateway via HTTP (fallback)
		gwResult, err = e.client.Execute(ctx, op.ToolName, args)
	}

	if err != nil {
		return nil, err
	}

	// Convert gateway.Result to runtime.Result
	return &runtime.Result{
		Status:      gwResult.Status,
		ContentType: gwResult.ContentType,
		Body:        gwResult.Body,
	}, nil
}

// setupCodeExecution sets up code execution for the MCP server
func setupCodeExecution(registry *mcp.Registry, logger *log.Logger) (*executor.Executor, error) {
	return codegen.SetupCodeExecution(registry, logger)
}
