//go:build !windows
// +build !windows

package main

import (
	"os/exec"
	"syscall"
)

// setupServerProcess configures process attributes for the server on Unix systems
func setupServerProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}
