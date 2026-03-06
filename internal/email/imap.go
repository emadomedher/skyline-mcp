package email

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	gomessage "github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset"
)

// EmailMessage represents a single email message.
type EmailMessage struct {
	UID     uint32 `json:"uid"`
	Subject string `json:"subject"`
	From    string `json:"from"`
	To      string `json:"to"`
	Date    string `json:"date"`
	Preview string `json:"preview,omitempty"` // First ~200 chars of body
	Body    string `json:"body,omitempty"`    // Full body (only when reading single message)
	Seen    bool   `json:"seen"`
	Folder  string `json:"folder"`
}

// FolderInfo describes an IMAP mailbox folder.
type FolderInfo struct {
	Name       string `json:"name"`
	Attributes string `json:"attributes,omitempty"`
}

// IMAPClient wraps go-imap for simplified operations.
// When pool is set (persistent mode), connections are borrowed from the pool
// instead of dialing fresh ones each time.
type IMAPClient struct {
	cfg    *EmailConfig
	pool   *IMAPPool
	logger *slog.Logger
}

// NewIMAPClient creates a new IMAP client wrapper (basic mode — connect per call).
func NewIMAPClient(cfg *EmailConfig, logger *slog.Logger) *IMAPClient {
	return &IMAPClient{cfg: cfg, logger: logger}
}

// NewIMAPClientWithPool creates an IMAP client that borrows connections from a pool.
func NewIMAPClientWithPool(cfg *EmailConfig, pool *IMAPPool, logger *slog.Logger) *IMAPClient {
	return &IMAPClient{cfg: cfg, pool: pool, logger: logger}
}

// connect establishes an authenticated IMAP connection.
// In persistent mode, borrows from the pool. In basic mode, dials a new connection.
func (c *IMAPClient) connect() (*imapclient.Client, error) {
	if c.pool != nil {
		return c.pool.Get()
	}

	addr := fmt.Sprintf("%s:%d", c.cfg.IMAPHost, c.cfg.IMAPPort)

	opts := &imapclient.Options{
		TLSConfig: &tls.Config{ServerName: c.cfg.IMAPHost},
	}

	var client *imapclient.Client
	var err error
	if c.cfg.IMAPPort == 993 {
		client, err = imapclient.DialTLS(addr, opts)
	} else {
		client, err = imapclient.DialStartTLS(addr, opts)
	}
	if err != nil {
		return nil, fmt.Errorf("imap connect %s: %w", addr, err)
	}

	if err := client.Login(c.cfg.Address, c.cfg.Password).Wait(); err != nil {
		client.Close()
		return nil, fmt.Errorf("imap login: %w", err)
	}

	return client, nil
}

// release returns a connection to the pool (persistent mode) or closes it (basic mode).
// Use discardOnError=true when the connection encountered an error.
func (c *IMAPClient) release(client *imapclient.Client, discardOnError bool) {
	if c.pool != nil {
		if discardOnError {
			c.pool.Discard(client)
		} else {
			c.pool.Put(client)
		}
		return
	}
	// Basic mode: always close
	client.Logout().Wait()
	client.Close()
}

// ListFolders returns all available IMAP mailbox folders.
func (c *IMAPClient) ListFolders() ([]FolderInfo, error) {
	client, err := c.connect()
	if err != nil {
		return nil, err
	}
	connErr := false
	defer func() { c.release(client, connErr) }()

	mailboxes, err := client.List("", "*", nil).Collect()
	if err != nil {
		connErr = true
		return nil, fmt.Errorf("imap list: %w", err)
	}

	var folders []FolderInfo
	for _, mb := range mailboxes {
		attrs := make([]string, len(mb.Attrs))
		for i, a := range mb.Attrs {
			attrs[i] = string(a)
		}
		fi := FolderInfo{
			Name:       mb.Mailbox,
			Attributes: strings.Join(attrs, ", "),
		}
		folders = append(folders, fi)
	}

	return folders, nil
}

// ListMessages lists messages in a folder. Returns up to `limit` messages starting from the newest.
func (c *IMAPClient) ListMessages(folder string, limit int) ([]EmailMessage, error) {
	if folder == "" {
		folder = "INBOX"
	}
	if limit <= 0 {
		limit = 20
	}

	client, err := c.connect()
	if err != nil {
		return nil, err
	}
	connErr := false
	defer func() { c.release(client, connErr) }()

	selData, err := client.Select(folder, nil).Wait()
	if err != nil {
		connErr = true
		return nil, fmt.Errorf("imap select %s: %w", folder, err)
	}

	total := selData.NumMessages
	if total == 0 {
		return []EmailMessage{}, nil
	}

	// Build sequence set: last N messages
	start := uint32(1)
	if total > uint32(limit) {
		start = total - uint32(limit) + 1
	}
	var seqSet imap.SeqSet
	seqSet.AddRange(start, total)

	fetchOpts := &imap.FetchOptions{
		UID:      true,
		Envelope: true,
		Flags:    true,
	}

	fetchCmd := client.Fetch(seqSet, fetchOpts)
	var messages []EmailMessage

	for {
		msgData := fetchCmd.Next()
		if msgData == nil {
			break
		}

		var msg EmailMessage
		msg.Folder = folder

		for {
			item := msgData.Next()
			if item == nil {
				break
			}

			switch data := item.(type) {
			case imapclient.FetchItemDataUID:
				msg.UID = uint32(data.UID)
			case imapclient.FetchItemDataFlags:
				for _, f := range data.Flags {
					if f == imap.FlagSeen {
						msg.Seen = true
					}
				}
			case imapclient.FetchItemDataEnvelope:
				env := data.Envelope
				msg.Subject = env.Subject
				if !env.Date.IsZero() {
					msg.Date = env.Date.Format(time.RFC3339)
				}
				if len(env.From) > 0 {
					msg.From = formatAddr(env.From[0])
				}
				if len(env.To) > 0 {
					addrs := make([]string, len(env.To))
					for i, a := range env.To {
						addrs[i] = formatAddr(a)
					}
					msg.To = strings.Join(addrs, ", ")
				}
			case imapclient.FetchItemDataBodySection:
				// handled elsewhere (ReadMessage)
			}
		}

		messages = append(messages, msg)
	}

	if err := fetchCmd.Close(); err != nil {
		connErr = true
		return messages, fmt.Errorf("imap fetch: %w", err)
	}

	// Reverse so newest is first
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	return messages, nil
}

// ReadMessage fetches a single message by UID with full body.
func (c *IMAPClient) ReadMessage(folder string, uid uint32) (*EmailMessage, error) {
	if folder == "" {
		folder = "INBOX"
	}

	client, err := c.connect()
	if err != nil {
		return nil, err
	}
	connErr := false
	defer func() { c.release(client, connErr) }()

	if _, err := client.Select(folder, nil).Wait(); err != nil {
		connErr = true
		return nil, fmt.Errorf("imap select %s: %w", folder, err)
	}

	var uidSet imap.UIDSet
	uidSet.AddNum(imap.UID(uid))

	fetchOpts := &imap.FetchOptions{
		UID:      true,
		Envelope: true,
		Flags:    true,
		BodySection: []*imap.FetchItemBodySection{
			{Peek: true},
		},
	}

	fetchCmd := client.Fetch(uidSet, fetchOpts)
	msgData := fetchCmd.Next()
	if msgData == nil {
		fetchCmd.Close()
		return nil, fmt.Errorf("message UID %d not found in %s", uid, folder)
	}

	msg := &EmailMessage{
		Folder: folder,
	}

	for {
		item := msgData.Next()
		if item == nil {
			break
		}

		switch data := item.(type) {
		case imapclient.FetchItemDataUID:
			msg.UID = uint32(data.UID)
		case imapclient.FetchItemDataFlags:
			for _, f := range data.Flags {
				if f == imap.FlagSeen {
					msg.Seen = true
				}
			}
		case imapclient.FetchItemDataEnvelope:
			env := data.Envelope
			msg.Subject = env.Subject
			if !env.Date.IsZero() {
				msg.Date = env.Date.Format(time.RFC3339)
			}
			if len(env.From) > 0 {
				msg.From = formatAddr(env.From[0])
			}
			if len(env.To) > 0 {
				addrs := make([]string, len(env.To))
				for i, a := range env.To {
					addrs[i] = formatAddr(a)
				}
				msg.To = strings.Join(addrs, ", ")
			}
		case imapclient.FetchItemDataBodySection:
			bodyBytes, _ := io.ReadAll(data.Literal)
			msg.Body = extractTextBody(bodyBytes)
		}
	}

	fetchCmd.Close()

	return msg, nil
}

// SearchMessages searches for messages matching a query in the given folder.
func (c *IMAPClient) SearchMessages(folder string, query string, limit int) ([]EmailMessage, error) {
	if folder == "" {
		folder = "INBOX"
	}
	if limit <= 0 {
		limit = 20
	}

	client, err := c.connect()
	if err != nil {
		return nil, err
	}
	connErr := false
	defer func() { c.release(client, connErr) }()

	if _, err := client.Select(folder, nil).Wait(); err != nil {
		connErr = true
		return nil, fmt.Errorf("imap select %s: %w", folder, err)
	}

	// Build IMAP search criteria — search in subject and from
	criteria := &imap.SearchCriteria{
		Or: [][2]imap.SearchCriteria{
			{
				{Header: []imap.SearchCriteriaHeaderField{{Key: "Subject", Value: query}}},
				{Header: []imap.SearchCriteriaHeaderField{{Key: "From", Value: query}}},
			},
		},
	}

	searchData, err := client.UIDSearch(criteria, nil).Wait()
	if err != nil {
		connErr = true
		return nil, fmt.Errorf("imap search: %w", err)
	}

	uids := searchData.AllUIDs()
	if len(uids) == 0 {
		return []EmailMessage{}, nil
	}

	// Limit results (take the last N for newest)
	if len(uids) > limit {
		uids = uids[len(uids)-limit:]
	}

	var uidSet imap.UIDSet
	uidSet.AddNum(uids...)

	fetchOpts := &imap.FetchOptions{
		UID:      true,
		Envelope: true,
		Flags:    true,
	}

	fetchCmd := client.Fetch(uidSet, fetchOpts)
	var messages []EmailMessage

	for {
		msgData := fetchCmd.Next()
		if msgData == nil {
			break
		}

		var msg EmailMessage
		msg.Folder = folder

		for {
			item := msgData.Next()
			if item == nil {
				break
			}

			switch data := item.(type) {
			case imapclient.FetchItemDataUID:
				msg.UID = uint32(data.UID)
			case imapclient.FetchItemDataFlags:
				for _, f := range data.Flags {
					if f == imap.FlagSeen {
						msg.Seen = true
					}
				}
			case imapclient.FetchItemDataEnvelope:
				env := data.Envelope
				msg.Subject = env.Subject
				if !env.Date.IsZero() {
					msg.Date = env.Date.Format(time.RFC3339)
				}
				if len(env.From) > 0 {
					msg.From = formatAddr(env.From[0])
				}
				if len(env.To) > 0 {
					addrs := make([]string, len(env.To))
					for i, a := range env.To {
						addrs[i] = formatAddr(a)
					}
					msg.To = strings.Join(addrs, ", ")
				}
			}
		}

		messages = append(messages, msg)
	}

	fetchCmd.Close()

	// Reverse so newest is first
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	return messages, nil
}

// MarkRead marks a message as seen.
func (c *IMAPClient) MarkRead(folder string, uid uint32) error {
	return c.setFlag(folder, uid, imap.FlagSeen, true)
}

// DeleteMessage marks a message as deleted.
func (c *IMAPClient) DeleteMessage(folder string, uid uint32) error {
	return c.setFlag(folder, uid, imap.FlagDeleted, true)
}

// MoveMessage moves a message to a different folder.
func (c *IMAPClient) MoveMessage(fromFolder string, uid uint32, toFolder string) error {
	if fromFolder == "" {
		fromFolder = "INBOX"
	}

	client, err := c.connect()
	if err != nil {
		return err
	}
	connErr := false
	defer func() { c.release(client, connErr) }()

	if _, err := client.Select(fromFolder, nil).Wait(); err != nil {
		connErr = true
		return fmt.Errorf("imap select %s: %w", fromFolder, err)
	}

	var uidSet imap.UIDSet
	uidSet.AddNum(imap.UID(uid))

	if _, err := client.Move(uidSet, toFolder).Wait(); err != nil {
		connErr = true
		return fmt.Errorf("imap move: %w", err)
	}

	return nil
}

// GetNewMessageCount returns the count of unseen messages (for polling).
func (c *IMAPClient) GetNewMessageCount(folder string) (uint32, error) {
	if folder == "" {
		folder = "INBOX"
	}

	client, err := c.connect()
	if err != nil {
		return 0, err
	}
	connErr := false
	defer func() { c.release(client, connErr) }()

	statusOpts := &imap.StatusOptions{
		NumMessages: true,
		NumUnseen:   true,
	}
	statusData, err := client.Status(folder, statusOpts).Wait()
	if err != nil {
		connErr = true
		return 0, fmt.Errorf("imap status: %w", err)
	}

	if statusData.NumUnseen != nil {
		return *statusData.NumUnseen, nil
	}
	return 0, nil
}

// setFlag sets or unsets a flag on a message.
func (c *IMAPClient) setFlag(folder string, uid uint32, flag imap.Flag, set bool) error {
	if folder == "" {
		folder = "INBOX"
	}

	client, err := c.connect()
	if err != nil {
		return err
	}
	connErr := false
	defer func() { c.release(client, connErr) }()

	if _, err := client.Select(folder, nil).Wait(); err != nil {
		connErr = true
		return fmt.Errorf("imap select %s: %w", folder, err)
	}

	var uidSet imap.UIDSet
	uidSet.AddNum(imap.UID(uid))

	var storeFlags imap.StoreFlags
	if set {
		storeFlags.Op = imap.StoreFlagsAdd
	} else {
		storeFlags.Op = imap.StoreFlagsDel
	}
	storeFlags.Flags = []imap.Flag{flag}

	if err := client.Store(uidSet, &storeFlags, nil).Close(); err != nil {
		connErr = true
		return fmt.Errorf("imap store: %w", err)
	}

	return nil
}

// VerifyConnection tests that IMAP login works.
func (c *IMAPClient) VerifyConnection() error {
	client, err := c.connect()
	if err != nil {
		return err
	}
	c.release(client, false)
	return nil
}

// VerifySMTPConnection tests that SMTP login works.
func VerifySMTPConnection(cfg *EmailConfig) error {
	if !cfg.HasSMTP() {
		return fmt.Errorf("SMTP not configured")
	}

	addr := fmt.Sprintf("%s:%d", cfg.SMTPHost, cfg.SMTPPort)
	host, _, _ := net.SplitHostPort(addr)

	switch cfg.SMTPTLS {
	case "ssl":
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", addr, &tls.Config{ServerName: host})
		if err != nil {
			return fmt.Errorf("smtp ssl: %w", err)
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
		return c.Quit()
	default:
		c, err := smtp.Dial(addr)
		if err != nil {
			return fmt.Errorf("smtp dial: %w", err)
		}
		defer c.Close()
		if cfg.SMTPTLS != "none" {
			if err := c.StartTLS(&tls.Config{ServerName: host}); err != nil {
				return fmt.Errorf("smtp starttls: %w", err)
			}
		}
		auth := smtp.PlainAuth("", cfg.Address, cfg.Password, host)
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
		return c.Quit()
	}
}

// formatAddr formats an IMAP address to "Name <email>" or just "email".
func formatAddr(addr imap.Address) string {
	name := addr.Name
	email := addr.Addr()
	if name != "" {
		return fmt.Sprintf("%s <%s>", name, email)
	}
	return email
}

// sanitizePreview cleans up a body preview (strips HTML tags, normalizes whitespace).
func sanitizePreview(s string, maxLen int) string {
	result := strings.Builder{}
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			if r == '\r' || r == '\n' || r == '\t' {
				r = ' '
			}
			result.WriteRune(r)
		}
	}
	cleaned := strings.Join(strings.Fields(result.String()), " ")
	if len(cleaned) > maxLen {
		cleaned = cleaned[:maxLen] + "..."
	}
	return cleaned
}

// extractTextBody extracts the plain text body from a MIME message.
func extractTextBody(raw []byte) string {
	entity, err := gomessage.Read(bytes.NewReader(raw))
	if err != nil {
		s := string(raw)
		if len(s) > 50000 {
			s = s[:50000] + "\n...[truncated]"
		}
		return s
	}

	if mr := entity.MultipartReader(); mr != nil {
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			ct, _, _ := part.Header.ContentType()
			if ct == "text/plain" {
				body, _ := io.ReadAll(part.Body)
				s := string(body)
				if len(s) > 50000 {
					s = s[:50000] + "\n...[truncated]"
				}
				return s
			}
			if ct == "text/html" {
				body, _ := io.ReadAll(part.Body)
				return sanitizePreview(string(body), 50000)
			}
		}
	}

	ct, _, _ := entity.Header.ContentType()
	body, _ := io.ReadAll(entity.Body)
	s := string(body)
	if ct == "text/html" {
		return sanitizePreview(s, 50000)
	}
	if len(s) > 50000 {
		s = s[:50000] + "\n...[truncated]"
	}
	return s
}
