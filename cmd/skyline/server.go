package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/term"

	"skyline-mcp/internal/audit"
	"skyline-mcp/internal/mcp"
	"skyline-mcp/internal/metrics"
	"skyline-mcp/internal/oauth"
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
	flag.Parse()

	logger := log.New(os.Stderr, "", log.LstdFlags)

	// Handle version flag (both -v and --version)
	if *versionFlag || *versionShort {
		showVersion()
		os.Exit(0)
	}

	// Handle update command
	if len(flag.Args()) > 0 && flag.Args()[0] == "update" {
		if err := runUpdate(logger); err != nil {
			logger.Fatalf("update failed: %v", err)
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
			logger.Fatalf("env file: %v", err)
		}
	}

	// Handle STDIO transport mode early (before profile/encryption logic)
	if *transport == "stdio" {
		if err := runSTDIO(*configPath, logger); err != nil {
			logger.Fatalf("STDIO mode error: %v", err)
		}
		return
	}

	// Handle HTTP transport mode with direct config (skip profile logic)
	if *transport == "http" && *configPath != "" {
		if err := runHTTPWithConfig(*configPath, *bind, *admin, logger); err != nil {
			logger.Fatalf("HTTP mode error: %v", err)
		}
		return
	}

	// Validate transport
	if *transport != "http" {
		logger.Fatalf("unsupported transport: %s (only 'http' and 'stdio' supported)", *transport)
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
			logger.Printf("")
			logger.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			logger.Printf("🔐 Encrypted profiles file found")
			logger.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			logger.Printf("")
			logger.Printf("An encrypted profiles file exists at:")
			logger.Printf("  %s", tempProfilesPath)
			logger.Printf("")
			logger.Printf("This file contains your API credentials and cannot be accessed")
			logger.Printf("without the encryption key.")
			logger.Printf("")
			logger.Printf("❌ SKYLINE_PROFILES_KEY environment variable is not set")
			logger.Printf("")
			logger.Printf("To decrypt your profiles, you need to set the encryption key:")
			logger.Printf("")
			logger.Printf("  1. If you saved the key to ~/.skyline/skyline.env:")
			logger.Printf("     source ~/.skyline/skyline.env")
			logger.Printf("     skyline")
			logger.Printf("")
			logger.Printf("  2. If you have the key in a password manager:")
			logger.Printf("     export SKYLINE_PROFILES_KEY=<your-64-char-hex-key>")
			logger.Printf("     skyline")
			logger.Printf("")
			logger.Printf("  3. If you lost the key:")
			logger.Printf("     ⚠️  Your profiles are permanently encrypted")
			logger.Printf("     You'll need to delete the file and start fresh:")
			logger.Printf("     rm %s", tempProfilesPath)
			logger.Printf("")
			logger.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			logger.Printf("")
			logger.Fatalf("Encryption key required to decrypt profiles file")
		}

		// No key and no file - check if running interactively
		// SECURITY: Never generate keys in service/non-interactive mode (prevents keys in logs)
		if !term.IsTerminal(int(os.Stdout.Fd())) {
			logger.Printf("")
			logger.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			logger.Printf("🔐 Encryption key required")
			logger.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			logger.Printf("")
			logger.Printf("Running in non-interactive mode (service/background).")
			logger.Printf("Encryption key must be configured before starting service.")
			logger.Printf("")
			logger.Printf("To set up encryption:")
			logger.Printf("")
			logger.Printf("  1. Generate a key:")
			logger.Printf("     openssl rand -hex 32")
			logger.Printf("")
			logger.Printf("  2. Save to ~/.skyline/skyline.env:")
			logger.Printf("     echo 'SKYLINE_PROFILES_KEY=<your-key>' > ~/.skyline/skyline.env")
			logger.Printf("     chmod 600 ~/.skyline/skyline.env")
			logger.Printf("")
			logger.Printf("  3. Restart the service:")
			logger.Printf("     systemctl --user restart skyline")
			logger.Printf("")
			logger.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			logger.Printf("")
			logger.Fatalf("Encryption key generation requires interactive terminal (security measure)")
		}

		// Interactive mode - generate new key
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			logger.Fatalf("failed to generate encryption key: %v", err)
		}
		keyHex := hex.EncodeToString(key)

		// Determine skyline.env path
		home, err := os.UserHomeDir()
		if err != nil {
			logger.Fatalf("get home dir: %v", err)
		}
		skylineDir := filepath.Join(home, ".skyline")
		envPath := filepath.Join(skylineDir, "skyline.env")

		// Create .skyline directory if it doesn't exist
		if err := os.MkdirAll(skylineDir, 0o755); err != nil {
			logger.Fatalf("create .skyline dir: %v", err)
		}

		// Save key to skyline.env
		envContent := fmt.Sprintf("export SKYLINE_PROFILES_KEY=%s\n", keyHex)
		if err := os.WriteFile(envPath, []byte(envContent), 0o600); err != nil {
			logger.Fatalf("save encryption key: %v", err)
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
			logger.Fatalf("invalid encryption key in %s: %v", *keyEnv, err)
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
		logger.Fatalf("unsupported auth mode %q", *authMode)
	}

	// Initialize audit logger
	auditLogger, err := audit.NewLogger("./skyline-audit.db")
	if err != nil {
		logger.Fatalf("init audit logger: %v", err)
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
			logger.Fatalf("get home dir: %v", err)
		}
		serverConfigPath = filepath.Join(home, ".skyline", "config.yaml")
	}

	// Load server configuration
	serverCfg, err := serverconfig.Load(serverConfigPath)
	if err != nil {
		logger.Fatalf("load server config: %v", err)
	}

	// Check if config file exists, if not create default
	if _, err := os.Stat(serverConfigPath); os.IsNotExist(err) {
		logger.Printf("Config file not found, creating default: %s", serverConfigPath)
		if err := serverconfig.GenerateDefault(serverConfigPath); err != nil {
			logger.Printf("Warning: could not create default config: %v", err)
		} else {
			logger.Printf("✓ Created default config.yaml")
		}
	}

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
		logger.Fatalf("tls setup: %v", err)
	}

	// Create TCP listener with same-port HTTP→HTTPS redirect
	tcpLn, err := net.Listen("tcp", listenAddr)
	if err != nil {
		logger.Fatalf("listen: %v", err)
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

	// Re-initialize audit logger with config path
	auditLogger.Close()
	auditLogger, err = audit.NewLogger(auditDBPath)
	if err != nil {
		logger.Fatalf("init audit logger: %v", err)
	}
	defer auditLogger.Close()

	// Set log level from config
	setLogLevel(logger, serverCfg.Logging.Level)

	// Use persisted admin token from config, or generate and save one
	adminToken := serverCfg.Server.AdminToken
	if adminToken == "" {
		adminTokenRaw := make([]byte, 16)
		if _, randErr := rand.Read(adminTokenRaw); randErr != nil {
			logger.Fatalf("generate admin token: %v", randErr)
		}
		adminToken = hex.EncodeToString(adminTokenRaw)
		serverCfg.Server.AdminToken = adminToken
		if err := serverconfig.InjectAdminToken(serverConfigPath, adminToken); err != nil {
			logger.Printf("Warning: could not persist admin token to config: %v", err)
		} else {
			logger.Printf("✓ Generated and saved admin token to config")
		}
	}

	logger.Printf("Skyline MCP Server starting...")
	logger.Printf("  Transport: %s", *transport)
	logger.Printf("  Admin UI: %v", *admin)
	logger.Printf("  Listen: %s", listenAddr)
	logger.Printf("  Config: %s", serverConfigPath)
	logger.Printf("  Profiles: %s", profilesPath)
	logger.Printf("  Audit DB: %s", auditDBPath)
	logger.Printf("  Code Execution: %v", serverCfg.Runtime.CodeExecution.Enabled)
	logger.Printf("  Cache: %v", serverCfg.Runtime.Cache.Enabled)
	logger.Printf("  Log Level: %s", serverCfg.Logging.Level)

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
	}

	// Initialize cache if enabled in config
	if serverCfg.Runtime.Cache.Enabled {
		s.cache = newProfileCache(serverCfg.Runtime.Cache.TTL)
		logger.Printf("  Cache TTL: %s", serverCfg.Runtime.Cache.TTL)
	}

	// Start metrics remote write if configured
	if rw := serverCfg.Metrics.RemoteWrite; rw != nil && rw.Endpoint != "" {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		metricsCollector.StartRemoteWrite(ctx, rw.Endpoint, rw.Interval, rw.Username, rw.Password, logger)
		logger.Printf("  Metrics remote write: %s (every %s)", rw.Endpoint, rw.Interval)
	}

	// Try to load existing profiles
	if err := s.load(); err != nil {
		// If profile exists but decryption failed, show helpful error
		if profileExists && keyRaw != "" {
			absPath, _ := filepath.Abs(profilesPath)
			logger.Printf("")
			logger.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			logger.Printf("❌ Failed to decrypt profiles file")
			logger.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			logger.Printf("")
			logger.Printf("The encryption key is invalid for this profiles file.")
			logger.Printf("")
			logger.Printf("📁 Profiles file:")
			logger.Printf("   %s", absPath)
			logger.Printf("")
			logger.Printf("🔑 Key used (from %s):", *keyEnv)
			logger.Printf("   %s...%s", keyRaw[:16], keyRaw[len(keyRaw)-16:])
			logger.Printf("")
			logger.Printf("💡 Options:")
			logger.Printf("   • Use the correct key for this file")
			logger.Printf("   • Delete the file to start fresh (data will be lost)")
			logger.Printf("   • Restore from backup")
			logger.Printf("")
			logger.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			logger.Printf("")
			logger.Fatalf("Error: %v", err)
		}
		logger.Fatalf("load store: %v", err)
	}

	// If profiles file doesn't exist, create an empty encrypted one
	if !profileExists {
		if err := s.save(); err != nil {
			logger.Printf("Warning: could not create profiles file: %v", err)
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
			logger.Fatalf("ui fs: %v", err)
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
		Handler:      logRequests(mux, logger),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	logger.Printf("")
	logger.Printf("✓ Skyline MCP Server ready (HTTPS)")
	if *admin {
		logger.Printf("  Admin token: %s", adminToken)
		logger.Printf("  → https://%s", listenAddr)
	}
	logger.Printf("")

	if err := httpServer.ServeTLS(ln, tlsCertPath, tlsKeyPath); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Fatalf("server error: %v", err)
	}
}

func logRequests(next http.Handler, logger *log.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		logger.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
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
