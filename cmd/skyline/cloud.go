package main

import (
	"context"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	cloudGatewayWSPath   = "/tunnel/connect"
	tunnelInitialBackoff = 2 * time.Second
	tunnelMaxBackoff     = 5 * time.Minute
	tunnelPingInterval   = 30 * time.Second
	tunnelPongTimeout    = 10 * time.Second
)

// cloudTunnel manages an outbound WebSocket connection to the Skyline Cloud gateway.
type cloudTunnel struct {
	endpoint string // e.g. "https://cloud.xskyline.com"
	apiKey   string
	logger   *slog.Logger

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// startCloudTunnel initiates an outbound WebSocket connection to the cloud gateway.
// It runs in the background and reconnects with exponential backoff on failure.
// Returns nil if no API key is configured (standalone mode).
func startCloudTunnel(ctx context.Context, endpoint, apiKey string, logger *slog.Logger) *cloudTunnel {
	if apiKey == "" {
		return nil
	}

	tctx, cancel := context.WithCancel(ctx)
	t := &cloudTunnel{
		endpoint: endpoint,
		apiKey:   apiKey,
		logger:   logger,
		ctx:      tctx,
		cancel:   cancel,
	}

	t.wg.Add(1)
	go t.connectLoop()

	return t
}

// Stop gracefully shuts down the cloud tunnel connection.
func (t *cloudTunnel) Stop() {
	t.cancel()
	t.wg.Wait()
}

// connectLoop handles connection and reconnection with exponential backoff.
func (t *cloudTunnel) connectLoop() {
	defer t.wg.Done()

	attempt := 0
	for {
		select {
		case <-t.ctx.Done():
			t.logger.Info("cloud tunnel shutting down")
			return
		default:
		}

		err := t.connect()
		if err == nil {
			// Clean disconnect (e.g. server asked us to close)
			attempt = 0
			continue
		}

		attempt++
		backoff := calcBackoff(attempt, tunnelInitialBackoff, tunnelMaxBackoff)
		t.logger.Warn("cloud tunnel disconnected, reconnecting",
			"error", err,
			"attempt", attempt,
			"backoff", backoff,
		)

		select {
		case <-t.ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
}

// connect establishes a single WebSocket connection and blocks until it closes.
func (t *cloudTunnel) connect() error {
	wsURL := t.wsURL()
	t.logger.Info("connecting to cloud gateway", "url", wsURL)

	header := http.Header{}
	header.Set("Authorization", "Bearer "+t.apiKey)
	header.Set("User-Agent", "skyline-mcp/"+Version)

	conn, resp, err := websocket.DefaultDialer.DialContext(t.ctx, wsURL, header)
	if err != nil {
		if resp != nil {
			return &tunnelError{StatusCode: resp.StatusCode, Err: err}
		}
		return err
	}
	defer conn.Close()

	t.logger.Info("cloud tunnel connected", "url", wsURL)

	// Set up pong handler for keepalive
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(tunnelPingInterval + tunnelPongTimeout))
	})

	// Start ping ticker in background
	pingDone := make(chan struct{})
	go func() {
		defer close(pingDone)
		ticker := time.NewTicker(tunnelPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-t.ctx.Done():
				return
			case <-ticker.C:
				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
					t.logger.Debug("cloud tunnel ping failed", "error", err)
					return
				}
			}
		}
	}()

	// Read loop — keeps connection alive and processes messages from gateway
	for {
		select {
		case <-t.ctx.Done():
			// Send close message before exiting
			_ = conn.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, "shutting down"),
				time.Now().Add(3*time.Second),
			)
			<-pingDone
			return nil
		default:
		}

		_ = conn.SetReadDeadline(time.Now().Add(tunnelPingInterval + tunnelPongTimeout))
		msgType, msg, err := conn.ReadMessage()
		if err != nil {
			<-pingDone
			if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				t.logger.Info("cloud tunnel closed by server")
				return nil
			}
			return err
		}

		t.logger.Debug("cloud tunnel message received",
			"type", msgType,
			"size", len(msg),
		)
	}
}

// wsURL converts the HTTPS endpoint to a WSS URL for the tunnel.
func (t *cloudTunnel) wsURL() string {
	// Convert https://cloud.xskyline.com to wss://gateway.xskyline.com/tunnel/connect
	base := t.endpoint
	base = strings.Replace(base, "https://cloud.", "wss://gateway.", 1)
	base = strings.Replace(base, "http://cloud.", "ws://gateway.", 1)
	return strings.TrimRight(base, "/") + cloudGatewayWSPath
}

// calcBackoff returns exponential backoff duration capped at maxBackoff.
func calcBackoff(attempt int, initial, max time.Duration) time.Duration {
	backoff := time.Duration(float64(initial) * math.Pow(2, float64(attempt-1)))
	if backoff > max {
		return max
	}
	return backoff
}

// tunnelError wraps a WebSocket dial error with the HTTP status code.
type tunnelError struct {
	StatusCode int
	Err        error
}

func (e *tunnelError) Error() string {
	return e.Err.Error()
}

func (e *tunnelError) Unwrap() error {
	return e.Err
}
