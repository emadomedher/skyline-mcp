package email

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2/imapclient"
)

// NewEmailEvent is emitted when IDLE detects new messages.
type NewEmailEvent struct {
	Account    string // email address
	Folder     string // mailbox name (e.g. "INBOX")
	NewCount   uint32 // count of new messages since last check
	TotalCount uint32 // total messages in folder after update
}

// IDLEListener maintains a persistent IMAP connection in IDLE mode
// to receive real-time push notifications for new emails.
// When new messages arrive, it calls the registered callback.
type IDLEListener struct {
	cfg    *EmailConfig
	folder string
	logger *slog.Logger
	onNew  func(NewEmailEvent)

	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
}

// NewIDLEListener creates an IDLE listener for the given email account.
// folder is the mailbox to watch (usually "INBOX").
// onNew is called whenever new messages are detected.
func NewIDLEListener(cfg *EmailConfig, folder string, logger *slog.Logger, onNew func(NewEmailEvent)) *IDLEListener {
	if folder == "" {
		folder = "INBOX"
	}
	return &IDLEListener{
		cfg:    cfg,
		folder: folder,
		logger: logger,
		onNew:  onNew,
	}
}

// Start begins the IDLE loop in a background goroutine.
// It connects to IMAP, SELECTs the folder, and enters IDLE.
// On connection loss, it reconnects with exponential backoff.
// Call Stop() to terminate.
func (l *IDLEListener) Start() {
	l.mu.Lock()
	if l.cancel != nil {
		l.mu.Unlock()
		return // already running
	}
	l.ctx, l.cancel = context.WithCancel(context.Background())
	l.mu.Unlock()

	go l.loop()
	l.logger.Info("IDLE listener started", "address", l.cfg.Address, "folder", l.folder)
}

// Stop terminates the IDLE listener.
func (l *IDLEListener) Stop() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cancel != nil {
		l.cancel()
		l.cancel = nil
		l.logger.Info("IDLE listener stopped", "address", l.cfg.Address, "folder", l.folder)
	}
}

// loop is the main IDLE loop. It reconnects on failure with backoff.
func (l *IDLEListener) loop() {
	for {
		if err := l.ctx.Err(); err != nil {
			return // stopped
		}

		l.logger.Debug("IDLE: connecting", "address", l.cfg.Address, "folder", l.folder)
		client, mailboxCh, err := l.dialWithHandler()
		if err != nil {
			if l.ctx.Err() != nil {
				return // stopped during dial
			}
			l.logger.Warn("IDLE: connection failed, will retry", "error", err, "address", l.cfg.Address)
			select {
			case <-l.ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}

		err = l.runIDLE(client, mailboxCh)
		// Clean up connection
		func() {
			client.Logout().Wait()
			client.Close()
		}()

		if err != nil {
			if l.ctx.Err() != nil {
				return // stopped
			}
			l.logger.Warn("IDLE: session ended, will reconnect", "error", err, "address", l.cfg.Address)
			select {
			case <-l.ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
		}
	}
}

// dialWithHandler dials an IMAP connection with a UnilateralDataHandler
// that signals mailboxCh when the server pushes mailbox updates (e.g. EXISTS).
func (l *IDLEListener) dialWithHandler() (*imapclient.Client, <-chan uint32, error) {
	mailboxCh := make(chan uint32, 16)

	opts := &imapclient.Options{
		TLSConfig: &tls.Config{ServerName: l.cfg.IMAPHost},
		UnilateralDataHandler: &imapclient.UnilateralDataHandler{
			Mailbox: func(data *imapclient.UnilateralDataMailbox) {
				if data.NumMessages != nil {
					select {
					case mailboxCh <- *data.NumMessages:
					default:
						// channel full, drop oldest
					}
				}
			},
		},
	}

	addr := fmt.Sprintf("%s:%d", l.cfg.IMAPHost, l.cfg.IMAPPort)
	var client *imapclient.Client
	var err error
	if l.cfg.IMAPPort == 993 {
		client, err = imapclient.DialTLS(addr, opts)
	} else {
		client, err = imapclient.DialStartTLS(addr, opts)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("imap dial %s: %w", addr, err)
	}

	if err := client.Login(l.cfg.Address, l.cfg.Password).Wait(); err != nil {
		client.Close()
		return nil, nil, fmt.Errorf("imap login: %w", err)
	}

	l.logger.Debug("IDLE: connected", "address", l.cfg.Address)
	return client, mailboxCh, nil
}

// runIDLE runs a single IDLE session on an established connection.
// mailboxCh receives the new NumMessages count from the UnilateralDataHandler.
// Returns when the connection drops or an error occurs.
func (l *IDLEListener) runIDLE(client *imapclient.Client, mailboxCh <-chan uint32) error {
	// SELECT the folder
	selData, err := client.Select(l.folder, nil).Wait()
	if err != nil {
		return fmt.Errorf("select %s: %w", l.folder, err)
	}
	lastCount := selData.NumMessages
	l.logger.Debug("IDLE: folder selected", "folder", l.folder, "messages", lastCount)

	for {
		if err := l.ctx.Err(); err != nil {
			return nil // stopped
		}

		// Enter IDLE
		idleCmd, err := client.Idle()
		if err != nil {
			return fmt.Errorf("idle start: %w", err)
		}

		// Wait for either: mailbox update, context cancel, or connection drop
		var newCount uint32
		gotUpdate := false

		select {
		case <-l.ctx.Done():
			_ = idleCmd.Close()
			return nil // stopped

		case count, ok := <-mailboxCh:
			if !ok {
				_ = idleCmd.Close()
				return fmt.Errorf("mailbox channel closed")
			}
			newCount = count
			gotUpdate = true

			// Drain any additional updates that arrived simultaneously
			for {
				select {
				case c := <-mailboxCh:
					newCount = c
				default:
					goto drained
				}
			}
		drained:

			// Stop IDLE so we can resume normal commands
			if err := idleCmd.Close(); err != nil {
				return fmt.Errorf("idle close: %w", err)
			}
		}

		if gotUpdate && newCount > lastCount {
			diff := newCount - lastCount
			l.logger.Info("IDLE: new messages detected",
				"folder", l.folder,
				"new", diff,
				"total", newCount,
				"address", l.cfg.Address,
			)
			if l.onNew != nil {
				l.onNew(NewEmailEvent{
					Account:    l.cfg.Address,
					Folder:     l.folder,
					NewCount:   diff,
					TotalCount: newCount,
				})
			}
		}
		if gotUpdate {
			lastCount = newCount
		}
	}
}
