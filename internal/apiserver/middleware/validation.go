// Package middleware provides HTTP middleware for request validation and processing.
package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"

	chi "github.com/go-chi/chi/v5"
)

const (
	// MaxRequestBodySize is the maximum allowed request body size (10MB)
	MaxRequestBodySize = 10 * 1024 * 1024
)

// ValidationError represents a validation error response
type ValidationError struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

// IDValidator creates a middleware that validates deployment IDs in URL parameters
func IDValidator(paramName string) func(http.Handler) http.Handler {
	// Valid ID pattern: alphanumeric and hyphens, 1-100 characters
	validIDPattern := regexp.MustCompile(`^[a-zA-Z0-9-]{1,100}$`)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := chi.URLParam(r, paramName)

			// Check if ID is empty
			if id == "" {
				writeValidationError(w, fmt.Sprintf("%s is required", paramName), paramName)
				return
			}

			// Check if ID matches pattern
			if !validIDPattern.MatchString(id) {
				writeValidationError(w, fmt.Sprintf("%s contains invalid characters or is too long", paramName), paramName)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// NameValidator creates a middleware that validates deployment names
func NameValidator() func(http.Handler) http.Handler {
	// Valid name pattern: alphanumeric, hyphens, underscores, dots
	// Must start and end with alphanumeric, 2-100 characters
	validNamePattern := regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,98}[a-zA-Z0-9]$`)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip validation for non-modifying requests
			if !isModifyingRequest(r) {
				next.ServeHTTP(w, r)
				return
			}

			// Parse and validate request body
			body, err := parseAndRestoreBody(r)
			if err != nil {
				writeValidationError(w, err.Error(), "body")
				return
			}

			// Validate name field if present
			if err := validateNameField(body, validNamePattern); err != nil {
				writeValidationError(w, err.Error(), "name")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// isModifyingRequest checks if the request method modifies data
func isModifyingRequest(r *http.Request) bool {
	return r.Method == http.MethodPost || r.Method == http.MethodPut
}

// parseAndRestoreBody reads, parses, and restores the request body with size limit
func parseAndRestoreBody(r *http.Request) (map[string]interface{}, error) {
	// Limit request body to prevent DoS attacks
	limitedReader := io.LimitReader(r.Body, MaxRequestBodySize)
	bodyBytes, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read request body")
	}

	// Check if we hit the limit by trying to read one more byte
	if n, _ := io.Copy(io.Discard, r.Body); n > 0 {
		return nil, fmt.Errorf("request body too large (max %d bytes)", MaxRequestBodySize)
	}

	_ = r.Body.Close()

	// Parse request body
	var body map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		return nil, fmt.Errorf("invalid JSON in request body")
	}

	// Restore the body for the next handler
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	return body, nil
}

// validateNameField checks if the name field is valid
func validateNameField(body map[string]interface{}, pattern *regexp.Regexp) error {
	name, ok := body["name"].(string)
	if !ok {
		return nil // Field not present, skip validation
	}

	if name == "" {
		return fmt.Errorf("name cannot be empty")
	}

	if len(name) < 2 {
		return fmt.Errorf("name must be at least 2 characters long")
	}

	if !pattern.MatchString(name) {
		return fmt.Errorf("name contains invalid characters or format")
	}

	return nil
}

// TerraformJSONValidator creates a middleware that validates Terraform JSON structure
func TerraformJSONValidator() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip validation for non-modifying requests
			if !isModifyingRequest(r) {
				next.ServeHTTP(w, r)
				return
			}

			// Parse and validate request body
			body, err := parseAndRestoreBody(r)
			if err != nil {
				writeValidationError(w, err.Error(), "body")
				return
			}

			// Validate terraform_json field if present
			if err := validateTerraformJSON(body); err != nil {
				writeValidationError(w, err.Error(), "terraform_json")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// validateTerraformJSON checks if the terraform_json field is valid
func validateTerraformJSON(body map[string]interface{}) error {
	tfJSON, ok := body["terraform_json"].(map[string]interface{})
	if !ok {
		return nil // Field not present, skip validation
	}

	// Validate that it has at least resource or data blocks
	_, hasResource := tfJSON["resource"]
	_, hasData := tfJSON["data"]

	if !hasResource && !hasData {
		return fmt.Errorf("terraform JSON must contain at least one resource or data block")
	}

	return nil
}

// ContentTypeValidator ensures requests have proper content type
func ContentTypeValidator() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only validate on requests with body
			if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
				// Check if request has a body (Content-Length > 0 or Transfer-Encoding is set)
				if r.ContentLength > 0 || r.Header.Get("Transfer-Encoding") != "" {
					contentType := r.Header.Get("Content-Type")
					if contentType != "application/json" {
						writeValidationError(w, "Content-Type must be application/json", "header")
						return
					}
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// writeValidationError writes a validation error response
func writeValidationError(w http.ResponseWriter, message, field string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)

	response := ValidationError{
		Error:   "validation_error",
		Message: message,
		Field:   field,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		// Log error but don't try to write again since we already set the response
		// This is best effort - the client will still get the status code
		_ = err
	}
}
