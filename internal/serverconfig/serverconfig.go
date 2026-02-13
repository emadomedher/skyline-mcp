package serverconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// ServerConfig represents the server configuration (config.yaml)
type ServerConfig struct {
	Server   ServerSection   `yaml:"server"`
	Runtime  RuntimeSection  `yaml:"runtime"`
	Audit    AuditSection    `yaml:"audit"`
	Profiles ProfilesSection `yaml:"profiles"`
	Security SecuritySection `yaml:"security"`
	Logging  LoggingSection  `yaml:"logging"`
}

type ServerSection struct {
	Listen         string        `yaml:"listen"`
	Timeout        time.Duration `yaml:"timeout,omitempty"`
	MaxRequestSize string        `yaml:"maxRequestSize,omitempty"`
	TLS            *TLSConfig    `yaml:"tls,omitempty"`
}

type TLSConfig struct {
	Enabled bool   `yaml:"enabled"`
	Cert    string `yaml:"cert"`
	Key     string `yaml:"key"`
}

type RuntimeSection struct {
	CodeExecution CodeExecutionConfig `yaml:"codeExecution"`
	Cache         CacheConfig         `yaml:"cache"`
}

type CodeExecutionConfig struct {
	Enabled     bool          `yaml:"enabled"`
	Engine      string        `yaml:"engine"`
	DenoPath    string        `yaml:"denoPath,omitempty"`
	Timeout     time.Duration `yaml:"timeout,omitempty"`
	MemoryLimit string        `yaml:"memoryLimit,omitempty"`
}

type CacheConfig struct {
	Enabled bool          `yaml:"enabled"`
	TTL     time.Duration `yaml:"ttl,omitempty"`
	MaxSize string        `yaml:"maxSize,omitempty"`
}

type AuditSection struct {
	Enabled      bool          `yaml:"enabled"`
	Database     string        `yaml:"database"`
	RotateAfter  time.Duration `yaml:"rotateAfter,omitempty"`
	MaxSize      string        `yaml:"maxSize,omitempty"`
}

type ProfilesSection struct {
	Storage       string `yaml:"storage"`
	EncryptionKey string `yaml:"encryptionKey"`
}

type SecuritySection struct {
	AllowedDomains []string    `yaml:"allowedDomains"`
	CORS           *CORSConfig `yaml:"cors,omitempty"`
}

type CORSConfig struct {
	Enabled bool     `yaml:"enabled"`
	Origins []string `yaml:"origins"`
}

type LoggingSection struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
	Output string `yaml:"output,omitempty"`
}

// Default returns a ServerConfig with sensible defaults
func Default() *ServerConfig {
	return &ServerConfig{
		Server: ServerSection{
			Listen:  "localhost:8191",
			Timeout: 30 * time.Second,
		},
		Runtime: RuntimeSection{
			CodeExecution: CodeExecutionConfig{
				Enabled:     true,
				Engine:      "deno",
				Timeout:     30 * time.Second,
				MemoryLimit: "512MB",
			},
			Cache: CacheConfig{
				Enabled: true,
				TTL:     1 * time.Hour,
				MaxSize: "100MB",
			},
		},
		Audit: AuditSection{
			Enabled:  true,
			Database: "~/.skyline/skyline-audit.db",
		},
		Profiles: ProfilesSection{
			Storage:       "~/.skyline/profiles.enc.yaml",
			EncryptionKey: "${SKYLINE_PROFILES_KEY}",
		},
		Security: SecuritySection{
			AllowedDomains: []string{"*"},
		},
		Logging: LoggingSection{
			Level:  "info",
			Format: "json",
		},
	}
}

// Load reads config from path, returns default if not found
func Load(path string) (*ServerConfig, error) {
	// Expand home directory
	if len(path) > 0 && path[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("get home dir: %w", err)
		}
		path = filepath.Join(home, path[1:])
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Config doesn't exist - return default
			return Default(), nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg ServerConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Apply defaults for missing fields
	cfg.ApplyDefaults()

	return &cfg, nil
}

// ApplyDefaults fills in missing fields with default values
func (c *ServerConfig) ApplyDefaults() {
	if c.Server.Listen == "" {
		c.Server.Listen = "localhost:8191"
	}
	if c.Server.Timeout == 0 {
		c.Server.Timeout = 30 * time.Second
	}

	// Runtime defaults
	if c.Runtime.CodeExecution.Engine == "" {
		c.Runtime.CodeExecution.Engine = "deno"
	}
	if c.Runtime.CodeExecution.Timeout == 0 {
		c.Runtime.CodeExecution.Timeout = 30 * time.Second
	}
	if c.Runtime.CodeExecution.MemoryLimit == "" {
		c.Runtime.CodeExecution.MemoryLimit = "512MB"
	}
	if c.Runtime.Cache.TTL == 0 {
		c.Runtime.Cache.TTL = 1 * time.Hour
	}
	if c.Runtime.Cache.MaxSize == "" {
		c.Runtime.Cache.MaxSize = "100MB"
	}

	// Audit defaults
	if c.Audit.Database == "" {
		c.Audit.Database = "~/.skyline/skyline-audit.db"
	}

	// Profiles defaults
	if c.Profiles.Storage == "" {
		c.Profiles.Storage = "~/.skyline/profiles.enc.yaml"
	}
	if c.Profiles.EncryptionKey == "" {
		c.Profiles.EncryptionKey = "${SKYLINE_PROFILES_KEY}"
	}

	// Security defaults
	if len(c.Security.AllowedDomains) == 0 {
		c.Security.AllowedDomains = []string{"*"}
	}

	// Logging defaults
	if c.Logging.Level == "" {
		c.Logging.Level = "info"
	}
	if c.Logging.Format == "" {
		c.Logging.Format = "json"
	}
}

// Save writes config to path
func (c *ServerConfig) Save(path string) error {
	// Expand home directory
	if len(path) > 0 && path[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("get home dir: %w", err)
		}
		path = filepath.Join(home, path[1:])
	}

	// Create directory if needed
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	// Marshal to YAML
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	// Write atomically
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename temp file: %w", err)
	}

	return nil
}

// GenerateDefault creates a default config.yaml with comments
func GenerateDefault(path string) error {
	// Expand home directory
	if len(path) > 0 && path[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("get home dir: %w", err)
		}
		path = filepath.Join(home, path[1:])
	}

	// Create directory if needed
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	// Default config with comments (same as install.sh)
	defaultConfig := `# Skyline MCP Server Configuration
# Manage these settings via Web UI at http://localhost:19190/ui/settings
# or edit this file directly

server:
  # HTTP transport settings
  listen: "localhost:8191"
  # timeout: 30s
  # maxRequestSize: 10MB
  
  # TLS (optional, for production)
  # tls:
  #   enabled: false
  #   cert: /path/to/cert.pem
  #   key: /path/to/key.pem

runtime:
  # Code execution engine (98% cost reduction vs traditional MCP)
  codeExecution:
    enabled: true
    engine: "deno"  # or "node", "bun"
    # denoPath: "/home/user/.deno/bin/deno"  # auto-detect if not set
    timeout: 30s
    memoryLimit: "512MB"
    
  # Discovery cache (for repeated API calls)
  cache:
    enabled: true
    ttl: 1h
    maxSize: 100MB

audit:
  enabled: true
  database: "~/.skyline/skyline-audit.db"
  # rotateAfter: 30d
  # maxSize: 1GB

profiles:
  # API credentials & rate-limiting configurations
  # Managed via Web UI - stores auth tokens, rate limits, custom headers
  storage: "~/.skyline/profiles.enc.yaml"
  encryptionKey: "${SKYLINE_PROFILES_KEY}"  # from skyline.env

# Security
security:
  # Allowed domains for discovery mode (wildcard supported)
  allowedDomains:
    - "*"  # Allow all by default (can restrict in production)
  # cors:
  #   enabled: true
  #   origins: ["http://localhost:*"]
  
# Logging
logging:
  level: "info"  # debug, info, warn, error
  format: "json"  # json or text
  # output: "~/.skyline/skyline.log"
`

	// Write to file
	if err := os.WriteFile(path, []byte(defaultConfig), 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}

// ExpandPath expands ~ to home directory
func ExpandPath(path string) (string, error) {
	if len(path) > 0 && path[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, path[1:]), nil
	}
	return path, nil
}
