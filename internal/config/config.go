package config

import (
	"fmt"
	"strings"
)

type Config struct {
	APIs               []APIConfig `json:"apis" yaml:"apis"`
	TimeoutSeconds     int         `json:"timeout_seconds,omitempty" yaml:"timeout_seconds,omitempty"`
	Retries            int         `json:"retries,omitempty" yaml:"retries,omitempty"`
	EnableCodeExecution *bool      `json:"enable_code_execution,omitempty" yaml:"enable_code_execution,omitempty"`
}

type APIConfig struct {
	Name            string                `json:"name" yaml:"name"`
	SpecURL         string                `json:"spec_url" yaml:"spec_url"`
	SpecFile        string                `json:"spec_file,omitempty" yaml:"spec_file,omitempty"`
	SpecType        string                `json:"spec_type,omitempty" yaml:"spec_type,omitempty"`
	BaseURLOverride string                `json:"base_url_override,omitempty" yaml:"base_url_override,omitempty"`
	Auth            *AuthConfig           `json:"auth,omitempty" yaml:"auth,omitempty"`
	TimeoutSeconds  *int                  `json:"timeout_seconds,omitempty" yaml:"timeout_seconds,omitempty"`
	Retries         *int                  `json:"retries,omitempty" yaml:"retries,omitempty"`
	Jenkins         *JenkinsConfig        `json:"jenkins,omitempty" yaml:"jenkins,omitempty"`
	Filter          *OperationFilter      `json:"filter,omitempty" yaml:"filter,omitempty"`
	Optimization    *GraphQLOptimization  `json:"optimization,omitempty" yaml:"optimization,omitempty"`
}

type AuthConfig struct {
	Type     string `json:"type" yaml:"type"`
	Token    string `json:"token,omitempty" yaml:"token,omitempty"`       // bearer
	Username string `json:"username,omitempty" yaml:"username,omitempty"` // basic
	Password string `json:"password,omitempty" yaml:"password,omitempty"` // basic
	Header   string `json:"header,omitempty" yaml:"header,omitempty"`     // api-key header name
	Value    string `json:"value,omitempty" yaml:"value,omitempty"`       // api-key value
}

func (c *Config) ApplyDefaults() {
	if c.TimeoutSeconds == 0 {
		c.TimeoutSeconds = 10
	}
	// Default: enable code execution (98% cost reduction)
	if c.EnableCodeExecution == nil {
		defaultTrue := true
		c.EnableCodeExecution = &defaultTrue
	}
	for i := range c.APIs {
		if c.APIs[i].TimeoutSeconds == nil {
			val := c.TimeoutSeconds
			c.APIs[i].TimeoutSeconds = &val
		}
		if c.APIs[i].Retries == nil {
			val := c.Retries
			c.APIs[i].Retries = &val
		}
		// Apply GraphQL optimization defaults
		if c.APIs[i].Optimization == nil {
			// Default: enable CRUD grouping (92% tool reduction: 260 → 23 tools)
			c.APIs[i].Optimization = &GraphQLOptimization{
				EnableCRUDGrouping: true,
			}
		}
	}
}

// CodeExecutionEnabled returns whether code execution is enabled (default: true)
func (c *Config) CodeExecutionEnabled() bool {
	if c.EnableCodeExecution == nil {
		return true // Default enabled
	}
	return *c.EnableCodeExecution
}

func (c *Config) Validate() error {
	// Allow empty API list - profile will respond with no tools available
	if len(c.APIs) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	for i, api := range c.APIs {
		if api.Name == "" {
			return fmt.Errorf("apis[%d]: name is required", i)
		}
		if api.SpecURL == "" && api.SpecFile == "" && api.SpecType != "grpc" {
			return fmt.Errorf("apis[%d]: either spec_url or spec_file is required", i)
		}
		if api.SpecType == "grpc" && api.BaseURLOverride == "" {
			return fmt.Errorf("apis[%d]: base_url_override is required for grpc", i)
		}
		if api.SpecURL != "" && api.SpecFile != "" {
			return fmt.Errorf("apis[%d]: spec_url and spec_file are mutually exclusive", i)
		}
		if _, ok := seen[api.Name]; ok {
			return fmt.Errorf("apis[%d]: duplicate name %q", i, api.Name)
		}
		seen[api.Name] = struct{}{}
		if api.Auth != nil {
			if err := api.Auth.Validate(); err != nil {
				return fmt.Errorf("apis[%d]: %w", i, err)
			}
		}
		if api.TimeoutSeconds != nil && *api.TimeoutSeconds < 0 {
			return fmt.Errorf("apis[%d]: timeout_seconds must be >= 0", i)
		}
		if api.Retries != nil && *api.Retries < 0 {
			return fmt.Errorf("apis[%d]: retries must be >= 0", i)
		}
		if api.Jenkins != nil {
			for j, write := range api.Jenkins.AllowWrites {
				if write.Name == "" {
					return fmt.Errorf("apis[%d].jenkins.allow_writes[%d]: name is required", i, j)
				}
				if write.Method == "" {
					return fmt.Errorf("apis[%d].jenkins.allow_writes[%d]: method is required", i, j)
				}
				if write.Path == "" {
					return fmt.Errorf("apis[%d].jenkins.allow_writes[%d]: path is required", i, j)
				}
			}
		}
		if api.Filter != nil {
			if err := api.Filter.Validate(i); err != nil {
				return fmt.Errorf("apis[%d]: %w", i, err)
			}
		}
	}
	return nil
}

func (a *AuthConfig) Validate() error {
	switch a.Type {
	case "":
		return fmt.Errorf("auth.type is required")
	case "bearer":
		if a.Token == "" {
			return fmt.Errorf("auth.token is required for bearer")
		}
	case "basic":
		if a.Username == "" || a.Password == "" {
			return fmt.Errorf("auth.username and auth.password are required for basic")
		}
	case "api-key":
		if a.Header == "" || a.Value == "" {
			return fmt.Errorf("auth.header and auth.value are required for api-key")
		}
	default:
		return fmt.Errorf("unsupported auth.type %q", a.Type)
	}
	return nil
}

func (f *OperationFilter) Validate(apiIndex int) error {
	if f.Mode == "" {
		return fmt.Errorf("filter.mode is required")
	}
	mode := strings.ToLower(f.Mode)
	if mode != "allowlist" && mode != "blocklist" {
		return fmt.Errorf("filter.mode must be 'allowlist' or 'blocklist', got %q", f.Mode)
	}
	if len(f.Operations) == 0 {
		return fmt.Errorf("filter.operations cannot be empty")
	}

	for j, op := range f.Operations {
		if op.OperationID == "" && op.Method == "" && op.Path == "" {
			return fmt.Errorf("filter.operations[%d]: at least one of operation_id, method, or path is required", j)
		}

		// Validate glob patterns (basic check)
		if op.OperationID != "" {
			if err := validateGlobPattern(op.OperationID); err != nil {
				return fmt.Errorf("filter.operations[%d].operation_id: %w", j, err)
			}
		}
		if op.Path != "" {
			if err := validateGlobPattern(op.Path); err != nil {
				return fmt.Errorf("filter.operations[%d].path: %w", j, err)
			}
		}
		if op.Method != "" {
			if err := validateMethodPattern(op.Method); err != nil {
				return fmt.Errorf("filter.operations[%d].method: %w", j, err)
			}
		}
	}

	return nil
}

func validateGlobPattern(pattern string) error {
	// Basic validation: check for invalid glob syntax
	// Allow *, ?, but reject patterns with syntax errors
	if strings.Contains(pattern, "***") {
		return fmt.Errorf("invalid glob pattern: too many consecutive asterisks")
	}
	return nil
}

func validateMethodPattern(method string) error {
	method = strings.ToUpper(method)
	if method == "*" {
		return nil
	}
	validMethods := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS", "CONNECT", "TRACE"}
	for _, valid := range validMethods {
		if method == valid {
			return nil
		}
	}
	return fmt.Errorf("invalid HTTP method %q", method)
}

func (c *Config) Secrets() []string {
	var secrets []string
	for _, api := range c.APIs {
		if api.Auth == nil {
			continue
		}
		switch api.Auth.Type {
		case "bearer":
			if api.Auth.Token != "" {
				secrets = append(secrets, api.Auth.Token)
			}
		case "basic":
			if api.Auth.Password != "" {
				secrets = append(secrets, api.Auth.Password)
			}
		case "api-key":
			if api.Auth.Value != "" {
				secrets = append(secrets, api.Auth.Value)
			}
		}
	}
	return secrets
}

type JenkinsConfig struct {
	AllowWrites []JenkinsWrite `json:"allow_writes,omitempty" yaml:"allow_writes,omitempty"`
}

type JenkinsWrite struct {
	Name    string `json:"name" yaml:"name"`
	Method  string `json:"method" yaml:"method"`
	Path    string `json:"path" yaml:"path"`
	Summary string `json:"summary,omitempty" yaml:"summary,omitempty"`
}

type OperationFilter struct {
	Mode       string             `json:"mode" yaml:"mode"`             // "allowlist" or "blocklist"
	Operations []OperationPattern `json:"operations" yaml:"operations"` // List of patterns
}

type OperationPattern struct {
	OperationID string `json:"operation_id,omitempty" yaml:"operation_id,omitempty"` // Pattern for operationId (e.g., "get*", "createUser")
	Method      string `json:"method,omitempty" yaml:"method,omitempty"`             // HTTP method pattern (e.g., "GET", "POST", "*")
	Path        string `json:"path,omitempty" yaml:"path,omitempty"`                 // Path pattern (e.g., "/users/*", "/admin/**")
	Summary     string `json:"summary,omitempty" yaml:"summary,omitempty"`           // Optional description for documentation
}
