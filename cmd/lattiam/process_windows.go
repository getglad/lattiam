//go:build windows
// +build windows

package main

import (
	"os/exec"
)

// setupServerProcess configures process attributes for the server on Windows systems
func setupServerProcess(cmd *exec.Cmd) {
	// Windows handles process groups differently
	// The default behavior is sufficient for our needs
}
