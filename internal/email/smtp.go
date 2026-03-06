package email

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
)

// SendEmail sends an email via SMTP.
func SendEmail(cfg *EmailConfig, to []string, cc []string, subject, body string, html bool) error {
	if !cfg.HasSMTP() {
		return fmt.Errorf("SMTP not configured")
	}

	// Build the email message
	from := cfg.Address
	var msg strings.Builder
	msg.WriteString("From: " + from + "\r\n")
	msg.WriteString("To: " + strings.Join(to, ", ") + "\r\n")
	if len(cc) > 0 {
		msg.WriteString("Cc: " + strings.Join(cc, ", ") + "\r\n")
	}
	msg.WriteString("Subject: " + subject + "\r\n")
	msg.WriteString("MIME-Version: 1.0\r\n")
	if html {
		msg.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n")
	} else {
		msg.WriteString("Content-Type: text/plain; charset=\"UTF-8\"\r\n")
	}
	msg.WriteString("\r\n")
	msg.WriteString(body)

	// All recipients (to + cc)
	allRecipients := make([]string, 0, len(to)+len(cc))
	allRecipients = append(allRecipients, to...)
	allRecipients = append(allRecipients, cc...)

	addr := fmt.Sprintf("%s:%d", cfg.SMTPHost, cfg.SMTPPort)

	switch cfg.SMTPTLS {
	case "ssl":
		return sendSSL(addr, cfg, from, allRecipients, msg.String())
	case "none":
		return sendPlain(addr, cfg, from, allRecipients, msg.String())
	default: // "starttls"
		return sendSTARTTLS(addr, cfg, from, allRecipients, msg.String())
	}
}

func sendSTARTTLS(addr string, cfg *EmailConfig, from string, to []string, msg string) error {
	c, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("smtp dial: %w", err)
	}
	defer c.Close()

	host, _, _ := net.SplitHostPort(addr)
	tlsCfg := &tls.Config{ServerName: host}
	if err := c.StartTLS(tlsCfg); err != nil {
		return fmt.Errorf("smtp starttls: %w", err)
	}

	auth := smtp.PlainAuth("", cfg.Address, cfg.Password, host)
	if err := c.Auth(auth); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("smtp mail: %w", err)
	}
	for _, rcpt := range to {
		if err := c.Rcpt(rcpt); err != nil {
			return fmt.Errorf("smtp rcpt %s: %w", rcpt, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp close: %w", err)
	}
	return c.Quit()
}

func sendSSL(addr string, cfg *EmailConfig, from string, to []string, msg string) error {
	host, _, _ := net.SplitHostPort(addr)
	tlsCfg := &tls.Config{ServerName: host}

	conn, err := tls.Dial("tcp", addr, tlsCfg)
	if err != nil {
		return fmt.Errorf("smtp ssl dial: %w", err)
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp client: %w", err)
	}
	defer c.Close()

	auth := smtp.PlainAuth("", cfg.Address, cfg.Password, host)
	if err := c.Auth(auth); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("smtp mail: %w", err)
	}
	for _, rcpt := range to {
		if err := c.Rcpt(rcpt); err != nil {
			return fmt.Errorf("smtp rcpt %s: %w", rcpt, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp close: %w", err)
	}
	return c.Quit()
}

func sendPlain(addr string, cfg *EmailConfig, from string, to []string, msg string) error {
	host, _, _ := net.SplitHostPort(addr)
	auth := smtp.PlainAuth("", cfg.Address, cfg.Password, host)
	return smtp.SendMail(addr, auth, from, to, []byte(msg))
}
