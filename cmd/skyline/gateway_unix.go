//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// setSysProcAttr detaches the child process from the controlling terminal.
func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
}

// signalZero is the null signal used to check if a process is alive.
var signalZero = syscall.Signal(0)

// sigTerm is the graceful shutdown signal.
var sigTerm = syscall.SIGTERM

// sigKill is the forced termination signal.
var sigKill = syscall.SIGKILL
