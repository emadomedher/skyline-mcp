//go:build windows

package main

import (
	"os"
	"os/exec"
)

// setSysProcAttr is a no-op on Windows (Setsid is not available).
func setSysProcAttr(_ *exec.Cmd) {}

// signalZero is the null signal used to check if a process is alive.
// On Windows, os.FindProcess + Signal(os.Kill) is the only way, but
// we use os.Kill as a probe since Windows doesn't support signal 0.
var signalZero os.Signal = os.Kill

// sigTerm is the graceful shutdown signal (os.Interrupt on Windows).
var sigTerm os.Signal = os.Interrupt

// sigKill is the forced termination signal.
var sigKill os.Signal = os.Kill
