package email

import (
	"fmt"
	"log/slog"
	"sync"
)

// NotifyFunc is called when the IDLE listener detects new emails.
// The URI is the MCP resource URI (e.g. "email://myemail/inbox").
type NotifyFunc func(uri string)

// PersistentManager manages IMAP connection pools and IDLE listeners
// for all persistent-mode email accounts. It bridges the email package
// with the MCP notification layer.
type PersistentManager struct {
	mu       sync.Mutex
	accounts map[string]*persistentAccount // keyed by API name
	logger   *slog.Logger
}

type persistentAccount struct {
	apiName  string
	pool     *IMAPPool
	idle     *IDLEListener
	cfg      *EmailConfig
	notifyFn NotifyFunc
}

// NewPersistentManager creates a manager for persistent email connections.
func NewPersistentManager(logger *slog.Logger) *PersistentManager {
	return &PersistentManager{
		accounts: make(map[string]*persistentAccount),
		logger:   logger,
	}
}

// InboxURI returns the MCP resource URI for an email account's inbox.
func InboxURI(apiName string) string {
	return fmt.Sprintf("email://%s/inbox", apiName)
}

// Register adds a persistent email account. Starts the connection pool
// and IDLE listener. The notifyFn is called when new emails arrive.
func (m *PersistentManager) Register(apiName string, cfg *EmailConfig, notifyFn NotifyFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Stop existing if re-registering
	if existing, ok := m.accounts[apiName]; ok {
		existing.stop()
	}

	acct := &persistentAccount{
		apiName:  apiName,
		cfg:      cfg,
		notifyFn: notifyFn,
		pool:     NewIMAPPool(cfg, 2, m.logger),
	}

	// Start IDLE listener with notification callback
	uri := InboxURI(apiName)
	acct.idle = NewIDLEListener(cfg, "INBOX", m.logger, func(event NewEmailEvent) {
		m.logger.Info("new email detected via IDLE",
			"api", apiName,
			"account", event.Account,
			"new_count", event.NewCount,
			"uri", uri,
		)
		if notifyFn != nil {
			notifyFn(uri)
		}
	})
	acct.idle.Start()

	m.accounts[apiName] = acct
	m.logger.Info("persistent email registered",
		"api", apiName,
		"address", cfg.Address,
		"pool_size", 2,
	)
}

// Unregister stops and removes a persistent email account.
func (m *PersistentManager) Unregister(apiName string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if acct, ok := m.accounts[apiName]; ok {
		acct.stop()
		delete(m.accounts, apiName)
		m.logger.Info("persistent email unregistered", "api", apiName)
	}
}

// GetPool returns the IMAP connection pool for an API, or nil if not registered.
func (m *PersistentManager) GetPool(apiName string) *IMAPPool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if acct, ok := m.accounts[apiName]; ok {
		return acct.pool
	}
	return nil
}

// Close stops all persistent accounts.
func (m *PersistentManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, acct := range m.accounts {
		acct.stop()
		delete(m.accounts, name)
	}
	m.logger.Info("persistent email manager closed")
}

// Stats returns pool/IDLE status for all persistent accounts.
func (m *PersistentManager) Stats() map[string]map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make(map[string]map[string]any, len(m.accounts))
	for name, acct := range m.accounts {
		idle, capacity := acct.pool.Stats()
		result[name] = map[string]any{
			"address":       acct.cfg.Address,
			"pool_idle":     idle,
			"pool_capacity": capacity,
			"idle_active":   acct.idle != nil,
		}
	}
	return result
}

func (a *persistentAccount) stop() {
	if a.idle != nil {
		a.idle.Stop()
	}
	if a.pool != nil {
		a.pool.Close()
	}
}
