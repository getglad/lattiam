// Package deployment provides deployment service and error handling
package deployment

import (
	"errors"
	"fmt"

	"github.com/lattiam/lattiam/internal/interfaces"
)

// Error represents a structured deployment error with context
type Error struct {
	Code       string                      // Machine-readable error code
	Message    string                      // Human-readable message
	Status     interfaces.DeploymentStatus // Related deployment status
	HTTPStatus int                         // Suggested HTTP status code
}

// Error implements the error interface
func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Common deployment errors
var (
	// Update-related errors
	ErrDeploymentInProgress = &Error{
		Code:       "DEPLOYMENT_IN_PROGRESS",
		Message:    "cannot update a deployment that is currently in progress",
		Status:     interfaces.DeploymentStatusProcessing,
		HTTPStatus: 409, // Conflict
	}

	ErrDeploymentQueued = &Error{
		Code:       "DEPLOYMENT_QUEUED",
		Message:    "Cannot update a deployment that is still queued",
		Status:     interfaces.DeploymentStatusQueued,
		HTTPStatus: 409, // Conflict
	}

	ErrDeploymentCanceled = &Error{
		Code:       "DEPLOYMENT_CANCELED",
		Message:    "Cannot update a canceled deployment",
		Status:     interfaces.DeploymentStatusCanceled,
		HTTPStatus: 410, // Gone
	}

	ErrDeploymentDestroying = &Error{
		Code:       "DEPLOYMENT_DESTROYING",
		Message:    "Cannot update a deployment that is being destroyed",
		Status:     interfaces.DeploymentStatusDestroying,
		HTTPStatus: 409, // Conflict
	}

	ErrDeploymentDestroyed = &Error{
		Code:       "DEPLOYMENT_DESTROYED",
		Message:    "Cannot update a deployment that has been destroyed",
		Status:     interfaces.DeploymentStatusDestroyed,
		HTTPStatus: 410, // Gone
	}

	// General errors
	ErrDeploymentNotFound = &Error{
		Code:       "DEPLOYMENT_NOT_FOUND",
		Message:    "Deployment not found",
		HTTPStatus: 404, // Not Found
	}

	ErrInvalidRequest = &Error{
		Code:       "INVALID_REQUEST",
		Message:    "Invalid deployment request",
		HTTPStatus: 400, // Bad Request
	}
)

// IsDeploymentError checks if an error is a deployment.Error
func IsDeploymentError(err error) (*Error, bool) {
	var depErr *Error
	if errors.As(err, &depErr) {
		return depErr, true
	}
	return nil, false
}
