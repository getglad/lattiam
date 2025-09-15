package config

import "os"

// IsLocalStack returns true if running in LocalStack mode
func IsLocalStack() bool {
	return os.Getenv("LATTIAM_TEST_MODE") == "localstack"
}
