package main

import (
	"encoding/json"
	"log/slog"
	"sync"

	"skyline-mcp/internal/audit"
	"skyline-mcp/internal/email"
	"skyline-mcp/internal/mcp"
	"skyline-mcp/internal/metrics"
	"skyline-mcp/internal/oauth"
	"skyline-mcp/internal/polling"
	"skyline-mcp/internal/ratelimit"
	"skyline-mcp/internal/redact"
	"skyline-mcp/internal/serverconfig"
)

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
	mu              sync.RWMutex
	store           profileStore
	path            string
	configPath      string
	serverCfg       *serverconfig.ServerConfig
	key             []byte
	authMode        string
	adminToken      string
	logger          *slog.Logger
	redactor        *redact.Redactor
	auditLogger     *audit.Logger
	metrics         *metrics.Collector
	cache           *profileCache
	mcpServers      sync.Map // map[profileName+configHash] → *mcp.StreamableHTTPServer
	sessionTracker  *mcp.SessionTracker
	agentHub        *audit.GenericHub
	oauthStore      *oauth.Store
	detectLimiter   *ratelimit.Limiter
	verifyLimiter   *ratelimit.Limiter
	pollEngine      *polling.Engine
	emailPersistent *email.PersistentManager
}

type upsertRequest struct {
	Token      string          `json:"token"`
	ConfigYAML string          `json:"config_yaml"`
	ConfigJSON json.RawMessage `json:"config_json"`
}

type detectRequest struct {
	BaseURL     string `json:"base_url"`
	BearerToken string `json:"bearer_token,omitempty"`
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
	Name     string `json:"name,omitempty"`
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

type emailLookupRequest struct {
	Email string `json:"email"`
}

type emailLookupResponse struct {
	Provider string `json:"provider"` // e.g., "Gmail", "Outlook", "unknown"
	SMTPHost string `json:"smtp_host"`
	SMTPPort int    `json:"smtp_port"`
	SMTPTLS  string `json:"smtp_tls"`
	IMAPHost string `json:"imap_host"`
	IMAPPort int    `json:"imap_port"`
	POP3Host string `json:"pop3_host,omitempty"`
	POP3Port int    `json:"pop3_port,omitempty"`
	Error    string `json:"error,omitempty"`
}

type emailVerifyRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	SMTPHost string `json:"smtp_host,omitempty"`
	SMTPPort int    `json:"smtp_port,omitempty"`
	SMTPTLS  string `json:"smtp_tls,omitempty"`
	IMAPHost string `json:"imap_host,omitempty"`
	IMAPPort int    `json:"imap_port,omitempty"`
}

type emailVerifyResponse struct {
	OK      bool   `json:"ok"`
	IMAP    string `json:"imap,omitempty"` // "ok", "failed", "skipped"
	SMTP    string `json:"smtp,omitempty"` // "ok", "failed", "skipped"
	IMAPErr string `json:"imap_error,omitempty"`
	SMTPErr string `json:"smtp_error,omitempty"`
	Error   string `json:"error,omitempty"`
}

type executeRequest struct {
	ToolName  string         `json:"tool_name"`
	Arguments map[string]any `json:"arguments"`
}
