package main

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
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
	err = proc.Signal(signalZero)
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

func gatewayStart(_ *slog.Logger) error {
	// Check if already running
	pid, alive, _ := readPID()
	if alive {
		fmt.Printf("skyline is already running (pid %d)\n", pid)
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
	setSysProcAttr(cmd)

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

	fmt.Printf("skyline started (pid %d)\n", newPid)
	return nil
}

func gatewayStop(_ *slog.Logger) error {
	pid, alive, err := readPID()
	if err != nil {
		return fmt.Errorf("read pid: %w", err)
	}
	if !alive {
		if pid > 0 {
			// Stale PID file
			removePID()
		}
		fmt.Println("skyline is not running")
		return nil
	}

	// Send SIGTERM for graceful shutdown
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process: %w", err)
	}
	if err := proc.Signal(sigTerm); err != nil {
		return fmt.Errorf("send SIGTERM: %w", err)
	}

	// Wait up to 30 seconds for the process to exit
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if err := proc.Signal(signalZero); err != nil {
			// Process is gone
			removePID()
			fmt.Printf("skyline stopped (pid %d)\n", pid)
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Force kill if still alive
	_ = proc.Signal(sigKill)
	removePID()
	fmt.Printf("skyline force-killed after timeout (pid %d)\n", pid)
	return nil
}

func gatewayRestart(logger *slog.Logger) error {
	_, alive, _ := readPID()
	if alive {
		if err := gatewayStop(logger); err != nil {
			return fmt.Errorf("stop: %w", err)
		}
	}
	return gatewayStart(logger)
}

func gatewayStatus(_ *slog.Logger) error {
	pid, alive, err := readPID()
	if err != nil {
		return fmt.Errorf("read pid: %w", err)
	}

	if !alive {
		if pid > 0 {
			removePID()
		}
		fmt.Println("● skyline is not running")
		return nil
	}

	// Gather process info
	version := getProcessVersion()
	listen := getListenAddr(pid)
	uptime := getProcessUptime(pid)
	memRSS := getProcessMemRSS(pid)
	logPath := getLogPath()

	// Print systemctl-style status
	if version != "" {
		fmt.Printf("● skyline - Skyline MCP Server %s\n", version)
	} else {
		fmt.Println("● skyline - Skyline MCP Server")
	}
	fmt.Printf("     Active: active (running) since %s; %s\n", formatStartTime(pid), uptime)
	fmt.Printf("    Process: %d\n", pid)
	if listen != "" {
		fmt.Printf("     Listen: %s\n", listen)
	}
	if memRSS != "" {
		fmt.Printf("     Memory: %s\n", memRSS)
	}
	if logPath != "" {
		fmt.Printf("        Log: %s\n", logPath)
	}

	return nil
}

// getProcessVersion returns the version string from the skyline binary.
func getProcessVersion() string {
	exePath, err := os.Executable()
	if err != nil {
		return ""
	}
	exePath, _ = filepath.EvalSymlinks(exePath)
	out, err := exec.Command(exePath, "--version").Output()
	if err != nil {
		return ""
	}
	// Output is "Skyline MCP Server v0.9.19\nPlatform: ..."
	line := strings.SplitN(string(out), "\n", 2)[0]
	return strings.TrimPrefix(line, "Skyline MCP Server ")
}

// getListenAddr reads the listen address from the config file.
func getListenAddr(pid int) string {
	// Try reading from config file
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	configPath := filepath.Join(home, ".skyline", "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return ""
	}

	var cfg struct {
		Server struct {
			Listen string `yaml:"listen"`
		} `yaml:"server"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return ""
	}

	listen := cfg.Server.Listen
	if listen == "" {
		listen = "localhost:8191"
	}

	// Also verify the port is actually in use by this pid
	actualAddr := getListenFromSS(pid)
	if actualAddr != "" {
		return "https://" + actualAddr
	}

	return "https://" + listen
}

// getListenFromSS reads the actual listen address from /proc/net/tcp6 or ss.
func getListenFromSS(pid int) string {
	// Read the fd symlinks to find the socket, then check /proc/net
	// Simpler: parse ss output
	out, err := exec.Command("ss", "-tlnp").Output()
	if err != nil {
		return ""
	}
	pidStr := fmt.Sprintf("pid=%d", pid)
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, pidStr) {
			// Format: LISTEN  0  4096  *:8191  *:*  users:(("skyline",pid=61909,fd=6))
			fields := strings.Fields(line)
			if len(fields) >= 4 {
				addr := fields[3]
				// Convert *:8191 to 0.0.0.0:8191
				addr = strings.Replace(addr, "*:", "0.0.0.0:", 1)
				return addr
			}
		}
	}
	return ""
}

// getProcessUptime returns a human-readable uptime string.
func getProcessUptime(pid int) string {
	procDir := fmt.Sprintf("/proc/%d", pid)
	info, err := os.Stat(procDir)
	if err != nil {
		return "unknown"
	}
	uptime := time.Since(info.ModTime())
	return formatDuration(uptime)
}

// formatStartTime returns the process start time as a formatted string.
func formatStartTime(pid int) string {
	procDir := fmt.Sprintf("/proc/%d", pid)
	info, err := os.Stat(procDir)
	if err != nil {
		return "unknown"
	}
	return info.ModTime().Format("Mon 2006-01-02 15:04:05 MST")
}

// getProcessMemRSS returns the RSS memory usage from /proc/[pid]/status.
func getProcessMemRSS(pid int) string {
	statusPath := fmt.Sprintf("/proc/%d/status", pid)
	data, err := os.ReadFile(statusPath)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				kb, _ := strconv.ParseInt(parts[1], 10, 64)
				if kb >= 1024 {
					return fmt.Sprintf("%.1fM", float64(kb)/1024)
				}
				return parts[1] + " kB"
			}
		}
	}
	return ""
}

// getLogPath returns the log file path.
func getLogPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	logPath := filepath.Join(home, ".skyline", "skyline.log")
	if _, err := os.Stat(logPath); err == nil {
		return logPath
	}
	return ""
}

// formatDuration returns a human-readable duration like "2h 15min ago" or "3 days ago".
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dmin %ds ago", int(d.Minutes()), int(d.Seconds())%60)
	}
	if d < 24*time.Hour {
		hours := int(d.Hours())
		mins := int(d.Minutes()) % 60
		return fmt.Sprintf("%dh %dmin ago", hours, mins)
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	return fmt.Sprintf("%d days %dh ago", days, hours)
}
