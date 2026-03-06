// Package email provides a built-in SMTP/IMAP/POP3 email client for Skyline MCP.
// Unlike other API types that proxy HTTP calls through OpenAPI specs, email is a
// native protocol handler that registers MCP tools directly.
package email

import "skyline-mcp/internal/config"

// EmailConfig holds the configuration for an email account.
// Stored inside APIConfig via the Email field.
type EmailConfig struct {
	// Account credentials
	Address  string `json:"address" yaml:"address"`   // user@example.com
	Password string `json:"password" yaml:"password"` // app password or regular password

	// SMTP (sending)
	SMTPHost string `json:"smtp_host,omitempty" yaml:"smtp_host,omitempty"` // smtp.gmail.com
	SMTPPort int    `json:"smtp_port,omitempty" yaml:"smtp_port,omitempty"` // 587 (STARTTLS) or 465 (SSL)
	SMTPTLS  string `json:"smtp_tls,omitempty" yaml:"smtp_tls,omitempty"`   // "starttls" (default), "ssl", "none"

	// Reading (IMAP preferred, POP3 fallback)
	IMAPHost string `json:"imap_host,omitempty" yaml:"imap_host,omitempty"` // imap.gmail.com
	IMAPPort int    `json:"imap_port,omitempty" yaml:"imap_port,omitempty"` // 993
	POP3Host string `json:"pop3_host,omitempty" yaml:"pop3_host,omitempty"` // pop.gmail.com
	POP3Port int    `json:"pop3_port,omitempty" yaml:"pop3_port,omitempty"` // 995

	// Connection mode: "basic" (connect per call) or "persistent" (pool + IDLE push)
	ConnectionMode string `json:"connection_mode,omitempty" yaml:"connection_mode,omitempty"` // "basic" (default), "persistent"

	// Polling (only used in basic mode; persistent mode uses IDLE instead)
	PollIntervalSeconds int `json:"poll_interval_seconds,omitempty" yaml:"poll_interval_seconds,omitempty"` // 0 = disabled
}

// IsPersistent returns true if the connection mode is persistent (pool + IDLE).
func (c *EmailConfig) IsPersistent() bool {
	return c.ConnectionMode == "persistent"
}

// ReadProtocol returns "imap", "pop3", or "" based on what's configured.
func (c *EmailConfig) ReadProtocol() string {
	if c.IMAPHost != "" {
		return "imap"
	}
	if c.POP3Host != "" {
		return "pop3"
	}
	return ""
}

// HasSMTP returns true if SMTP sending is configured.
func (c *EmailConfig) HasSMTP() bool {
	return c.SMTPHost != ""
}

// ApplyDefaults fills in default ports if not set.
func (c *EmailConfig) ApplyDefaults() {
	if c.SMTPPort == 0 {
		c.SMTPPort = 587
	}
	if c.SMTPTLS == "" {
		c.SMTPTLS = "starttls"
	}
	if c.IMAPHost != "" && c.IMAPPort == 0 {
		c.IMAPPort = 993
	}
	if c.POP3Host != "" && c.POP3Port == 0 {
		c.POP3Port = 995
	}
}

// ConfigFromAPIConfig converts a config.EmailConfig to an email.EmailConfig
// and applies defaults. This bridges the config package (no import cycles)
// with the email package.
func ConfigFromAPIConfig(c *config.EmailConfig) *EmailConfig {
	cfg := &EmailConfig{
		Address:             c.Address,
		Password:            c.Password,
		SMTPHost:            c.SMTPHost,
		SMTPPort:            c.SMTPPort,
		SMTPTLS:             c.SMTPTLS,
		IMAPHost:            c.IMAPHost,
		IMAPPort:            c.IMAPPort,
		POP3Host:            c.POP3Host,
		POP3Port:            c.POP3Port,
		ConnectionMode:      c.ConnectionMode,
		PollIntervalSeconds: c.PollIntervalSeconds,
	}
	cfg.ApplyDefaults()
	return cfg
}
