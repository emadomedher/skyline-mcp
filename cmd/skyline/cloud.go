package main

import (
	"context"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"
)

const (
	cloudGatewayWSPath   = "/tunnel/connect"
	tunnelInitialBackoff = 2 * time.Second
	tunnelMaxBackoff     = 5 * time.Minute
	tunnelPingInterval   = 30 * time.Second
	tunnelPongTimeout    = 10 * time.Second
)

// cloudTunnel manages an outbound WebSocket connection to the Skyline Cloud gateway.
// It creates a yamux session over the WebSocket so the gateway can open streams
// back to this MCP server and proxy HTTP requests through them.
type cloudTunnel struct {
	endpoint string
	apiKey   string
	handler  http.Handler // local MCP HTTP handler to serve requests through the tunnel
	logger   *slog.Logger

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// startCloudTunnel initiates an outbound WebSocket connection to the cloud gateway.
// handler is the local HTTP handler that tunnel requests will be forwarded to.
func startCloudTunnel(ctx context.Context, endpoint, apiKey string, handler http.Handler, logger *slog.Logger) *cloudTunnel {
	if apiKey == "" {
		return nil
	}

	tctx, cancel := context.WithCancel(ctx)
	t := &cloudTunnel{
		endpoint: endpoint,
		apiKey:   apiKey,
		handler:  handler,
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

// connect establishes a single WebSocket connection, reads the "connected"
// message to get the public URL, then creates a yamux client session and
// serves incoming streams as HTTP requests through the local handler.
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

	// Read the "connected" message from the gateway to get our public URL.
	var connMsg struct {
		Type      string `json:"type"`
		TunnelID  string `json:"tunnelId"`
		Subdomain string `json:"subdomain"`
		PublicURL string `json:"publicUrl"`
	}
	if err := conn.ReadJSON(&connMsg); err != nil {
		return err
	}
	if connMsg.Type != "connected" {
		return &tunnelError{Err: io.EOF}
	}

	t.logger.Info("cloud tunnel established",
		"public_url", connMsg.PublicURL,
		"subdomain", connMsg.Subdomain,
		"tunnel_id", connMsg.TunnelID,
	)

	// Wrap the WebSocket as a net.Conn for yamux.
	wsConn := newWSConn(conn)

	// Create yamux client session. The gateway is the yamux server and will
	// open streams back to us for each proxied HTTP request.
	cfg := yamux.DefaultConfig()
	cfg.KeepAliveInterval = tunnelPingInterval
	cfg.ConnectionWriteTimeout = 10 * time.Second
	cfg.LogOutput = io.Discard

	session, err := yamux.Client(wsConn, cfg)
	if err != nil {
		return err
	}
	defer session.Close()

	t.logger.Info("yamux session ready, accepting streams")

	// Accept incoming streams from the gateway and serve HTTP requests.
	for {
		stream, err := session.AcceptStream()
		if err != nil {
			if t.ctx.Err() != nil {
				return nil
			}
			return err
		}

		go t.serveStream(stream)
	}
}

// serveStream handles a single yamux stream by serving it as an HTTP request.
func (t *cloudTunnel) serveStream(stream net.Conn) {
	defer stream.Close()

	// Create a single-connection HTTP server that serves exactly one request
	// through the local MCP handler, then closes.
	srv := &http.Server{
		Handler:     t.handler,
		ReadTimeout: 30 * time.Second,
	}
	// ServeConn is not available on http.Server, so use the lower-level approach:
	// read the HTTP request, pass it to the handler, write the response.
	srv.Serve(newSingleConnListener(stream))
}

// wsURL converts the HTTPS endpoint to a WSS URL for the tunnel.
func (t *cloudTunnel) wsURL() string {
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

// ---------- WebSocket net.Conn adapter ----------

// wsConn wraps a gorilla/websocket.Conn to implement net.Conn for yamux.
type wsConn struct {
	ws     *websocket.Conn
	reader io.Reader
}

func newWSConn(ws *websocket.Conn) net.Conn {
	return &wsConn{ws: ws}
}

func (c *wsConn) Read(p []byte) (int, error) {
	for {
		if c.reader != nil {
			n, err := c.reader.Read(p)
			if err == io.EOF {
				c.reader = nil
				continue
			}
			return n, err
		}
		_, reader, err := c.ws.NextReader()
		if err != nil {
			return 0, err
		}
		c.reader = reader
	}
}

func (c *wsConn) Write(p []byte) (int, error) {
	err := c.ws.WriteMessage(websocket.BinaryMessage, p)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *wsConn) Close() error                       { return c.ws.Close() }
func (c *wsConn) LocalAddr() net.Addr                { return c.ws.LocalAddr() }
func (c *wsConn) RemoteAddr() net.Addr               { return c.ws.RemoteAddr() }
func (c *wsConn) SetDeadline(t time.Time) error      { return c.ws.SetReadDeadline(t) }
func (c *wsConn) SetReadDeadline(t time.Time) error  { return c.ws.SetReadDeadline(t) }
func (c *wsConn) SetWriteDeadline(t time.Time) error { return c.ws.SetWriteDeadline(t) }

// ---------- single-connection listener ----------

// singleConnListener wraps a net.Conn as a net.Listener that returns it once.
type singleConnListener struct {
	conn net.Conn
	once sync.Once
	ch   chan net.Conn
}

func newSingleConnListener(conn net.Conn) net.Listener {
	l := &singleConnListener{conn: conn, ch: make(chan net.Conn, 1)}
	l.ch <- conn
	return l
}

func (l *singleConnListener) Accept() (net.Conn, error) {
	conn, ok := <-l.ch
	if !ok {
		return nil, io.EOF
	}
	return conn, nil
}

func (l *singleConnListener) Close() error {
	l.once.Do(func() { close(l.ch) })
	return nil
}

func (l *singleConnListener) Addr() net.Addr { return l.conn.LocalAddr() }

