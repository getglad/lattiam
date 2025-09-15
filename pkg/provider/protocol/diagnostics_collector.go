package protocol

import (
	"context"
	"fmt"
	"strings"
	"time"

	tfplugin6 "github.com/lattiam/lattiam/internal/proto/tfplugin6"
	"github.com/lattiam/lattiam/pkg/logging"
)

// ConnectionDiagnostics provides detailed diagnostics for provider connections
type ConnectionDiagnostics struct {
	ProviderName    string
	ProviderVersion string
	ProtocolVersion int
	StartTime       time.Time
	ConnectDuration time.Duration
	SchemaLoadTime  time.Duration
	Errors          []error
	Warnings        []string
}

// DiagnosticCollector collects diagnostic information during provider operations
type DiagnosticCollector struct {
	diagnostics map[string]*ConnectionDiagnostics
}

// NewDiagnosticCollector creates a new diagnostic collector
func NewDiagnosticCollector() *DiagnosticCollector {
	return &DiagnosticCollector{
		diagnostics: make(map[string]*ConnectionDiagnostics),
	}
}

// StartConnection begins tracking a provider connection
func (dc *DiagnosticCollector) StartConnection(providerName string, version string, protocol int) {
	dc.diagnostics[providerName] = &ConnectionDiagnostics{
		ProviderName:    providerName,
		ProviderVersion: version,
		ProtocolVersion: protocol,
		StartTime:       time.Now(),
		Errors:          []error{},
		Warnings:        []string{},
	}
}

// RecordConnectionComplete records successful connection
func (dc *DiagnosticCollector) RecordConnectionComplete(providerName string) {
	if diag, ok := dc.diagnostics[providerName]; ok {
		diag.ConnectDuration = time.Since(diag.StartTime)
	}
}

// RecordSchemaLoadTime records how long schema loading took
func (dc *DiagnosticCollector) RecordSchemaLoadTime(providerName string, duration time.Duration) {
	if diag, ok := dc.diagnostics[providerName]; ok {
		diag.SchemaLoadTime = duration
	}
}

// RecordError records an error during provider operations
func (dc *DiagnosticCollector) RecordError(providerName string, err error) {
	if diag, ok := dc.diagnostics[providerName]; ok {
		diag.Errors = append(diag.Errors, err)
	}
}

// RecordWarning records a warning during provider operations
func (dc *DiagnosticCollector) RecordWarning(providerName string, warning string) {
	if diag, ok := dc.diagnostics[providerName]; ok {
		diag.Warnings = append(diag.Warnings, warning)
	}
}

// GetDiagnostics returns diagnostics for a provider
func (dc *DiagnosticCollector) GetDiagnostics(providerName string) *ConnectionDiagnostics {
	return dc.diagnostics[providerName]
}

// GetSummary returns a summary of all diagnostics
func (dc *DiagnosticCollector) GetSummary() string {
	var summary strings.Builder

	for name, diag := range dc.diagnostics {
		summary.WriteString(fmt.Sprintf("\nProvider: %s v%s (protocol v%d)\n",
			name, diag.ProviderVersion, diag.ProtocolVersion))
		summary.WriteString(fmt.Sprintf("  Connection time: %v\n", diag.ConnectDuration))
		summary.WriteString(fmt.Sprintf("  Schema load time: %v\n", diag.SchemaLoadTime))

		if len(diag.Errors) > 0 {
			summary.WriteString(fmt.Sprintf("  Errors: %d\n", len(diag.Errors)))
			for _, err := range diag.Errors {
				summary.WriteString(fmt.Sprintf("    - %v\n", err))
			}
		}

		if len(diag.Warnings) > 0 {
			summary.WriteString(fmt.Sprintf("  Warnings: %d\n", len(diag.Warnings)))
			for _, warn := range diag.Warnings {
				summary.WriteString(fmt.Sprintf("    - %s\n", warn))
			}
		}
	}

	return summary.String()
}

// ContextualDiagnosticHandler extends the basic DiagnosticHandler with more context
type ContextualDiagnosticHandler struct {
	*DiagnosticHandler
	collector    *DiagnosticCollector
	providerName string
}

// NewContextualDiagnosticHandler creates a diagnostic handler with additional context
func NewContextualDiagnosticHandler(
	ctx context.Context,
	providerName string,
	operation string,
	collector *DiagnosticCollector,
) *ContextualDiagnosticHandler {
	return &ContextualDiagnosticHandler{
		DiagnosticHandler: NewDiagnosticHandler(ctx, providerName, operation),
		collector:         collector,
		providerName:      providerName,
	}
}

// RecordTiming records operation timing
func (edh *ContextualDiagnosticHandler) RecordTiming(operation string, duration time.Duration) {
	logging.Protocol.Operation(edh.ctx, "OperationTiming", map[string]interface{}{
		"provider":  edh.providerName,
		"operation": operation,
		"duration":  duration.String(),
	})

	// Record slow operations as warnings
	if duration > 5*time.Second {
		warning := fmt.Sprintf("Slow operation: %s took %v", operation, duration)
		edh.collector.RecordWarning(edh.providerName, warning)
	}
}

// WrapOperation wraps an operation with timing and error recording
func (edh *ContextualDiagnosticHandler) WrapOperation(
	operation string,
	fn func() error,
) error {
	start := time.Now()
	err := fn()
	duration := time.Since(start)

	edh.RecordTiming(operation, duration)

	if err != nil {
		edh.collector.RecordError(edh.providerName,
			fmt.Errorf("%s: %w", operation, err))
	}

	return err
}

// Diagnostic processing functions

// ProcessDiagnostics processes diagnostics and returns error if any errors found
func ProcessDiagnostics(ctx context.Context, diags []*tfplugin6.Diagnostic, operation, provider string) error {
	if len(diags) == 0 {
		return nil
	}

	var errors []string
	var warnings []string

	for _, diag := range diags {
		message := fmt.Sprintf("%s: %s", diag.GetSummary(), diag.GetDetail())

		switch diag.GetSeverity() {
		case tfplugin6.Diagnostic_ERROR:
			errors = append(errors, message)
			logging.Diagnostic.Error("operation=%s provider=%s error=%s", operation, provider, message)
		case tfplugin6.Diagnostic_WARNING:
			warnings = append(warnings, message)
			logging.Diagnostic.Warn("operation=%s provider=%s warning=%s", operation, provider, message)
		case tfplugin6.Diagnostic_INVALID:
			// Invalid severity - treat as error for safety
			errors = append(errors, message)
			logging.Diagnostic.Error("operation=%s provider=%s invalid_severity=%s", operation, provider, message)
		}
	}

	// Log warnings even if there are errors
	if len(warnings) > 0 {
		logging.Protocol.Operation(ctx, operation+"_warnings", map[string]interface{}{
			"provider": provider,
			"warnings": len(warnings),
		})
	}

	// Return error if any errors found
	if len(errors) > 0 {
		return fmt.Errorf("provider %s operation %s failed: %s", provider, operation, strings.Join(errors, "; "))
	}

	return nil
}

// CheckDiagnosticsV6ForErrors checks v6 diagnostics and returns just the warning string (for logging)
func CheckDiagnosticsV6ForErrors(diags []*tfplugin6.Diagnostic) (hasErrors bool, warningMessage string) {
	if len(diags) == 0 {
		return false, ""
	}

	var errors []string
	var warnings []string

	for _, diag := range diags {
		message := fmt.Sprintf("%s: %s", diag.GetSummary(), diag.GetDetail())

		switch diag.GetSeverity() {
		case tfplugin6.Diagnostic_ERROR:
			errors = append(errors, message)
		case tfplugin6.Diagnostic_WARNING:
			warnings = append(warnings, message)
		case tfplugin6.Diagnostic_INVALID:
			// Invalid severity - treat as error for safety
			errors = append(errors, message)
		}
	}

	hasErrors = len(errors) > 0

	if len(warnings) > 0 {
		warningMessage = strings.Join(warnings, "; ")
	} else if len(errors) > 0 {
		warningMessage = strings.Join(errors, "; ")
	}

	return hasErrors, warningMessage
}
