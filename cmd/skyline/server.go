package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/term"

	"skyline-mcp/internal/audit"
	"skyline-mcp/internal/logging"
	"skyline-mcp/internal/mcp"
	"skyline-mcp/internal/metrics"
	"skyline-mcp/internal/oauth"
	"skyline-mcp/internal/ratelimit"
	"skyline-mcp/internal/redact"
	"skyline-mcp/internal/serverconfig"
)

//go:embed ui/*
var uiFiles embed.FS

func main() {
	transport := flag.String("transport", "http", "Transport mode: stdio, http")
	admin := flag.Bool("admin", true, "Enable Web UI and admin dashboard (only for http transport)")
	bind := flag.String("bind", "localhost:8191", "Network interface and port to bind to (e.g., localhost:8191 or 0.0.0.0:8191)")
	storagePath := flag.String("storage", "./profiles.enc.yaml", "Encrypted profiles storage path")
	configPath := flag.String("config", "", "Server config.yaml path (default: ~/.skyline/config.yaml)")
	authMode := flag.String("auth-mode", "bearer", "Auth mode: none or bearer")
	keyEnv := flag.String("key-env", "SKYLINE_PROFILES_KEY", "Env var name containing encryption key")
	envFile := flag.String("env-file", "", "Optional env file to load before startup")
	versionFlag := flag.Bool("version", false, "Show version information")
	versionShort := flag.Bool("v", false, "Show version information (shorthand)")
	validateFlag := flag.Bool("validate", false, "Validate encrypted profiles file can be decrypted")
	initProfilesFlag := flag.Bool("init-profiles", false, "Generate new encrypted profiles file")
	keyFlag := flag.String("key", "", "Encryption key (overrides env var)")
	logFormat := flag.String("log-format", "text", "Log output format: text, json")
	logLevel := flag.String("log-level", "info", "Log level: debug, info, warn, error")
	flag.Parse()

	logger := logging.Setup(*logFormat, *logLevel)

	// Handle version flag (both -v and --version)
	if *versionFlag || *versionShort {
		showVersion()
		os.Exit(0)
	}

	// Handle update command
	if len(flag.Args()) > 0 && flag.Args()[0] == "update" {
		if err := runUpdate(logger); err != nil {
			slog.Error("update failed", "error", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	// Handle --validate flag
	if *validateFlag {
		exitCode := runValidate(*storagePath, *keyFlag, *keyEnv, logger)
		os.Exit(exitCode)
	}

	// Handle --init-profiles flag
	if *initProfilesFlag {
		exitCode := runInitProfiles(*storagePath, *keyFlag, *keyEnv, logger)
		os.Exit(exitCode)
	}

	if *envFile != "" {
		if err := loadEnvFile(*envFile); err != nil {
			slog.Error("env file error", "error", err)
			os.Exit(1)
		}
	}

	// Handle STDIO transport mode early (before profile/encryption logic)
	if *transport == "stdio" {
		if err := runSTDIO(*configPath, logger); err != nil {
			slog.Error("STDIO mode error", "error", err)
			os.Exit(1)
		}
		return
	}

	// Handle HTTP transport mode with direct config (skip profile logic)
	if *transport == "http" && *configPath != "" {
		if err := runHTTPWithConfig(*configPath, *bind, *admin, logger); err != nil {
			slog.Error("HTTP mode error", "error", err)
			os.Exit(1)
		}
		return
	}

	// Validate transport
	if *transport != "http" {
		slog.Error("unsupported transport", "transport", *transport)
		os.Exit(1)
	}

	// From here on: HTTP mode with profile-based system
	// Determine profiles path early to check if file exists
	tempProfilesPath := *storagePath
	if tempProfilesPath == "./profiles.enc.yaml" {
		home, err := os.UserHomeDir()
		if err == nil {
			tempProfilesPath = filepath.Join(home, ".skyline", "profiles.enc.yaml")
		}
	}

	// Check if profiles file exists BEFORE loading config
	profilesFileExists := fileExists(tempProfilesPath)

	// Check if encryption key is set
	keyRaw := os.Getenv(*keyEnv)
	var key []byte
	var err error
	var keyGenerated bool
	var envFileCreated bool

	if keyRaw == "" {
		// No encryption key set
		if profilesFileExists {
			// Encrypted profiles file exists - need key to decrypt it
			slog.Error("encrypted profiles file found but no key set",
				"path", tempProfilesPath,
				"hint", "set SKYLINE_PROFILES_KEY environment variable")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Fprintln(os.Stderr, "🔐 Encrypted profiles file found")
			fmt.Fprintln(os.Stderr, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintf(os.Stderr, "An encrypted profiles file exists at:\n  %s\n", tempProfilesPath)
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "❌ SKYLINE_PROFILES_KEY environment variable is not set")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "To decrypt your profiles, set the encryption key:")
			fmt.Fprintln(os.Stderr, "  1. source ~/.skyline/skyline.env && skyline")
			fmt.Fprintln(os.Stderr, "  2. export SKYLINE_PROFILES_KEY=<your-64-char-hex-key> && skyline")
			fmt.Fprintf(os.Stderr, "  3. If lost: rm %s\n", tempProfilesPath)
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			os.Exit(1)
		}

		// No key and no file - check if running interactively
		// SECURITY: Never generate keys in service/non-interactive mode (prevents keys in logs)
		if !term.IsTerminal(int(os.Stdout.Fd())) {
			slog.Error("encryption key required in non-interactive mode",
				"hint", "set SKYLINE_PROFILES_KEY before starting service")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Fprintln(os.Stderr, "🔐 Encryption key required")
			fmt.Fprintln(os.Stderr, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "Running in non-interactive mode (service/background).")
			fmt.Fprintln(os.Stderr, "Encryption key must be configured before starting service.")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "  1. openssl rand -hex 32")
			fmt.Fprintln(os.Stderr, "  2. echo 'SKYLINE_PROFILES_KEY=<your-key>' > ~/.skyline/skyline.env")
			fmt.Fprintln(os.Stderr, "  3. systemctl --user restart skyline")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			os.Exit(1)
		}

		// Interactive mode - generate new key
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			slog.Error("failed to generate encryption key", "error", err)
			os.Exit(1)
		}
		keyHex := hex.EncodeToString(key)

		// Determine skyline.env path
		home, err := os.UserHomeDir()
		if err != nil {
			slog.Error("get home dir failed", "error", err)
			os.Exit(1)
		}
		skylineDir := filepath.Join(home, ".skyline")
		envPath := filepath.Join(skylineDir, "skyline.env")

		// Create .skyline directory if it doesn't exist
		if err := os.MkdirAll(skylineDir, 0o755); err != nil {
			slog.Error("create .skyline dir failed", "error", err)
			os.Exit(1)
		}

		// Save key to skyline.env
		envContent := fmt.Sprintf("export SKYLINE_PROFILES_KEY=%s\n", keyHex)
		if err := os.WriteFile(envPath, []byte(envContent), 0o600); err != nil {
			slog.Error("save encryption key failed", "error", err)
			os.Exit(1)
		}

		// Set the env var for this process
		os.Setenv(*keyEnv, keyHex)
		keyGenerated = true

		// Print to terminal ONLY (we confirmed we're interactive above)
		fmt.Println("")
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Println("🔑 Generated new encryption key")
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Println("")
		fmt.Printf("✓ Saved encryption key to: %s\n", envPath)
		fmt.Println("")
		fmt.Println("🔑 Your encryption key:")
		fmt.Println("")
		fmt.Printf("    %s\n", keyHex)
		fmt.Println("")
		fmt.Println("⚠️  CRITICAL: Save this key in a secure location!")
		fmt.Println("")
		fmt.Println("   • This key encrypts your API profiles (credentials, tokens)")
		fmt.Println("   • Without this key, your profiles CANNOT be decrypted")
		fmt.Println("   • If you lose this key, you will lose access to all profiles")
		fmt.Println("")
		fmt.Println("   The key has been saved to ~/.skyline/skyline.env")
		fmt.Println("")
		fmt.Println("   To use this key in future sessions:")
		fmt.Println("   1. Add to your shell profile (~/.bashrc or ~/.zshrc):")
		fmt.Printf("      export SKYLINE_PROFILES_KEY=%s\n", keyHex)
		fmt.Println("")
		fmt.Println("   2. Or load the env file each time:")
		fmt.Println("      source ~/.skyline/skyline.env")
		fmt.Println("")
		fmt.Println("   Recommended: Store a backup copy in your password manager!")
		fmt.Println("")
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Println("")
	} else {
		// Key is set - decode it
		key, err = decodeKey(keyRaw)
		if err != nil {
			slog.Error("invalid encryption key", "env", *keyEnv, "error", err)
			os.Exit(1)
		}

		// Ensure env file exists (may have been deleted or key set manually)
		home, homeErr := os.UserHomeDir()
		if homeErr == nil {
			envPath := filepath.Join(home, ".skyline", "skyline.env")
			if !fileExists(envPath) {
				_ = os.MkdirAll(filepath.Join(home, ".skyline"), 0o755)
				envContent := fmt.Sprintf("export SKYLINE_PROFILES_KEY=%s\n", keyRaw)
				if writeErr := os.WriteFile(envPath, []byte(envContent), 0o600); writeErr == nil {
					envFileCreated = true
				}
			}
		}
	}

	mode := strings.ToLower(strings.TrimSpace(*authMode))
	if mode != "none" && mode != "bearer" {
		slog.Error("unsupported auth mode", "mode", *authMode)
		os.Exit(1)
	}

	// Initialize audit logger (temporary — re-initialized with config path below)
	auditLogger, err := audit.NewLogger("./skyline-audit.db", 0)
	if err != nil {
		slog.Error("init audit logger failed", "error", err)
		os.Exit(1)
	}
	defer auditLogger.Close()

	// Initialize metrics collector
	metricsCollector := metrics.NewCollector()

	// Determine config file path
	serverConfigPath := *configPath
	if serverConfigPath == "" {
		// Default to ~/.skyline/config.yaml
		home, err := os.UserHomeDir()
		if err != nil {
			slog.Error("get home dir failed", "error", err)
			os.Exit(1)
		}
		serverConfigPath = filepath.Join(home, ".skyline", "config.yaml")
	}

	// Load server configuration
	serverCfg, err := serverconfig.Load(serverConfigPath)
	if err != nil {
		slog.Error("load server config failed", "error", err)
		os.Exit(1)
	}

	// Check if config file exists, if not create default
	if _, err := os.Stat(serverConfigPath); os.IsNotExist(err) {
		slog.Info("config file not found, creating default", "path", serverConfigPath)
		if err := serverconfig.GenerateDefault(serverConfigPath); err != nil {
			slog.Warn("could not create default config", "error", err)
		} else {
			slog.Info("created default config.yaml")
		}
	}

	// Apply log level/format from server config if not overridden by CLI flags
	cfgLogLevel := serverCfg.Logging.Level
	cfgLogFormat := serverCfg.Logging.Format
	// CLI flags take precedence; if they were explicitly set, the logger is already
	// configured. If not, re-configure from config file.
	if *logLevel == "info" && cfgLogLevel != "" && cfgLogLevel != "info" {
		*logLevel = cfgLogLevel
	}
	if *logFormat == "text" && cfgLogFormat != "" && cfgLogFormat != "text" {
		*logFormat = cfgLogFormat
	}
	// Re-setup logger with final format/level (from config or CLI)
	logger = logging.Setup(*logFormat, *logLevel)

	// Apply configuration
	// Override bind address if set via command line flag
	const defaultBind = "localhost:8191"
	listenAddr := *bind
	if listenAddr == defaultBind && serverCfg.Server.Listen != "" {
		// Use config file value if command line is default
		listenAddr = serverCfg.Server.Listen
	}

	// If the address has no port (e.g. "0.0.0.0"), append the default port
	if _, _, err := net.SplitHostPort(listenAddr); err != nil {
		_, defaultPort, _ := net.SplitHostPort(defaultBind)
		listenAddr = listenAddr + ":" + defaultPort
	}

	// TLS setup — always enabled, auto-generates self-signed cert if needed
	var tlsCertPath, tlsKeyPath string
	if serverCfg.Server.TLS != nil {
		tlsCertPath = serverCfg.Server.TLS.Cert
		tlsKeyPath = serverCfg.Server.TLS.Key
	}
	tlsHost, _, _ := net.SplitHostPort(listenAddr)
	if tlsHost == "" {
		tlsHost = "localhost"
	}
	tlsCertPath, tlsKeyPath, err = ensureTLSCert(tlsCertPath, tlsKeyPath, []string{tlsHost, "localhost", "127.0.0.1", "::1"}, logger)
	if err != nil {
		slog.Error("tls setup failed", "error", err)
		os.Exit(1)
	}

	// Create TCP listener with same-port HTTP→HTTPS redirect
	tcpLn, err := net.Listen("tcp", listenAddr)
	if err != nil {
		slog.Error("listen failed", "addr", listenAddr, "error", err)
		os.Exit(1)
	}
	ln := &tlsRedirectListener{Listener: tcpLn, httpsHost: listenAddr}

	// Override storage path from config if not set via flag
	profilesPath := *storagePath
	if profilesPath == "./profiles.enc.yaml" && serverCfg.Profiles.Storage != "" {
		expandedPath, err := serverconfig.ExpandPath(serverCfg.Profiles.Storage)
		if err == nil {
			profilesPath = expandedPath
		}
	}

	// Check if profiles file exists at final path
	profileExists := fileExists(profilesPath)

	// Expand audit database path from config
	auditDBPath := "./skyline-audit.db"
	if serverCfg.Audit.Database != "" {
		expandedPath, err := serverconfig.ExpandPath(serverCfg.Audit.Database)
		if err == nil {
			auditDBPath = expandedPath
		}
	}

	// Re-initialize audit logger with config path and rotation setting
	auditLogger.Close()
	auditLogger, err = audit.NewLogger(auditDBPath, serverCfg.Audit.RotateAfter)
	if err != nil {
		slog.Error("init audit logger failed", "error", err)
		os.Exit(1)
	}
	defer auditLogger.Close()

	// Use persisted admin token from config, or generate and save one
	adminToken := serverCfg.Server.AdminToken
	if adminToken == "" {
		adminTokenRaw := make([]byte, 16)
		if _, randErr := rand.Read(adminTokenRaw); randErr != nil {
			slog.Error("generate admin token failed", "error", randErr)
			os.Exit(1)
		}
		adminToken = hex.EncodeToString(adminTokenRaw)
		serverCfg.Server.AdminToken = adminToken
		if err := serverconfig.InjectAdminToken(serverConfigPath, adminToken); err != nil {
			slog.Warn("could not persist admin token to config", "error", err)
		} else {
			slog.Info("generated and saved admin token to config")
		}
	}

	slog.Info("Skyline MCP Server starting",
		"transport", *transport,
		"admin", *admin,
		"listen", listenAddr,
		"config", serverConfigPath,
		"profiles", profilesPath,
		"audit_db", auditDBPath,
		"code_execution", serverCfg.Runtime.CodeExecution.Enabled,
		"cache", serverCfg.Runtime.Cache.Enabled,
		"log_level", *logLevel,
		"log_format", *logFormat,
	)

	s := &server{
		path:           profilesPath,
		configPath:     serverConfigPath,
		serverCfg:      serverCfg,
		key:            key,
		authMode:       mode,
		adminToken:     adminToken,
		logger:         logger,
		redactor:       redact.NewRedactor(),
		auditLogger:    auditLogger,
		metrics:        metricsCollector,
		sessionTracker: mcp.NewSessionTracker(),
		agentHub:       audit.NewGenericHub(),
		oauthStore:     oauth.NewStore(),
		detectLimiter:  ratelimit.New(5, 0, 0), // 5 requests per minute for detect endpoint
		verifyLimiter:  ratelimit.New(5, 0, 0), // 5 requests per minute for verify endpoint
	}

	// Initialize cache if enabled in config
	if serverCfg.Runtime.Cache.Enabled {
		s.cache = newProfileCache(serverCfg.Runtime.Cache.TTL)
		slog.Info("cache enabled", "ttl", serverCfg.Runtime.Cache.TTL)
	}

	// Start metrics remote write if configured
	if rw := serverCfg.Metrics.RemoteWrite; rw != nil && rw.Endpoint != "" {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		metricsCollector.StartRemoteWrite(ctx, rw.Endpoint, rw.Interval, rw.Username, rw.Password, logger)
		slog.Info("metrics remote write enabled", "endpoint", rw.Endpoint, "interval", rw.Interval)
	}

	// Try to load existing profiles
	if err := s.load(); err != nil {
		// If profile exists but decryption failed, show helpful error
		if profileExists && keyRaw != "" {
			absPath, _ := filepath.Abs(profilesPath)
			slog.Error("failed to decrypt profiles file",
				"path", absPath,
				"key_env", *keyEnv,
				"error", err,
			)
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Fprintln(os.Stderr, "❌ Failed to decrypt profiles file")
			fmt.Fprintln(os.Stderr, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "The encryption key is invalid for this profiles file.")
			fmt.Fprintf(os.Stderr, "\n📁 Profiles file:\n   %s\n", absPath)
			fmt.Fprintf(os.Stderr, "\n🔑 Key used (from %s):\n   %s...%s\n", *keyEnv, keyRaw[:16], keyRaw[len(keyRaw)-16:])
			fmt.Fprintln(os.Stderr, "\n💡 Options:")
			fmt.Fprintln(os.Stderr, "   • Use the correct key for this file")
			fmt.Fprintln(os.Stderr, "   • Delete the file to start fresh (data will be lost)")
			fmt.Fprintln(os.Stderr, "   • Restore from backup")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			os.Exit(1)
		}
		slog.Error("load store failed", "error", err)
		os.Exit(1)
	}

	// If profiles file doesn't exist, create an empty encrypted one
	if !profileExists {
		if err := s.save(); err != nil {
			slog.Warn("could not create profiles file", "error", err)
		}

		// Show first-run status if key wasn't just generated (that case already showed its message)
		if !keyGenerated {
			absPath, _ := filepath.Abs(profilesPath)
			fmt.Println("")
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Println("🚀 First run setup")
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Println("")
			fmt.Println("✓ Encryption key found in environment (SKYLINE_PROFILES_KEY)")
			fmt.Printf("✓ Created new encrypted profiles file: %s\n", absPath)
			if envFileCreated {
				fmt.Println("✓ Persisted encryption key to ~/.skyline/skyline.env")
			}
			fmt.Println("")
			fmt.Println("   Your server is ready. Add API profiles via the Web UI")
			fmt.Println("   or the REST API at /profiles.")
			fmt.Println("")
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Println("")
		}
	}

	// HTTP transport mode - profile-based system
	mux := http.NewServeMux()

	// Admin/UI routes (only if --admin flag is set)
	if *admin {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}
			http.Redirect(w, r, "/admin/", http.StatusFound)
		})
		uiFS, err := fs.Sub(uiFiles, "ui")
		if err != nil {
			slog.Error("ui fs error", "error", err)
			os.Exit(1)
		}
		mux.Handle("/ui/", http.StripPrefix("/ui/", http.FileServer(http.FS(uiFS))))
		mux.HandleFunc("/admin", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/admin/", http.StatusFound)
		})
		mux.HandleFunc("/admin/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/admin/" || r.URL.Path == "/admin" {
				data, err := uiFiles.ReadFile("ui/admin.html")
				if err != nil {
					http.Error(w, "admin page not available", http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Write(data)
			}
		})

		// Admin endpoints
		mux.HandleFunc("/admin/auth", s.handleAdminAuth)
		mux.HandleFunc("/admin/metrics", s.handleMetrics)
		mux.HandleFunc("/admin/audit", s.handleAudit)
		mux.HandleFunc("/admin/stats", s.handleStats)
		mux.HandleFunc("/admin/config", s.handleConfig)
		mux.HandleFunc("/admin/sessions", s.handleSessions)
		mux.HandleFunc("/admin/events", s.handleEventStream)
	} else {
		// Simple health check if no admin
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Skyline MCP Server\n"))
		})
	}
	// API endpoints (always available)
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/profiles", s.handleProfiles)
	mux.HandleFunc("/profiles/", s.handleProfileRoute)
	mux.HandleFunc("/detect", s.handleDetect)
	mux.HandleFunc("/verify", s.handleVerify)
	mux.HandleFunc("/oauth/start", s.handleOAuthStart)
	mux.HandleFunc("/oauth/callback", s.handleOAuthCallback)
	mux.HandleFunc("/oauth/exchange", s.handleOAuthExchange)
	mux.HandleFunc("/test", s.handleTest)
	mux.HandleFunc("/operations", s.handleOperations)

	// OAuth 2.1 endpoints (for ChatGPT MCP compatibility)
	mux.HandleFunc("/.well-known/oauth-protected-resource", s.handleOAuthProtectedResource)
	mux.HandleFunc("/.well-known/oauth-authorization-server", s.handleOAuthAuthorizationServer)
	mux.HandleFunc("/oauth/register", s.handleOAuthRegister)
	mux.HandleFunc("/oauth/authorize", s.handleOAuthAuthorize)
	mux.HandleFunc("/oauth/token", s.handleOAuthToken)

	httpServer := &http.Server{
		Addr:         listenAddr,
		Handler:      recoverMiddleware(logRequests(mux, logger)),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	slog.Info("Skyline MCP Server ready (HTTPS)",
		"admin_token", adminToken[:8]+"...",
		"url", "https://"+listenAddr,
	)

	// Start graceful-shutdown listener in the background.
	go shutdownOnSignal([]*http.Server{httpServer}, func() {
		auditLogger.Close()
		slog.Debug("audit logger closed")
	})

	if err := httpServer.ServeTLS(ln, tlsCertPath, tlsKeyPath); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}

func logRequests(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Add security headers to non-MCP responses (browser-facing routes).
		// Skip MCP protocol endpoints so we don't interfere with API clients.
		if !strings.HasSuffix(r.URL.Path, "/mcp") {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("X-XSS-Protection", "1; mode=block")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			w.Header().Set("Content-Security-Policy",
				"default-src 'self'; "+
					"script-src 'self' 'unsafe-inline' 'unsafe-eval' https://code.iconify.design; "+
					"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; "+
					"img-src 'self' data: https:; "+
					"connect-src 'self' https://api.iconify.design; "+
					"font-src 'self' https://fonts.gstatic.com")
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		next.ServeHTTP(w, r)
		logger.Debug("http request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start))
	})
}

func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				slog.Error("panic recovered in HTTP handler", "error", err, "path", r.URL.Path, "method", r.Method)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

const maxBodySize = 1 << 20 // 1MB body size limit for API endpoints

// limitBody applies a 1MB body size limit to prevent abuse.
func limitBody(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
}

func (s *server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
