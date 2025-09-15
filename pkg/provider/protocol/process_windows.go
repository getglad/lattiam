//go:build windows
// +build windows

package protocol

import (
	"os/exec"
)

// setupProcessGroup configures proper process group management for provider processes
// On Windows, process groups are handled differently, so this is a no-op
func setupProcessGroup(cmd *exec.Cmd) {
	// Windows handles process groups differently
	// The default behavior is sufficient for our needs
}
