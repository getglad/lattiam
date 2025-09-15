package apiserver

import (
	"encoding/json"
	"net/http"

	"github.com/lattiam/lattiam/internal/logging"
)

// Package-level logger
var logger = logging.NewLogger("apiserver")

// ErrorResponse represents a structured error response
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// WriteJSON safely writes JSON response with proper error handling
func WriteJSON(w http.ResponseWriter, status int, data interface{}) {
	// Marshal the data first before writing status
	jsonData, err := json.Marshal(data)
	if err != nil {
		// If we can't marshal the response, send an error response instead
		WriteError(w, http.StatusInternalServerError, "encoding_error", "Failed to encode response")
		logger.Errorf("JSON encoding error: %v, data: %+v", err, data)
		return
	}

	// Set headers and write response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	// Write the pre-marshaled data
	if _, err := w.Write(jsonData); err != nil {
		// Log the error but we can't send another response since headers are already sent
		logger.Errorf("Failed to write response body: %v", err)
	}
}

// WriteError writes a structured error response
func WriteError(w http.ResponseWriter, status int, code string, message string) {
	response := ErrorResponse{
		Error:   code,
		Message: message,
	}

	// Try to write the error response
	jsonData, err := json.Marshal(response)
	if err != nil {
		// Fallback to plain text if we can't even marshal the error
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("Internal server error: failed to encode error response"))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(jsonData)
}
