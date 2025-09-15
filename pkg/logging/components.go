// Package logging provides structured logging support for the Lattiam application
package logging

// Component-specific loggers for easy incremental adoption

// Protocol logger for provider protocol operations
var Protocol = NewLogger("protocol")

// Manager logger for provider management operations
var Manager = NewLogger("manager")

// State logger for state management operations
var State = NewLogger("state")

// Schema logger for schema operations
var Schema = NewLogger("schema")

// Client logger for client operations
var Client = NewLogger("client")

// Download logger for provider download operations
var Download = NewLogger("download")

// Cache logger for caching operations
var Cache = NewLogger("cache")

// Diagnostic logger for diagnostic operations
var Diagnostic = NewLogger("diagnostic")

// Retry logger for retry operations
var Retry = NewLogger("retry")

// Config logger for configuration operations
var Config = NewLogger("config")

// ProtocolConnection logs protocol connection information
func ProtocolConnection(providerName, address string, version int, resourceType string) {
	Protocol.Info("Connecting to provider=%s address=%s protocol_version=%d resource_type=%s",
		providerName, address, version, resourceType)
}

// ProtocolOperation logs protocol operation information
func ProtocolOperation(operation, provider string, details ...interface{}) {
	if len(details) > 0 {
		Protocol.Debug("operation=%s provider=%s %v", operation, provider, details[0])
	} else {
		Protocol.Debug("operation=%s provider=%s", operation, provider)
	}
}

// ProtocolSuccess logs successful protocol operations
func ProtocolSuccess(operation, provider string, details ...interface{}) {
	if len(details) > 0 {
		Protocol.Info("operation=%s provider=%s status=success %v", operation, provider, details[0])
	} else {
		Protocol.Info("operation=%s provider=%s status=success", operation, provider)
	}
}

// ProtocolError logs protocol errors
func ProtocolError(operation, provider string, err interface{}) {
	Protocol.Error("operation=%s provider=%s error=%v", operation, provider, err)
}

// ProtocolWarning logs protocol warnings
func ProtocolWarning(operation, provider string, warning string) {
	Protocol.Warn("operation=%s provider=%s warning=%s", operation, provider, warning)
}

// SchemaOperation logs schema operations
func SchemaOperation(operation, provider, resourceType string) {
	Schema.Debug("schema_operation=%s provider=%s resource_type=%s", operation, provider, resourceType)
}

// TypeConversion logs type conversion operations
func TypeConversion(fromType, toType string, success bool) {
	if success {
		Protocol.Trace("type_conversion from=%s to=%s status=success", fromType, toType)
	} else {
		Protocol.Warn("type_conversion from=%s to=%s status=failed", fromType, toType)
	}
}
