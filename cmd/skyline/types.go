package main

import (
	"context"
	"encoding/json"
	"log"
	"sync"

	"github.com/gorilla/websocket"

	"skyline-mcp/internal/audit"
	"skyline-mcp/internal/metrics"
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
	mu          sync.RWMutex
	store       profileStore
	path        string
	configPath  string
	serverCfg   *serverconfig.ServerConfig
	key         []byte
	authMode    string
	adminToken  string
	logger      *log.Logger
	redactor    *redact.Redactor
	auditLogger *audit.Logger
	metrics     *metrics.Collector
	cache       *profileCache
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
