package polling

import (
	"context"
	"log/slog"

	"skyline-mcp/internal/email"
)

// EmailInboxSource polls an IMAP inbox for new messages.
// It implements PollSource and returns a summary of the inbox state.
type EmailInboxSource struct {
	cfg    *email.EmailConfig
	logger *slog.Logger
	folder string
	limit  int
}

// EmailInboxSummary is the data returned by each poll cycle.
type EmailInboxSummary struct {
	Folder   string            `json:"folder"`
	Count    int               `json:"count"`
	Messages []EmailMsgSummary `json:"messages"`
}

// EmailMsgSummary is a minimal message summary for diff comparison.
type EmailMsgSummary struct {
	UID     uint32 `json:"uid"`
	From    string `json:"from"`
	Subject string `json:"subject"`
	Date    string `json:"date"`
	Unread  bool   `json:"unread"`
}

// NewEmailInboxSource creates a poll source that checks an IMAP folder.
func NewEmailInboxSource(cfg *email.EmailConfig, logger *slog.Logger) *EmailInboxSource {
	return &EmailInboxSource{
		cfg:    cfg,
		logger: logger,
		folder: "INBOX",
		limit:  50,
	}
}

func (s *EmailInboxSource) ID() string {
	return SourceID("email", "inbox", s.cfg.Address)
}

func (s *EmailInboxSource) Fetch(ctx context.Context) (any, error) {
	client := email.NewIMAPClient(s.cfg, s.logger)
	messages, err := client.ListMessages(s.folder, s.limit)
	if err != nil {
		return nil, err
	}

	summary := EmailInboxSummary{
		Folder: s.folder,
		Count:  len(messages),
	}
	for _, msg := range messages {
		summary.Messages = append(summary.Messages, EmailMsgSummary{
			UID:     msg.UID,
			From:    msg.From,
			Subject: msg.Subject,
			Date:    msg.Date,
			Unread:  !msg.Seen,
		})
	}

	return summary, nil
}
