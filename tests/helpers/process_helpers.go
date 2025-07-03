package helpers

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// AssertNoTerraformProviderProcesses asserts that no terraform-provider processes
// that are children of the current process are running.
func AssertNoTerraformProviderProcesses(t *testing.T) {
	t.Helper()

	// Get the parent process ID (the test runner)
	pid := os.Getpid()

	// Use pgrep to find terraform-provider processes that are children of the test process
	// This is more reliable than ps aux | grep
	// #nosec G204 - pid is from os.Getpid() which is safe, not user input
	cmd := exec.Command("pgrep", "-P", fmt.Sprintf("%d", pid), "terraform-provider")
	output, err := cmd.Output()
	// pgrep exits with 1 if no processes are found, which is what we want.
	// So, we only check for errors other than ExitError(1).
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// Exit code 1 means no processes were found, which is success for us.
			if exitErr.ExitCode() == 1 {
				return
			}
		}
		// Any other error is a failure.
		require.NoError(t, err, "Failed to run pgrep command")
	}

	pids := strings.TrimSpace(string(output))
	require.Empty(t, pids, "Found running terraform-provider child processes: %s", pids)
}
