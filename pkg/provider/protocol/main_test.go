package protocol

import (
	"fmt"
	"os"
	"testing"
)

// TestMain runs before all tests and performs cleanup
func TestMain(m *testing.M) {
	// Clean up any zombie provider processes before running tests
	if err := CleanupZombieProviders(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to cleanup zombie providers: %v\n", err)
	}

	// Setup shared provider cache and pre-download providers
	SetupTestProviders()

	// Run tests
	code := m.Run()

	// Clean up any remaining provider processes after tests
	if err := CleanupZombieProviders(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to cleanup providers after tests: %v\n", err)
	}

	os.Exit(code)
}
