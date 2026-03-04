package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// pidFilePath returns the path to the PID file (~/.skyline/skyline.pid).
func pidFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home directory: %w", err)
	}
	return filepath.Join(home, ".skyline", "skyline.pid"), nil
}

// writePID writes the current process ID to the PID file.
func writePID() error {
	path, err := pidFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create pid directory: %w", err)
	}
	return os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0600)
}

// removePID removes the PID file.
func removePID() {
	path, err := pidFilePath()
	if err != nil {
		return
	}
	os.Remove(path)
}

// readPID reads the PID from the PID file and returns the pid and whether
// the process is actually running.
func readPID() (int, bool, error) {
	path, err := pidFilePath()
	if err != nil {
		return 0, false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, false, nil
		}
		return 0, false, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, false, fmt.Errorf("invalid pid file: %w", err)
	}

	// Check if process is alive by sending signal 0
	proc, err := os.FindProcess(pid)
	if err != nil {
		return pid, false, nil
	}
	err = proc.Signal(syscall.Signal(0))
	if err != nil {
		return pid, false, nil
	}
	return pid, true, nil
}

// runGateway dispatches the gateway subcommand: start, stop, restart, status.
func runGateway(logger *slog.Logger, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: skyline gateway <start|stop|restart|status>")
	}

	switch args[0] {
	case "start":
		return gatewayStart(logger)
	case "stop":
		return gatewayStop(logger)
	case "restart":
		return gatewayRestart(logger)
	case "status":
		return gatewayStatus(logger)
	default:
		return fmt.Errorf("unknown gateway command %q — expected start, stop, restart, or status", args[0])
	}
}

func gatewayStart(logger *slog.Logger) error {
	// Check if already running
	pid, alive, _ := readPID()
	if alive {
		logger.Info("skyline is already running", "pid", pid)
		return nil
	}

	// Find our own executable
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	// Re-exec ourselves with --daemon flag. The --daemon flag causes the
	// server to write a PID file and detach from the terminal.
	cmd := exec.Command(exePath, "--daemon")
	cmd.Dir = filepath.Dir(exePath)

	// Detach from controlling terminal — the child runs independently.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	// Redirect stdout/stderr to log file
	home, _ := os.UserHomeDir()
	logPath := filepath.Join(home, ".skyline", "skyline.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	// Inherit environment (includes SKYLINE_PROFILES_KEY etc.)
	cmd.Env = os.Environ()

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("start skyline: %w", err)
	}

	// Don't wait — let the child run detached
	go func() {
		_ = cmd.Wait()
		logFile.Close()
	}()

	// Wait briefly and confirm it's actually alive
	time.Sleep(2 * time.Second)
	newPid, alive, _ := readPID()
	if !alive {
		return fmt.Errorf("skyline failed to start — check %s for details", logPath)
	}

	logger.Info("skyline started", "pid", newPid, "log", logPath)
	return nil
}

func gatewayStop(logger *slog.Logger) error {
	pid, alive, err := readPID()
	if err != nil {
		return fmt.Errorf("read pid: %w", err)
	}
	if !alive {
		if pid > 0 {
			// Stale PID file
			removePID()
		}
		logger.Info("skyline is not running")
		return nil
	}

	// Send SIGTERM for graceful shutdown
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process: %w", err)
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("send SIGTERM: %w", err)
	}

	// Wait up to 30 seconds for the process to exit
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			// Process is gone
			removePID()
			logger.Info("skyline stopped", "pid", pid)
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Force kill if still alive
	_ = proc.Signal(syscall.SIGKILL)
	removePID()
	logger.Warn("skyline force-killed after timeout", "pid", pid)
	return nil
}

func gatewayRestart(logger *slog.Logger) error {
	pid, alive, _ := readPID()
	if alive {
		logger.Info("stopping skyline...", "pid", pid)
		if err := gatewayStop(logger); err != nil {
			return fmt.Errorf("stop: %w", err)
		}
	}
	return gatewayStart(logger)
}

func gatewayStatus(logger *slog.Logger) error {
	pid, alive, err := readPID()
	if err != nil {
		return fmt.Errorf("read pid: %w", err)
	}
	if alive {
		logger.Info("skyline is running", "pid", pid)
	} else {
		if pid > 0 {
			logger.Info("skyline is not running (stale pid file)", "pid", pid)
			removePID()
		} else {
			logger.Info("skyline is not running")
		}
	}
	return nil
}
