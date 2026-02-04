package config

import (
	"fmt"
)

type Config struct {
	APIs           []APIConfig `yaml:"apis"`
	TimeoutSeconds int         `yaml:"timeout_seconds,omitempty"`
	Retries        int         `yaml:"retries,omitempty"`
}

type APIConfig struct {
	Name            string         `yaml:"name"`
	SpecURL         string         `yaml:"spec_url"`
	SpecFile        string         `yaml:"spec_file,omitempty"`
	BaseURLOverride string         `yaml:"base_url_override,omitempty"`
	Auth            *AuthConfig    `yaml:"auth,omitempty"`
	TimeoutSeconds  *int           `yaml:"timeout_seconds,omitempty"`
	Retries         *int           `yaml:"retries,omitempty"`
	Jenkins         *JenkinsConfig `yaml:"jenkins,omitempty"`
}

type AuthConfig struct {
	Type     string `yaml:"type"`
	Token    string `yaml:"token,omitempty"`    // bearer
	Username string `yaml:"username,omitempty"` // basic
	Password string `yaml:"password,omitempty"` // basic
	Header   string `yaml:"header,omitempty"`   // api-key header name
	Value    string `yaml:"value,omitempty"`    // api-key value
}

func (c *Config) ApplyDefaults() {
	if c.TimeoutSeconds == 0 {
		c.TimeoutSeconds = 10
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
	}
}

func (c *Config) Validate() error {
	if len(c.APIs) == 0 {
		return fmt.Errorf("no apis configured")
	}
	seen := map[string]struct{}{}
	for i, api := range c.APIs {
		if api.Name == "" {
			return fmt.Errorf("apis[%d]: name is required", i)
		}
		if api.SpecURL == "" && api.SpecFile == "" {
			return fmt.Errorf("apis[%d]: either spec_url or spec_file is required", i)
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
	AllowWrites []JenkinsWrite `yaml:"allow_writes,omitempty"`
}

type JenkinsWrite struct {
	Name    string `yaml:"name"`
	Method  string `yaml:"method"`
	Path    string `yaml:"path"`
	Summary string `yaml:"summary,omitempty"`
}
