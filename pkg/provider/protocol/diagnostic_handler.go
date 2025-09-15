package protocol

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/lattiam/lattiam/internal/proto/tfplugin6"
	"github.com/lattiam/lattiam/pkg/logging"
)

// DiagnosticHandler provides diagnostic handling for v6 protocol
type DiagnosticHandler struct {
	ctx          context.Context
	providerName string
	operation    string
	logger       *logging.Logger
}

// NewDiagnosticHandler creates a new diagnostic handler with context
func NewDiagnosticHandler(ctx context.Context, providerName, operation string) *DiagnosticHandler {
	return &DiagnosticHandler{
		ctx:          ctx,
		providerName: providerName,
		operation:    operation,
		logger:       logging.NewLogger("diagnostic-handler"),
	}
}

// HandleDiagnosticsV6 processes v6 diagnostics with structured logging and error handling
func (h *DiagnosticHandler) HandleDiagnosticsV6(diags []*tfplugin6.Diagnostic) error {
	if len(diags) == 0 {
		return nil
	}

	// Convert to tfprotov6 diagnostics for better handling
	tfDiags := make([]*tfprotov6.Diagnostic, len(diags))
	for i, diag := range diags {
		tfDiags[i] = &tfprotov6.Diagnostic{
			Severity: tfprotov6.DiagnosticSeverity(diag.GetSeverity()),
			Summary:  diag.GetSummary(),
			Detail:   diag.GetDetail(),
		}
	}

	return h.processDiagnosticsV6(tfDiags)
}

// processDiagnosticsV6 handles the actual diagnostic processing for v6
func (h *DiagnosticHandler) processDiagnosticsV6(diags []*tfprotov6.Diagnostic) error {
	var errors []string
	var warnings []string

	for _, diag := range diags {
		switch diag.Severity {
		case tfprotov6.DiagnosticSeverityError:
			message := diag.Summary
			if diag.Detail != "" {
				message += " - " + diag.Detail
			}
			errors = append(errors, message)
		case tfprotov6.DiagnosticSeverityWarning:
			message := diag.Summary
			if diag.Detail != "" {
				message += " - " + diag.Detail
			}
			warnings = append(warnings, message)
		case tfprotov6.DiagnosticSeverityInvalid:
			// Invalid severity - treat as error for safety
			message := diag.Summary
			if diag.Detail != "" {
				message += " - " + diag.Detail
			}
			errors = append(errors, message)
		}
	}

	// Log warnings if any
	if len(warnings) > 0 {
		h.logger.Warn("Provider %s %s warnings: %s", h.providerName, h.operation, strings.Join(warnings, "; "))
	}

	// Return error if we have any errors
	if len(errors) > 0 {
		return fmt.Errorf("provider %s %s failed with %d errors: %s",
			h.providerName, h.operation, len(errors), strings.Join(errors, "; "))
	}

	return nil
}
