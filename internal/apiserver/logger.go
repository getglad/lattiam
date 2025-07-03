package apiserver

import (
	"github.com/lattiam/lattiam/internal/logging"
)

// apiLogger wraps the shared logger for API use
type apiLogger struct {
	*logging.Logger
}

// newAPILogger creates a logger for API use
func newAPILogger() *apiLogger {
	return &apiLogger{
		Logger: logging.NewLogger("API"),
	}
}
