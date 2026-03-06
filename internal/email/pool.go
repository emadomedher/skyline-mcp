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

// IMAPPool maintains a pool of authenticated IMAP connections for reuse.
// In persistent mode, tool calls borrow connections from the pool instead
// of dialing fresh TCP+TLS connections each time.
type IMAPPool struct {
	cfg    *EmailConfig
	logger *slog.Logger

	mu     sync.Mutex
	conns  chan *poolConn // buffered channel acts as the pool
	size   int
	closed bool
}

// poolConn wraps an imapclient.Client with metadata.
type poolConn struct {
	client    *imapclient.Client
	createdAt time.Time
	lastUsed  time.Time
}

// maxConnAge is the maximum time a connection can live before being replaced.
const maxConnAge = 15 * time.Minute

// NewIMAPPool creates a connection pool for the given email config.
// poolSize controls how many idle connections to maintain (default: 2).
func NewIMAPPool(cfg *EmailConfig, poolSize int, logger *slog.Logger) *IMAPPool {
	if poolSize <= 0 {
		poolSize = 2
	}
	return &IMAPPool{
		cfg:    cfg,
		logger: logger,
		conns:  make(chan *poolConn, poolSize),
		size:   poolSize,
	}
}

// Get borrows an authenticated IMAP connection from the pool.
// If no idle connection is available, a new one is dialed.
// The caller MUST call Put() when done (or Close the connection on error).
func (p *IMAPPool) Get() (*imapclient.Client, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, fmt.Errorf("pool is closed")
	}
	p.mu.Unlock()

	// Try to get an idle connection (non-blocking)
	for {
		select {
		case pc := <-p.conns:
			// Check age — discard stale connections
			if time.Since(pc.createdAt) > maxConnAge {
				p.logger.Debug("pool: discarding stale connection", "age", time.Since(pc.createdAt))
				pc.client.Close()
				continue
			}
			// Health check with NOOP
			if err := pc.client.Noop().Wait(); err != nil {
				p.logger.Debug("pool: discarding dead connection", "error", err)
				pc.client.Close()
				continue
			}
			pc.lastUsed = time.Now()
			return pc.client, nil
		default:
			// No idle connection — dial a new one
			return p.dial()
		}
	}
}

// Put returns a connection to the pool. If the pool is full, the connection
// is closed instead. If the connection is unhealthy, close it and don't return.
func (p *IMAPPool) Put(client *imapclient.Client) {
	if client == nil {
		return
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		client.Logout().Wait()
		client.Close()
		return
	}
	p.mu.Unlock()

	pc := &poolConn{
		client:    client,
		createdAt: time.Now(), // refreshed on put
		lastUsed:  time.Now(),
	}

	select {
	case p.conns <- pc:
		// returned to pool
	default:
		// pool full — close the extra connection
		p.logger.Debug("pool: pool full, closing extra connection")
		func() {
			client.Logout().Wait()
			client.Close()
		}()
	}
}

// Discard closes a connection without returning it to the pool.
// Use this when the connection encountered an error.
func (p *IMAPPool) Discard(client *imapclient.Client) {
	if client == nil {
		return
	}
	func() {
		client.Logout().Wait()
		client.Close()
	}()
}

// Close drains and closes all pooled connections.
func (p *IMAPPool) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	p.mu.Unlock()

	// Drain all idle connections
	close(p.conns)
	for pc := range p.conns {
		func() {
			pc.client.Logout().Wait()
			pc.client.Close()
		}()
	}
	p.logger.Info("IMAP pool closed", "address", p.cfg.Address)
}

// Stats returns pool statistics.
func (p *IMAPPool) Stats() (idle, capacity int) {
	return len(p.conns), p.size
}

// dial creates a new authenticated IMAP connection.
func (p *IMAPPool) dial() (*imapclient.Client, error) {
	return dialIMAP(p.cfg, p.logger)
}

// dialIMAP dials and authenticates a single IMAP connection.
// Shared by the pool and the IDLE listener.
func dialIMAP(cfg *EmailConfig, logger *slog.Logger) (*imapclient.Client, error) {
	addr := fmt.Sprintf("%s:%d", cfg.IMAPHost, cfg.IMAPPort)

	opts := &imapclient.Options{
		TLSConfig: &tls.Config{ServerName: cfg.IMAPHost},
	}

	var client *imapclient.Client
	var err error
	if cfg.IMAPPort == 993 {
		client, err = imapclient.DialTLS(addr, opts)
	} else {
		client, err = imapclient.DialStartTLS(addr, opts)
	}
	if err != nil {
		return nil, fmt.Errorf("imap dial %s: %w", addr, err)
	}

	if err := client.Login(cfg.Address, cfg.Password).Wait(); err != nil {
		client.Close()
		return nil, fmt.Errorf("imap login: %w", err)
	}

	logger.Debug("imap: dialed new connection", "address", cfg.Address)
	return client, nil
}

// dialWithBackoff dials an IMAP connection with exponential backoff retry.
// Used by persistent components (pool warmup, IDLE listener) that must reconnect
// after network failures. Returns the connected client or an error if ctx is cancelled.
func dialWithBackoff(ctx context.Context, cfg *EmailConfig, logger *slog.Logger) (*imapclient.Client, error) {
	backoff := 1 * time.Second
	const maxBackoff = 2 * time.Minute

	for {
		client, err := dialIMAP(cfg, logger)
		if err == nil {
			return client, nil
		}

		logger.Warn("imap: connection failed, retrying", "error", err, "backoff", backoff, "address", cfg.Address)

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("imap: connection cancelled: %w", ctx.Err())
		case <-time.After(backoff):
		}

		// Exponential backoff: 1s → 2s → 4s → 8s → ... → 2m (cap)
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}
