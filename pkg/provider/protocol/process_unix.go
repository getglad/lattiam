//go:build !windows
// +build !windows

package protocol

import (
	"os/exec"
	"syscall"
)

// setupProcessGroup configures proper process group management for provider processes
// This ensures providers are properly cleaned up and don't become zombies
func setupProcessGroup(cmd *exec.Cmd) {
	// Create new process group to prevent provider processes from becoming orphaned
	// when the parent terminates. This ensures clean shutdown of provider trees
	// and prevents zombie processes in containerized environments.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true, // Create new process group
		Pgid:    0,    // Use process PID as group ID
	}
}
