package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// ShutdownTimeout is the maximum time to wait for in-flight requests to finish.
const ShutdownTimeout = 30 * time.Second

// shutdownOnSignal blocks until SIGINT or SIGTERM is received, then gracefully
// shuts down the given HTTP servers. A second signal forces immediate exit.
//
// cleanupFn is called after the HTTP servers are stopped to release resources
// such as database connections and gRPC connections. It may be nil.
func shutdownOnSignal(servers []*http.Server, cleanupFn func()) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigCh
	slog.Info("Shutting down gracefully (30s timeout)...", "signal", sig.String())

	// A second signal forces immediate exit.
	go func() {
		sig := <-sigCh
		slog.Warn("Forced shutdown", "signal", sig.String())
		os.Exit(1)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()

	// Shut down all servers in parallel.
	var wg sync.WaitGroup
	for _, srv := range servers {
		wg.Add(1)
		go func(s *http.Server) {
			defer wg.Done()
			if err := s.Shutdown(ctx); err != nil {
				slog.Error("HTTP server shutdown error", "addr", s.Addr, "error", err)
			}
		}(srv)
	}
	wg.Wait()

	// Run resource cleanup after servers are drained.
	if cleanupFn != nil {
		cleanupFn()
	}

	if ctx.Err() == context.DeadlineExceeded {
		slog.Warn("Forced shutdown after timeout")
		os.Exit(1)
	}

	slog.Info("Shutdown complete")
}
