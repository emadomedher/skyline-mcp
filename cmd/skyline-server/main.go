package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"gopkg.in/yaml.v3"

	"skyline-mcp/internal/audit"
	"skyline-mcp/internal/canonical"
	"skyline-mcp/internal/config"
	"skyline-mcp/internal/mcp"
	"skyline-mcp/internal/metrics"
	"skyline-mcp/internal/parsers/graphql"
	"skyline-mcp/internal/parsers/openrpc"
	"skyline-mcp/internal/parsers/postman"
	"skyline-mcp/internal/redact"
	"skyline-mcp/internal/runtime"
	"skyline-mcp/internal/serverconfig"
	"skyline-mcp/internal/spec"
)

//go:embed ui/*
var uiFiles embed.FS

type envelope struct {
	Version    int    `yaml:"version"`
	Nonce      string `yaml:"nonce"`
	Ciphertext string `yaml:"ciphertext"`
}

type profileStore struct {
	Profiles []profile `yaml:"profiles"`
}

type profile struct {
	Name       string `yaml:"name" json:"name"`
	Token      string `yaml:"token" json:"token"`
	ConfigYAML string `yaml:"config_yaml" json:"config_yaml"`
}

type server struct {
	mu          sync.RWMutex
	store       profileStore
	path        string
	configPath  string
	serverCfg   *serverconfig.ServerConfig
	key         []byte
	authMode    string
	logger      *log.Logger
	redactor    *redact.Redactor
	auditLogger *audit.Logger
	metrics     *metrics.Collector
}

type upsertRequest struct {
	Token      string          `json:"token"`
	ConfigYAML string          `json:"config_yaml"`
	ConfigJSON json.RawMessage `json:"config_json"`
}

func main() {
	bind := flag.String("bind", "localhost:19190", "Network interface and port to bind to (e.g., localhost:19190 or 0.0.0.0:19190)")
	storagePath := flag.String("storage", "./profiles.enc.yaml", "Encrypted profiles storage path")
	configPath := flag.String("config", "", "Server config.yaml path (default: ~/.skyline/config.yaml)")
	authMode := flag.String("auth-mode", "bearer", "Auth mode: none or bearer")
	keyEnv := flag.String("key-env", "SKYLINE_PROFILES_KEY", "Env var name containing encryption key")
	envFile := flag.String("env-file", "", "Optional env file to load before startup")
	versionFlag := flag.Bool("version", false, "Show version information")
	versionShort := flag.Bool("v", false, "Show version information (shorthand)")
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

	if *envFile != "" {
		if err := loadEnvFile(*envFile); err != nil {
			logger.Fatalf("env file: %v", err)
		}
	}

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
			logger.Printf("     skyline-server")
			logger.Printf("")
			logger.Printf("  2. If you have the key in a password manager:")
			logger.Printf("     export SKYLINE_PROFILES_KEY=<your-64-char-hex-key>")
			logger.Printf("     skyline-server")
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
		
		// No key and no file - generate new key
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
		envContent := fmt.Sprintf("SKYLINE_PROFILES_KEY=%s\nCONFIG_SERVER_KEY=%s\n", keyHex, keyHex)
		if err := os.WriteFile(envPath, []byte(envContent), 0o600); err != nil {
			logger.Fatalf("save encryption key: %v", err)
		}
		
		// Set the env var for this session
		os.Setenv(*keyEnv, keyHex)
		
		logger.Printf("")
		logger.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		logger.Printf("🔑 Generated new encryption key")
		logger.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		logger.Printf("")
		logger.Printf("✓ Saved encryption key to: %s", envPath)
		logger.Printf("")
		logger.Printf("🔑 Your encryption key:")
		logger.Printf("")
		logger.Printf("    %s", keyHex)
		logger.Printf("")
		logger.Printf("⚠️  CRITICAL: Save this key in a secure location!")
		logger.Printf("")
		logger.Printf("   • This key encrypts your API profiles (credentials, tokens)")
		logger.Printf("   • Without this key, your profiles CANNOT be decrypted")
		logger.Printf("   • If you lose this key, you will lose access to all profiles")
		logger.Printf("")
		logger.Printf("   The key has been saved to ~/.skyline/skyline.env")
		logger.Printf("")
		logger.Printf("   To use this key in future sessions:")
		logger.Printf("   1. Add to your shell profile (~/.bashrc or ~/.zshrc):")
		logger.Printf("      export SKYLINE_PROFILES_KEY=%s", keyHex)
		logger.Printf("")
		logger.Printf("   2. Or load the env file each time:")
		logger.Printf("      source ~/.skyline/skyline.env")
		logger.Printf("")
		logger.Printf("   Recommended: Store a backup copy in your password manager!")
		logger.Printf("")
		logger.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		logger.Printf("")
	} else {
		// Key is set - decode it
		key, err = decodeKey(keyRaw)
		if err != nil {
			logger.Fatalf("invalid encryption key in %s: %v", *keyEnv, err)
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
	listenAddr := *bind
	if listenAddr == "localhost:19190" && serverCfg.Server.Listen != "" {
		// Use config file value if command line is default
		listenAddr = serverCfg.Server.Listen
	}

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

	logger.Printf("Skyline MCP Server starting...")
	logger.Printf("  Listen: %s", listenAddr)
	logger.Printf("  Config: %s", serverConfigPath)
	logger.Printf("  Profiles: %s", profilesPath)
	logger.Printf("  Audit DB: %s", auditDBPath)
	logger.Printf("  Code Execution: %v", serverCfg.Runtime.CodeExecution.Enabled)
	logger.Printf("  Cache: %v", serverCfg.Runtime.Cache.Enabled)
	logger.Printf("  Log Level: %s", serverCfg.Logging.Level)

	s := &server{
		path:        profilesPath,
		configPath:  serverConfigPath,
		serverCfg:   serverCfg,
		key:         key,
		authMode:    mode,
		logger:      logger,
		redactor:    redact.NewRedactor(),
		auditLogger: auditLogger,
		metrics:     metricsCollector,
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
		logger.Printf("Profiles file not found, creating empty encrypted file: %s", profilesPath)
		if err := s.save(); err != nil {
			logger.Printf("Warning: could not create profiles file: %v", err)
		} else {
			logger.Printf("✓ Created empty profiles file")
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/ui/", http.StatusFound)
	})
	mux.HandleFunc("/ui", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/", http.StatusFound)
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
			http.ServeFile(w, r, filepath.Join("cmd/skyline-server/ui/admin.html"))
		}
	})
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/profiles", s.handleProfiles)
	mux.HandleFunc("/profiles/", s.handleProfileOrGateway)
	mux.HandleFunc("/detect", s.handleDetect)
	mux.HandleFunc("/test", s.handleTest)
	mux.HandleFunc("/operations", s.handleOperations)

	// Admin endpoints for monitoring
	mux.HandleFunc("/admin/metrics", s.handleMetrics)
	mux.HandleFunc("/admin/audit", s.handleAudit)
	mux.HandleFunc("/admin/stats", s.handleStats)
	mux.HandleFunc("/admin/config", s.handleConfig)

	httpServer := &http.Server{
		Addr:         listenAddr,
		Handler:      logRequests(mux, logger),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	logger.Printf("")
	logger.Printf("✓ Skyline Web UI ready")
	logger.Printf("  → http://%s/ui/", listenAddr)
	logger.Printf("  → http://%s/admin/", listenAddr)
	logger.Printf("")
	
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
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

func (s *server) handleProfiles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.mu.RLock()
		names := make([]string, 0, len(s.store.Profiles))
		for _, p := range s.store.Profiles {
			if p.Name != "" {
				names = append(names, p.Name)
			}
		}
		s.mu.RUnlock()
		writeJSON(w, http.StatusOK, map[string]any{"profiles": names})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *server) handleProfileOrGateway(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if strings.HasSuffix(path, "/tools") {
		s.handleProfileTools(w, r)
		return
	}
	if strings.HasSuffix(path, "/execute") {
		s.handleProfileExecute(w, r)
		return
	}
	if strings.HasSuffix(path, "/gateway") {
		s.handleGatewayWebSocket(w, r)
		return
	}
	s.handleProfile(w, r)
}

func (s *server) handleProfile(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/profiles/")
	name = strings.TrimSpace(name)
	if name == "" {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.mu.RLock()
		prof, ok := s.findProfile(name)
		s.mu.RUnlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		if err := s.authorizeProfile(r, prof); err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		if strings.EqualFold(r.URL.Query().Get("format"), "json") {
			var cfg config.Config
			if err := yaml.Unmarshal([]byte(prof.ConfigYAML), &cfg); err != nil {
				http.Error(w, "invalid stored config", http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"name":   prof.Name,
				"token":  prof.Token,
				"config": cfg,
			})
			return
		}
		w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
		_, _ = w.Write([]byte(prof.ConfigYAML))
	case http.MethodPut:
		var req upsertRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
		req.Token = strings.TrimSpace(req.Token)
		req.ConfigYAML = strings.TrimSpace(req.ConfigYAML)
		if len(req.ConfigJSON) > 0 {
			var cfg config.Config
			if err := json.Unmarshal(req.ConfigJSON, &cfg); err != nil {
				http.Error(w, "invalid config_json", http.StatusBadRequest)
				return
			}
			data, err := yaml.Marshal(cfg)
			if err != nil {
				http.Error(w, "failed to marshal config_json", http.StatusInternalServerError)
				return
			}
			req.ConfigYAML = strings.TrimSpace(string(data))
		}
		if req.ConfigYAML == "" {
			http.Error(w, "config_yaml or config_json is required", http.StatusBadRequest)
			return
		}
		if err := config.ValidateYAML([]byte(req.ConfigYAML)); err != nil {
			http.Error(w, fmt.Sprintf("invalid config_yaml: %v", err), http.StatusBadRequest)
			return
		}

		s.mu.Lock()
		defer s.mu.Unlock()
		existing, ok := s.findProfile(name)
		if s.authMode == "bearer" {
			token := bearerToken(r.Header.Get("Authorization"))
			if ok {
				if token == "" || token != existing.Token {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
			} else {
				if token == "" || token != req.Token {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
			}
		}
		if req.Token == "" {
			if ok {
				req.Token = existing.Token
			} else {
				http.Error(w, "token is required", http.StatusBadRequest)
				return
			}
		}
		if ok {
			existing.Token = req.Token
			existing.ConfigYAML = req.ConfigYAML
			s.updateProfile(existing)
		} else {
			s.store.Profiles = append(s.store.Profiles, profile{
				Name:       name,
				Token:      req.Token,
				ConfigYAML: req.ConfigYAML,
			})
		}
		if err := s.save(); err != nil {
			http.Error(w, "failed to persist", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	case http.MethodDelete:
		s.mu.Lock()
		defer s.mu.Unlock()
		prof, ok := s.findProfile(name)
		if !ok {
			http.NotFound(w, r)
			return
		}
		if err := s.authorizeProfile(r, prof); err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		s.deleteProfile(name)
		if err := s.save(); err != nil {
			http.Error(w, "failed to persist", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *server) authorizeProfile(r *http.Request, prof profile) error {
	if s.authMode != "bearer" {
		return nil
	}
	token := bearerToken(r.Header.Get("Authorization"))
	if token == "" || token != prof.Token {
		return fmt.Errorf("unauthorized")
	}
	return nil
}

func (p profile) ToConfig() *config.Config {
	var cfg config.Config
	_ = yaml.Unmarshal([]byte(p.ConfigYAML), &cfg)
	cfg.ApplyDefaults() // Apply default timeout (10s) and retries if not set
	return &cfg
}

const graphqlIntrospectionPayload = `{"query":"query IntrospectionQuery { __schema { queryType { name } mutationType { name } types { kind name description fields(includeDeprecated: true) { name description args { name description defaultValue type { kind name ofType { kind name ofType { kind name ofType { kind name ofType { kind name } } } } } } type { kind name ofType { kind name ofType { kind name ofType { kind name ofType { kind name } } } } } } inputFields { name description defaultValue type { kind name ofType { kind name ofType { kind name ofType { kind name ofType { kind name } } } } } } enumValues(includeDeprecated: true) { name } } } }"}`

const rpcDiscoverPayload = `{"jsonrpc":"2.0","method":"rpc.discover","id":1,"params":[]}`

type detectRequest struct {
	BaseURL string `json:"base_url"`
}

type detectResponse struct {
	BaseURL  string        `json:"base_url"`
	Online   bool          `json:"online"`
	Detected []detectProbe `json:"detected"`
}

type detectProbe struct {
	Type     string `json:"type"`
	SpecURL  string `json:"spec_url"`
	Method   string `json:"method"`
	Status   int    `json:"status"`
	Found    bool   `json:"found"`
	Error    string `json:"error,omitempty"`
	Endpoint string `json:"endpoint"`
}

type testRequest struct {
	SpecURL string `json:"spec_url"`
}

type testResponse struct {
	SpecURL string `json:"spec_url"`
	Online  bool   `json:"online"`
	Status  int    `json:"status"`
	Error   string `json:"error,omitempty"`
}

type operationsRequest struct {
	SpecURL  string `json:"spec_url"`
	SpecType string `json:"spec_type,omitempty"`
}

type operationsResponse struct {
	Operations []operationInfo `json:"operations"`
	Error      string          `json:"error,omitempty"`
}

type operationInfo struct {
	ID      string `json:"id"`
	Method  string `json:"method"`
	Path    string `json:"path"`
	Summary string `json:"summary"`
}

type toolInfo struct {
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	InputSchema  map[string]any `json:"input_schema"`
	OutputSchema map[string]any `json:"output_schema"`
}

type executeRequest struct {
	ToolName  string         `json:"tool_name"`
	Arguments map[string]any `json:"arguments"`
}

func (s *server) handleDetect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req detectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	baseURL := strings.TrimSpace(req.BaseURL)
	if baseURL == "" {
		http.Error(w, "base_url is required", http.StatusBadRequest)
		return
	}

	resp := detectResponse{BaseURL: baseURL}

	probes := []struct {
		Type    string
		Path    string
		Method  string
		Body    []byte
		Headers map[string]string
	}{
		{Type: "jira-rest", Path: "/rest/api/3/serverInfo", Method: http.MethodGet},
		{Type: "openapi", Path: "/openapi.json", Method: http.MethodGet},
		{Type: "openapi", Path: "/openapi.yaml", Method: http.MethodGet},
		{Type: "openapi", Path: "/openapi/openapi.json", Method: http.MethodGet},
		{Type: "openapi", Path: "/openapi/openapi.yaml", Method: http.MethodGet},
		{Type: "openapi", Path: "/v3/api-docs", Method: http.MethodGet},
		{Type: "swagger2", Path: "/swagger.json", Method: http.MethodGet},
		{Type: "swagger2", Path: "/swagger.yaml", Method: http.MethodGet},
		{Type: "swagger2", Path: "/swagger/swagger.json", Method: http.MethodGet},
		{Type: "swagger2", Path: "/v2/api-docs", Method: http.MethodGet},
		{Type: "wsdl", Path: "/wsdl", Method: http.MethodGet},
		{Type: "wsdl", Path: "/wsdl?wsdl", Method: http.MethodGet},
		{Type: "wsdl", Path: "/wdsl/wsdl", Method: http.MethodGet},
		{Type: "odata", Path: "/$metadata", Method: http.MethodGet},
		{Type: "odata", Path: "/odata/$metadata", Method: http.MethodGet},
		{Type: "openrpc", Path: "/jsonrpc/openrpc.json", Method: http.MethodGet},
		{Type: "openrpc", Path: "/openrpc.json", Method: http.MethodGet},
		{Type: "openrpc", Path: "/jsonrpc", Method: http.MethodPost, Body: []byte(rpcDiscoverPayload), Headers: map[string]string{"Content-Type": "application/json"}},
		{Type: "openrpc", Path: "/rpc", Method: http.MethodPost, Body: []byte(rpcDiscoverPayload), Headers: map[string]string{"Content-Type": "application/json"}},
		{Type: "graphql", Path: "/graphql/schema", Method: http.MethodGet},
		{Type: "graphql", Path: "/graphql", Method: http.MethodPost, Body: []byte(graphqlIntrospectionPayload), Headers: map[string]string{"Content-Type": "application/json"}},
		{Type: "graphql", Path: "/api/graphql", Method: http.MethodPost, Body: []byte(graphqlIntrospectionPayload), Headers: map[string]string{"Content-Type": "application/json"}},
	}
	if basePathLooksLikeGraphQL(baseURL) {
		probes = append([]struct {
			Type    string
			Path    string
			Method  string
			Body    []byte
			Headers map[string]string
		}{
			{Type: "graphql", Path: "", Method: http.MethodPost, Body: []byte(graphqlIntrospectionPayload), Headers: map[string]string{"Content-Type": "application/json"}},
			{Type: "graphql", Path: "/schema", Method: http.MethodGet},
		}, probes...)
	}

	client := &http.Client{Timeout: 8 * time.Second}
	for _, probe := range probes {
		target := strings.TrimRight(baseURL, "/") + probe.Path
		found, status, err := s.probeURL(client, probe.Method, target, probe.Body, probe.Headers)
		item := detectProbe{
			Type:     probe.Type,
			SpecURL:  target,
			Method:   probe.Method,
			Status:   status,
			Found:    found,
			Endpoint: target,
		}
		if err != nil {
			item.Error = err.Error()
		}
		resp.Detected = append(resp.Detected, item)
		if found {
			resp.Online = true
		}
	}

	adapters := map[string]func([]byte) bool{
		"openapi":  spec.NewOpenAPIAdapter().Detect,
		"swagger2": spec.NewSwagger2Adapter().Detect,
		"graphql": func(raw []byte) bool {
			return graphql.LooksLikeGraphQLSDL(raw) || graphql.LooksLikeGraphQLIntrospection(raw)
		},
		"wsdl":  spec.NewWSDLAdapter().Detect,
		"odata":   looksLikeODataMetadata,
		"postman": postman.LooksLikePostmanCollection,
		"openrpc": openrpc.LooksLikeOpenRPC,
	}

	for i := range resp.Detected {
		if !resp.Detected[i].Found {
			continue
		}
		if resp.Detected[i].Type == "jira-rest" {
			continue
		}
		isOpenRPCDiscover := resp.Detected[i].Type == "openrpc" && resp.Detected[i].Method == http.MethodPost
		var postBody []byte
		if isOpenRPCDiscover {
			postBody = []byte(rpcDiscoverPayload)
		}
		raw, err := s.fetchRaw(client, resp.Detected[i].Method, resp.Detected[i].SpecURL, resp.Detected[i].Method == http.MethodPost && !isOpenRPCDiscover, postBody)
		if err != nil {
			resp.Detected[i].Found = false
			resp.Detected[i].Error = err.Error()
			continue
		}
		// For rpc.discover responses, unwrap the JSON-RPC result.
		if isOpenRPCDiscover {
			raw = unwrapJSONRPCResult(raw)
		}
		detectFn := adapters[resp.Detected[i].Type]
		if detectFn == nil || !detectFn(raw) {
			resp.Detected[i].Found = false
			resp.Detected[i].Error = "content did not match detected type"
		}
	}

	resp.Detected = applyJiraRestHint(resp.Detected, baseURL)

	writeJSON(w, http.StatusOK, resp)
}

func (s *server) handleTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req testRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	specURL := strings.TrimSpace(req.SpecURL)
	if specURL == "" {
		http.Error(w, "spec_url is required", http.StatusBadRequest)
		return
	}
	client := &http.Client{Timeout: 8 * time.Second}
	found, status, err := s.probeURL(client, http.MethodGet, specURL, nil, nil)
	resp := testResponse{
		SpecURL: specURL,
		Online:  found,
		Status:  status,
	}
	if err != nil {
		resp.Error = err.Error()
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *server) handleOperations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req operationsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}

	specURL := strings.TrimSpace(req.SpecURL)
	if specURL == "" {
		http.Error(w, "spec_url is required", http.StatusBadRequest)
		return
	}

	// Fetch and parse the spec
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	operations, err := s.fetchOperations(ctx, specURL, req.SpecType)
	if err != nil {
		writeJSON(w, http.StatusOK, operationsResponse{
			Error: err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, operationsResponse{
		Operations: operations,
	})
}

func (s *server) fetchOperations(ctx context.Context, specURL, specType string) ([]operationInfo, error) {
	fetcher := spec.NewFetcher(30 * time.Second)

	// Fetch spec
	raw, err := fetcher.Fetch(ctx, specURL, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch spec: %w", err)
	}

	// Try all adapters to detect and parse spec
	adapters := []spec.SpecAdapter{
		spec.NewOpenAPIAdapter(),
		spec.NewSwagger2Adapter(),
		spec.NewPostmanAdapter(),
		spec.NewGoogleDiscoveryAdapter(),
		spec.NewOpenRPCAdapter(),
		spec.NewGraphQLAdapter(),
		spec.NewJenkinsAdapter(),
		spec.NewWSDLAdapter(),
		spec.NewODataAdapter(),
	}

	var service *canonical.Service
	for _, adapter := range adapters {
		if !adapter.Detect(raw) {
			continue
		}
		parsed, err := adapter.Parse(ctx, raw, "temp", "")
		if err != nil {
			s.logger.Printf("adapter %T parse error: %v", adapter, err)
			continue
		}
		service = parsed
		break
	}

	if service == nil {
		return nil, fmt.Errorf("no supported spec format detected")
	}

	// Convert to operationInfo
	result := make([]operationInfo, len(service.Operations))
	for i, op := range service.Operations {
		result[i] = operationInfo{
			ID:      op.ID,
			Method:  op.Method,
			Path:    op.Path,
			Summary: op.Summary,
		}
	}

	return result, nil
}

func (s *server) handleProfileTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract profile name from URL path
	name := extractProfileName(r.URL.Path, "/profiles/", "/tools")
	if name == "" {
		http.Error(w, "profile name required", http.StatusBadRequest)
		return
	}

	// Authenticate request
	s.mu.RLock()
	prof, ok := s.findProfile(name)
	s.mu.RUnlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := s.authorizeProfile(r, prof); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// Load specs for profile
	cfg := prof.ToConfig()
	s.redactor.AddSecrets(cfg.Secrets())

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	services, err := spec.LoadServices(ctx, cfg, s.logger, s.redactor)
	if err != nil {
		http.Error(w, fmt.Sprintf("load services: %v", err), http.StatusInternalServerError)
		return
	}

	// Apply operation filters
	services = spec.ApplyOperationFilters(services, cfg.APIs)

	// Build registry to get tools with JSON schemas
	registry, err := mcp.NewRegistry(services)
	if err != nil {
		http.Error(w, fmt.Sprintf("build registry: %v", err), http.StatusInternalServerError)
		return
	}

	// Convert registry tools to response format
	tools := make([]toolInfo, 0, len(registry.Tools))
	for _, tool := range registry.Tools {
		tools = append(tools, toolInfo{
			Name:         tool.Name,
			Description:  tool.Description,
			InputSchema:  tool.InputSchema,
			OutputSchema: tool.OutputSchema,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"tools": tools})
}

func (s *server) handleProfileExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract profile name from URL path
	name := extractProfileName(r.URL.Path, "/profiles/", "/execute")
	if name == "" {
		http.Error(w, "profile name required", http.StatusBadRequest)
		return
	}

	// Authenticate request
	s.mu.RLock()
	prof, ok := s.findProfile(name)
	s.mu.RUnlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := s.authorizeProfile(r, prof); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// Parse request
	var req executeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	if req.ToolName == "" {
		http.Error(w, "tool_name is required", http.StatusBadRequest)
		return
	}

	// Load specs and build executor
	cfg := prof.ToConfig()
	s.redactor.AddSecrets(cfg.Secrets())

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	services, err := spec.LoadServices(ctx, cfg, s.logger, s.redactor)
	if err != nil {
		http.Error(w, fmt.Sprintf("load services: %v", err), http.StatusInternalServerError)
		return
	}

	// Apply filters
	services = spec.ApplyOperationFilters(services, cfg.APIs)

	// Build registry to look up the tool
	registry, err := mcp.NewRegistry(services)
	if err != nil {
		http.Error(w, fmt.Sprintf("build registry: %v", err), http.StatusInternalServerError)
		return
	}

	// Look up the tool by name
	tool, ok := registry.Tools[req.ToolName]
	if !ok {
		http.Error(w, fmt.Sprintf("unknown tool: %s", req.ToolName), http.StatusNotFound)
		return
	}

	// Create executor
	executor, err := runtime.NewExecutor(cfg, services, s.logger, s.redactor)
	if err != nil {
		http.Error(w, fmt.Sprintf("create executor: %v", err), http.StatusInternalServerError)
		return
	}

	// Execute the operation
	result, err := executor.Execute(ctx, tool.Operation, req.Arguments)
	if err != nil {
		http.Error(w, fmt.Sprintf("execute: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for now
	},
}

// handleGatewayWebSocket handles WebSocket connections for bidirectional gateway communication
func (s *server) handleGatewayWebSocket(w http.ResponseWriter, r *http.Request) {
	// Extract profile name from URL path
	name := extractProfileName(r.URL.Path, "/profiles/", "/gateway")
	if name == "" {
		http.Error(w, "profile name required", http.StatusBadRequest)
		return
	}

	// Authenticate request (check token before upgrading)
	s.mu.RLock()
	prof, ok := s.findProfile(name)
	s.mu.RUnlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := s.authorizeProfile(r, prof); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// Upgrade to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Printf("websocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	s.logger.Printf("websocket gateway connected: profile=%s", name)

	// Log connection
	clientAddr := conn.RemoteAddr().String()
	s.auditLogger.LogConnect(name, clientAddr)
	s.metrics.RecordConnection(true)

	// Create gateway session
	session := &gatewaySession{
		server:        s,
		conn:          conn,
		profile:       prof,
		logger:        s.logger,
		subscriptions: make(map[string]context.CancelFunc),
	}

	// Handle messages
	session.handleMessages()

	// Log disconnection
	s.auditLogger.LogDisconnect(name, clientAddr)
	s.metrics.RecordConnection(false)
}

// gatewaySession represents a WebSocket session for gateway communication
type gatewaySession struct {
	server        *server
	conn          *websocket.Conn
	profile       profile
	logger        *log.Logger
	subscriptions map[string]context.CancelFunc
	mu            sync.Mutex
}

// jsonrpcMessage represents a JSON-RPC 2.0 message
type jsonrpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// handleMessages processes incoming WebSocket messages
func (gs *gatewaySession) handleMessages() {
	gs.logger.Printf("[WS] Starting message handler loop for profile=%s", gs.profile.Name)
	for {
		gs.logger.Printf("[WS] Waiting for next message...")
		var msg jsonrpcMessage
		err := gs.conn.ReadJSON(&msg)
		if err != nil {
			gs.logger.Printf("[WS] ReadJSON error: %v (unexpected=%v)", err, websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure))
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				gs.logger.Printf("websocket error: %v", err)
			}
			break
		}

		gs.logger.Printf("[WS] Received message: method=%s, id=%v", msg.Method, msg.ID)
		// Route message based on method
		gs.routeMessage(&msg)
		gs.logger.Printf("[WS] Message handled, continuing loop...")
	}

	// Clean up subscriptions on disconnect
	gs.mu.Lock()
	for id, cancel := range gs.subscriptions {
		cancel()
		delete(gs.subscriptions, id)
	}
	gs.mu.Unlock()

	gs.logger.Printf("websocket gateway disconnected: profile=%s", gs.profile.Name)
}

// routeMessage routes JSON-RPC messages to appropriate handlers
func (gs *gatewaySession) routeMessage(msg *jsonrpcMessage) {
	switch msg.Method {
	case "execute":
		gs.handleExecute(msg)
	case "subscribe":
		gs.handleSubscribe(msg)
	case "unsubscribe":
		gs.handleUnsubscribe(msg)
	case "tools/list":
		gs.handleToolsList(msg)
	default:
		gs.sendError(msg.ID, -32601, fmt.Sprintf("method not found: %s", msg.Method))
	}
}

// handleExecute handles tool execution requests
func (gs *gatewaySession) handleExecute(msg *jsonrpcMessage) {
	startTime := time.Now()
	var params struct {
		ToolName  string         `json:"tool_name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		gs.sendError(msg.ID, -32602, "invalid params")
		gs.server.auditLogger.LogError(gs.profile.Name, "execute", "invalid params", gs.conn.RemoteAddr().String())
		return
	}

	// Load specs and build executor
	cfg := gs.profile.ToConfig()
	gs.server.redactor.AddSecrets(cfg.Secrets())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	services, err := spec.LoadServices(ctx, cfg, gs.logger, gs.server.redactor)
	if err != nil {
		errMsg := fmt.Sprintf("load services: %v", err)
		gs.sendError(msg.ID, -32603, errMsg)
		gs.server.auditLogger.LogExecute(ctx, gs.profile.Name, params.ToolName, params.Arguments,
			time.Since(startTime), 0, false, errMsg, gs.conn.RemoteAddr().String())
		gs.server.metrics.RecordRequest(gs.profile.Name, params.ToolName, time.Since(startTime), false)
		return
	}

	// Apply filters
	services = spec.ApplyOperationFilters(services, cfg.APIs)

	// Build registry to look up the tool
	registry, err := mcp.NewRegistry(services)
	if err != nil {
		errMsg := fmt.Sprintf("build registry: %v", err)
		gs.sendError(msg.ID, -32603, errMsg)
		gs.server.auditLogger.LogExecute(ctx, gs.profile.Name, params.ToolName, params.Arguments,
			time.Since(startTime), 0, false, errMsg, gs.conn.RemoteAddr().String())
		gs.server.metrics.RecordRequest(gs.profile.Name, params.ToolName, time.Since(startTime), false)
		return
	}

	// Look up the tool by name
	tool, ok := registry.Tools[params.ToolName]
	if !ok {
		errMsg := fmt.Sprintf("unknown tool: %s", params.ToolName)
		gs.sendError(msg.ID, -32602, errMsg)
		gs.server.auditLogger.LogExecute(ctx, gs.profile.Name, params.ToolName, params.Arguments,
			time.Since(startTime), 404, false, errMsg, gs.conn.RemoteAddr().String())
		gs.server.metrics.RecordRequest(gs.profile.Name, params.ToolName, time.Since(startTime), false)
		return
	}

	// Create executor
	executor, err := runtime.NewExecutor(cfg, services, gs.logger, gs.server.redactor)
	if err != nil {
		errMsg := fmt.Sprintf("create executor: %v", err)
		gs.sendError(msg.ID, -32603, errMsg)
		gs.server.auditLogger.LogExecute(ctx, gs.profile.Name, params.ToolName, params.Arguments,
			time.Since(startTime), 0, false, errMsg, gs.conn.RemoteAddr().String())
		gs.server.metrics.RecordRequest(gs.profile.Name, params.ToolName, time.Since(startTime), false)
		return
	}

	// Execute the operation
	result, err := executor.Execute(ctx, tool.Operation, params.Arguments)
	duration := time.Since(startTime)

	if err != nil {
		errMsg := fmt.Sprintf("execute: %v", err)
		gs.sendError(msg.ID, -32603, errMsg)
		gs.server.auditLogger.LogExecute(ctx, gs.profile.Name, params.ToolName, params.Arguments,
			duration, 0, false, errMsg, gs.conn.RemoteAddr().String())
		gs.server.metrics.RecordRequest(gs.profile.Name, params.ToolName, duration, false)
		return
	}

	// Log successful execution
	gs.server.auditLogger.LogExecute(ctx, gs.profile.Name, params.ToolName, params.Arguments,
		duration, result.Status, true, "", gs.conn.RemoteAddr().String())
	gs.server.metrics.RecordRequest(gs.profile.Name, params.ToolName, duration, true)

	// Send success response
	gs.sendResult(msg.ID, result)
}

// handleToolsList handles tools/list requests
func (gs *gatewaySession) handleToolsList(msg *jsonrpcMessage) {
	// Load specs for profile
	cfg := gs.profile.ToConfig()
	gs.server.redactor.AddSecrets(cfg.Secrets())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	services, err := spec.LoadServices(ctx, cfg, gs.logger, gs.server.redactor)
	if err != nil {
		gs.sendError(msg.ID, -32603, fmt.Sprintf("load services: %v", err))
		return
	}

	// Apply operation filters
	services = spec.ApplyOperationFilters(services, cfg.APIs)

	// Build registry to get tools with JSON schemas
	registry, err := mcp.NewRegistry(services)
	if err != nil {
		gs.sendError(msg.ID, -32603, fmt.Sprintf("build registry: %v", err))
		return
	}

	// Convert registry tools to response format
	tools := make([]toolInfo, 0, len(registry.Tools))
	for _, tool := range registry.Tools {
		tools = append(tools, toolInfo{
			Name:         tool.Name,
			Description:  tool.Description,
			InputSchema:  tool.InputSchema,
			OutputSchema: tool.OutputSchema,
		})
	}

	gs.sendResult(msg.ID, map[string]any{"tools": tools})
}

// handleSubscribe handles subscription requests (placeholder for future implementation)
func (gs *gatewaySession) handleSubscribe(msg *jsonrpcMessage) {
	var params struct {
		Resource string         `json:"resource"`
		Params   map[string]any `json:"params"`
	}
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		gs.sendError(msg.ID, -32602, "invalid params")
		return
	}

	// For now, just acknowledge the subscription
	// Future: implement actual streaming/subscription logic
	gs.sendResult(msg.ID, map[string]any{
		"subscription_id": fmt.Sprintf("sub_%v", msg.ID),
		"status":          "subscribed",
	})
}

// handleUnsubscribe handles unsubscribe requests
func (gs *gatewaySession) handleUnsubscribe(msg *jsonrpcMessage) {
	var params struct {
		SubscriptionID string `json:"subscription_id"`
	}
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		gs.sendError(msg.ID, -32602, "invalid params")
		return
	}

	gs.mu.Lock()
	if cancel, ok := gs.subscriptions[params.SubscriptionID]; ok {
		cancel()
		delete(gs.subscriptions, params.SubscriptionID)
	}
	gs.mu.Unlock()

	gs.sendResult(msg.ID, map[string]any{"status": "unsubscribed"})
}

// sendResult sends a JSON-RPC success response
func (gs *gatewaySession) sendResult(id any, result any) {
	response := jsonrpcMessage{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	if err := gs.conn.WriteJSON(response); err != nil {
		gs.logger.Printf("websocket write error: %v", err)
	}
}

// sendError sends a JSON-RPC error response
func (gs *gatewaySession) sendError(id any, code int, message string) {
	response := jsonrpcMessage{
		JSONRPC: "2.0",
		ID:      id,
		Error: &jsonrpcError{
			Code:    code,
			Message: message,
		},
	}
	if err := gs.conn.WriteJSON(response); err != nil {
		gs.logger.Printf("websocket write error: %v", err)
	}
}

// sendNotification sends a JSON-RPC notification (no ID, no response expected)
func (gs *gatewaySession) sendNotification(method string, params any) {
	notification := jsonrpcMessage{
		JSONRPC: "2.0",
		Method:  method,
		Params:  mustMarshal(params),
	}
	if err := gs.conn.WriteJSON(notification); err != nil {
		gs.logger.Printf("websocket write error: %v", err)
	}
}

func mustMarshal(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

// handleMetrics returns Prometheus-compatible metrics
func (s *server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(s.metrics.PrometheusFormat()))
}

// handleAudit returns audit log entries
func (s *server) handleAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse query parameters
	query := r.URL.Query()
	profile := query.Get("profile")
	eventType := query.Get("event_type")
	toolName := query.Get("tool_name")
	limit := 100
	if l := query.Get("limit"); l != "" {
		if parsed, err := fmt.Sscanf(l, "%d", &limit); err == nil && parsed == 1 {
			if limit > 1000 {
				limit = 1000
			}
		}
	}

	// Query audit log
	events, err := s.auditLogger.Query(audit.QueryOptions{
		Profile:   profile,
		EventType: eventType,
		ToolName:  toolName,
		Limit:     limit,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("query audit log: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"events": events,
		"count":  len(events),
	})
}

// handleStats returns aggregated statistics
func (s *server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse query parameters
	query := r.URL.Query()
	profile := query.Get("profile")

	// Default to last 24 hours
	since := time.Now().Add(-24 * time.Hour)
	if sinceStr := query.Get("since"); sinceStr != "" {
		if parsed, err := time.Parse(time.RFC3339, sinceStr); err == nil {
			since = parsed
		}
	}

	// Get audit stats
	auditStats, err := s.auditLogger.GetStats(profile, since)
	if err != nil {
		http.Error(w, fmt.Sprintf("get stats: %v", err), http.StatusInternalServerError)
		return
	}

	// Get metrics snapshot
	metricsSnapshot := s.metrics.Snapshot()

	writeJSON(w, http.StatusOK, map[string]any{
		"audit_stats":      auditStats,
		"metrics_snapshot": metricsSnapshot,
		"period": map[string]any{
			"since": since,
			"until": time.Now(),
		},
	})
}

// handleConfig manages server configuration (config.yaml)
func (s *server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetConfig(w, r)
	case http.MethodPost:
		s.handlePostConfig(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleGetConfig returns the current server configuration
func (s *server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	// Read config file
	data, err := os.ReadFile(s.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Config file doesn't exist - return empty default
			writeJSON(w, http.StatusOK, map[string]any{
				"raw": "# Skyline MCP Server Configuration\n# File not found - using defaults\n",
				"server": map[string]any{
					"listen": "localhost:8191",
				},
				"runtime": map[string]any{
					"codeExecution": map[string]any{
						"enabled": true,
					},
				},
				"audit": map[string]any{
					"enabled": true,
				},
				"logging": map[string]any{
					"level": "info",
				},
			})
			return
		}
		http.Error(w, fmt.Sprintf("read config: %v", err), http.StatusInternalServerError)
		return
	}

	// Parse YAML to validate and provide structured response
	var configData map[string]any
	if err := yaml.Unmarshal(data, &configData); err != nil {
		http.Error(w, fmt.Sprintf("parse config: %v", err), http.StatusBadRequest)
		return
	}

	// Return both raw YAML and parsed structure
	response := map[string]any{
		"raw": string(data),
	}

	// Add parsed fields if they exist
	if server, ok := configData["server"].(map[string]any); ok {
		response["server"] = server
	}
	if runtime, ok := configData["runtime"].(map[string]any); ok {
		response["runtime"] = runtime
	}
	if audit, ok := configData["audit"].(map[string]any); ok {
		response["audit"] = audit
	}
	if profiles, ok := configData["profiles"].(map[string]any); ok {
		response["profiles"] = profiles
	}
	if security, ok := configData["security"].(map[string]any); ok {
		response["security"] = security
	}
	if logging, ok := configData["logging"].(map[string]any); ok {
		response["logging"] = logging
	}

	writeJSON(w, http.StatusOK, response)
}

// handlePostConfig saves updated server configuration
func (s *server) handlePostConfig(w http.ResponseWriter, r *http.Request) {
	// Read request body (raw YAML)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("read body: %v", err), http.StatusBadRequest)
		return
	}

	// Validate YAML syntax
	var configData map[string]any
	if err := yaml.Unmarshal(data, &configData); err != nil {
		http.Error(w, fmt.Sprintf("invalid yaml: %v", err), http.StatusBadRequest)
		return
	}

	// Create config directory if it doesn't exist
	configDir := filepath.Dir(s.configPath)
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		http.Error(w, fmt.Sprintf("create config dir: %v", err), http.StatusInternalServerError)
		return
	}

	// Write config file atomically (write to temp, then rename)
	tmp := s.configPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		http.Error(w, fmt.Sprintf("write temp file: %v", err), http.StatusInternalServerError)
		return
	}

	if err := os.Rename(tmp, s.configPath); err != nil {
		os.Remove(tmp) // Clean up temp file on error
		http.Error(w, fmt.Sprintf("save config: %v", err), http.StatusInternalServerError)
		return
	}

	s.logger.Printf("config saved to %s", s.configPath)

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"message": "Configuration saved successfully. Restart the server for changes to take effect.",
		"path": s.configPath,
	})
}

func extractProfileName(path, prefix, suffix string) string {
	path = strings.TrimPrefix(path, prefix)
	path = strings.TrimSuffix(path, suffix)
	return strings.TrimSpace(path)
}

func looksLikeODataMetadata(raw []byte) bool {
	s := string(raw)
	return strings.Contains(s, "edmx:Edmx") || strings.Contains(s, "<edmx:DataServices") || strings.Contains(s, "oasis-open.org/odata")
}

func basePathLooksLikeGraphQL(baseURL string) bool {
	lower := strings.ToLower(baseURL)
	return strings.Contains(lower, "/graphql")
}

func applyJiraRestHint(detected []detectProbe, baseURL string) []detectProbe {
	if !strings.HasSuffix(strings.ToLower(baseURL), ".atlassian.net") {
		return detected
	}
	for i := range detected {
		if detected[i].Type == "jira-rest" && detected[i].Found {
			detected[i].SpecURL = "https://developer.atlassian.com/cloud/jira/platform/swagger-v3.v3.json"
			detected[i].Endpoint = detected[i].SpecURL
			return detected
		}
	}
	return detected
}

func (s *server) probeURL(client *http.Client, method, url string, body []byte, headers map[string]string) (bool, int, error) {
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return false, 0, err
	}
	req.Header.Set("Accept", "application/json, text/yaml, application/yaml, application/xml, text/xml, */*")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, resp.StatusCode, nil
	}
	return true, resp.StatusCode, nil
}

func (s *server) fetchRaw(client *http.Client, method, url string, useIntrospection bool, explicitBody []byte) ([]byte, error) {
	var body []byte
	if len(explicitBody) > 0 {
		body = explicitBody
	} else if useIntrospection {
		body = []byte(graphqlIntrospectionPayload)
	}
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json, text/yaml, application/yaml, application/xml, text/xml, */*")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func unwrapJSONRPCResult(raw []byte) []byte {
	var rpcResp struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &rpcResp); err == nil && len(rpcResp.Result) > 0 {
		return []byte(rpcResp.Result)
	}
	return raw
}

func bearerToken(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 {
		return ""
	}
	if !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func (s *server) findProfile(name string) (profile, bool) {
	for _, p := range s.store.Profiles {
		if p.Name == name {
			return p, true
		}
	}
	return profile{}, false
}

func (s *server) updateProfile(updated profile) {
	for i := range s.store.Profiles {
		if s.store.Profiles[i].Name == updated.Name {
			s.store.Profiles[i] = updated
			return
		}
	}
}

func (s *server) deleteProfile(name string) {
	out := s.store.Profiles[:0]
	for _, p := range s.store.Profiles {
		if p.Name != name {
			out = append(out, p)
		}
	}
	s.store.Profiles = out
}

func (s *server) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.store = profileStore{}
			return nil
		}
		return err
	}
	var env envelope
	if err := yaml.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("parse storage: %w", err)
	}
	plain, err := decrypt(env, s.key)
	if err != nil {
		return fmt.Errorf("decryption failed (wrong key or corrupted data): %w", err)
	}
	var store profileStore
	if err := yaml.Unmarshal(plain, &store); err != nil {
		return fmt.Errorf("parse store: %w", err)
	}
	s.store = store
	return nil
}

func (s *server) save() error {
	plain, err := yaml.Marshal(s.store)
	if err != nil {
		return err
	}
	env, err := encrypt(plain, s.key)
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(env)
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func encrypt(plain, key []byte) (*envelope, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ciphertext := gcm.Seal(nil, nonce, plain, nil)
	return &envelope{
		Version:    1,
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	}, nil
}

func decrypt(env envelope, key []byte) ([]byte, error) {
	nonce, err := base64.StdEncoding.DecodeString(env.Nonce)
	if err != nil {
		return nil, fmt.Errorf("decode nonce: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(env.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decode ciphertext: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return plain, nil
}

func decodeKey(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty key")
	}
	if strings.HasPrefix(raw, "base64:") {
		raw = strings.TrimPrefix(raw, "base64:")
	}
	if strings.HasPrefix(raw, "hex:") {
		raw = strings.TrimPrefix(raw, "hex:")
	}
	if decoded, err := base64.StdEncoding.DecodeString(raw); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if decoded, err := hex.DecodeString(raw); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if len(raw) == 32 {
		return []byte(raw), nil
	}
	return nil, fmt.Errorf("key must be 32 bytes (raw), base64, or hex")
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
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

func setLogLevel(logger *log.Logger, level string) {
	// Note: Go's standard logger doesn't have levels built-in
	// This is a placeholder for future structured logging
	// For now, we just log the configured level
	switch strings.ToLower(level) {
	case "debug", "info", "warn", "error":
		// Valid levels - no action needed for now
	default:
		logger.Printf("Warning: unknown log level %q, using 'info'", level)
	}
}
