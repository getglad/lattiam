package protocol

import (
	"errors"
	"fmt"
	"strings"
)

// Common errors used by both V5 and V6 clients
var (
	ErrUnsupportedProtocolVersion   = errors.New("unsupported protocol version")
	ErrProviderSchemaNotAvailable   = errors.New("provider schema not available")
	ErrPrepareConfigError           = errors.New("prepare config error")
	ErrConfigureError               = errors.New("configure error")
	ErrResourceSchemaNotFound       = errors.New("resource schema not found")
	ErrPlanError                    = errors.New("plan error")
	ErrApplyError                   = errors.New("apply error")
	ErrPlanReturnedNoState          = errors.New("plan returned no planned state")
	ErrApplyReturnedNoState         = errors.New("apply returned no new state")
	ErrRequiredAttributeNotProvided = errors.New("required attribute not provided")
	ErrFailedToConvertAttribute     = errors.New("failed to convert attribute")
	ErrFailedToProcessBlock         = errors.New("failed to process block")
	ErrCannotConvertMapToNonMap     = errors.New("cannot convert map to non-map type")
	ErrUnableToParseProviderAddress = errors.New("unable to parse provider address")
)

// ErrRequiresReplace is returned when a resource update requires replacement
type ErrRequiresReplace struct {
	ResourceType string
	Attributes   []string
}

// Error implements the error interface
func (e *ErrRequiresReplace) Error() string {
	return fmt.Sprintf("resource %s requires replacement due to changes in: %v", e.ResourceType, e.Attributes)
}

// IsRetryableError determines if an error should trigger a retry
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}
	errMsg := err.Error()
	return strings.Contains(errMsg, "rpc error") ||
		strings.Contains(errMsg, "transport is closing") ||
		strings.Contains(errMsg, "connection reset")
}
