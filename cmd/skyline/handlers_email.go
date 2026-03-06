package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"skyline-mcp/internal/email"
)

func (s *server) handleEmailLookup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limitBody(w, r)
	var req emailLookupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	addr := strings.TrimSpace(req.Email)
	if addr == "" || !strings.Contains(addr, "@") {
		http.Error(w, "email is required", http.StatusBadRequest)
		return
	}

	result, err := email.LookupProvider(r.Context(), addr)
	if err != nil {
		writeJSON(w, http.StatusOK, emailLookupResponse{Error: err.Error()})
		return
	}

	resp := emailLookupResponse{}
	if result.Recognized && result.Config != nil {
		resp.Provider = result.Config.Name
		resp.SMTPHost = result.Config.SMTPHost
		resp.SMTPPort = result.Config.SMTPPort
		resp.SMTPTLS = result.Config.SMTPTLS
		resp.IMAPHost = result.Config.IMAPHost
		resp.IMAPPort = result.Config.IMAPPort
		resp.POP3Host = result.Config.POP3Host
		resp.POP3Port = result.Config.POP3Port
	} else {
		resp.Provider = "unknown"
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *server) handleEmailVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limitBody(w, r)
	var req emailVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	addr := strings.TrimSpace(req.Email)
	pass := strings.TrimSpace(req.Password)
	if addr == "" || pass == "" {
		http.Error(w, "email and password are required", http.StatusBadRequest)
		return
	}

	cfg := &email.EmailConfig{
		Address:  addr,
		Password: pass,
		SMTPHost: req.SMTPHost,
		SMTPPort: req.SMTPPort,
		SMTPTLS:  req.SMTPTLS,
		IMAPHost: req.IMAPHost,
		IMAPPort: req.IMAPPort,
	}
	cfg.ApplyDefaults()

	resp := emailVerifyResponse{}

	// Test IMAP
	if cfg.IMAPHost != "" {
		client := email.NewIMAPClient(cfg, slog.Default())
		if err := client.VerifyConnection(); err != nil {
			resp.IMAP = "failed"
			resp.IMAPErr = humanizeEmailError(err.Error())
			s.logger.Debug("email imap verify failed", "email", addr, "host", cfg.IMAPHost, "error", err)
		} else {
			resp.IMAP = "ok"
		}
	} else {
		resp.IMAP = "skipped"
	}

	// Test SMTP
	if cfg.SMTPHost != "" {
		if err := email.VerifySMTPConnection(cfg); err != nil {
			resp.SMTP = "failed"
			resp.SMTPErr = humanizeEmailError(err.Error())
			s.logger.Debug("email smtp verify failed", "email", addr, "host", cfg.SMTPHost, "error", err)
		} else {
			resp.SMTP = "ok"
		}
	} else {
		resp.SMTP = "skipped"
	}

	// Overall result: ok if at least one protocol verified successfully
	resp.OK = resp.IMAP == "ok" || resp.SMTP == "ok"
	if !resp.OK {
		if resp.IMAPErr != "" {
			resp.Error = resp.IMAPErr
		} else if resp.SMTPErr != "" {
			resp.Error = resp.SMTPErr
		} else {
			resp.Error = "no servers configured to verify"
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// humanizeEmailError converts raw IMAP/SMTP protocol errors into
// user-friendly messages for the admin UI.
func humanizeEmailError(raw string) string {
	lo := strings.ToLower(raw)
	switch {
	case strings.Contains(lo, "authenticationfailed"),
		strings.Contains(lo, "invalid credentials"),
		strings.Contains(lo, "authentication failed"),
		strings.Contains(lo, "login failed"),
		strings.Contains(lo, "incorrect password"),
		strings.Contains(lo, "wrong password"):
		return "Invalid credentials — check your email and password."
	case strings.Contains(lo, "too many login attempts"),
		strings.Contains(lo, "rate limit"),
		strings.Contains(lo, "temporary"):
		return "Too many login attempts — try again later."
	case strings.Contains(lo, "less secure app"),
		strings.Contains(lo, "web login required"),
		strings.Contains(lo, "application-specific password"):
		return "Your provider requires an app password — check your account security settings."
	case strings.Contains(lo, "no such host"),
		strings.Contains(lo, "lookup"),
		strings.Contains(lo, "dns"):
		return "Server not found — check the hostname."
	case strings.Contains(lo, "connection refused"):
		return "Connection refused — check the host and port."
	case strings.Contains(lo, "connection timed out"),
		strings.Contains(lo, "i/o timeout"),
		strings.Contains(lo, "deadline exceeded"):
		return "Connection timed out — the server may be unreachable."
	case strings.Contains(lo, "tls"),
		strings.Contains(lo, "certificate"),
		strings.Contains(lo, "x509"):
		return "TLS/SSL error — try a different security setting."
	case strings.Contains(lo, "eof"),
		strings.Contains(lo, "connection reset"):
		return "Connection dropped by server — check host, port, and security settings."
	default:
		return raw
	}
}

// emailOperations returns the static list of email tool operations
// for the operation filter UI (no spec URL needed).
func emailOperations() []operationInfo {
	return []operationInfo{
		{ID: "send_email", Method: "POST", Path: "/send", Summary: "Send an email message"},
		{ID: "list_emails", Method: "GET", Path: "/messages", Summary: "List recent emails in a folder"},
		{ID: "read_email", Method: "GET", Path: "/messages/{uid}", Summary: "Read a specific email by UID"},
		{ID: "search_emails", Method: "GET", Path: "/messages/search", Summary: "Search emails by subject or sender"},
		{ID: "list_folders", Method: "GET", Path: "/folders", Summary: "List all email folders/mailboxes"},
		{ID: "mark_email_read", Method: "POST", Path: "/messages/{uid}/read", Summary: "Mark an email as read"},
		{ID: "delete_email", Method: "DELETE", Path: "/messages/{uid}", Summary: "Delete an email"},
		{ID: "move_email", Method: "POST", Path: "/messages/{uid}/move", Summary: "Move an email to a different folder"},
	}
}
