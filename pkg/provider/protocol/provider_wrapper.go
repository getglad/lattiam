package protocol

import (
	"github.com/lattiam/lattiam/internal/interfaces"
)

// Name returns the provider name
func (p *providerWrapper) Name() string {
	return p.provInstance.Name()
}

// Version returns the provider version
func (p *providerWrapper) Version() string {
	return p.provInstance.Version()
}

// GetLastDiagnostics retrieves diagnostics from the last operation
func (p *providerWrapper) GetLastDiagnostics() []interfaces.Diagnostic {
	p.diagnosticsMu.RLock()
	defer p.diagnosticsMu.RUnlock()

	// Return a copy to prevent external modification
	result := make([]interfaces.Diagnostic, len(p.lastDiagnostics))
	copy(result, p.lastDiagnostics)
	return result
}

// GetCapabilities returns provider capabilities
func (p *providerWrapper) GetCapabilities() interfaces.ProviderCapabilities {
	return interfaces.ProviderCapabilities{
		ProtocolVersion:   6, // Always using v6 interface (may be adapter for v5)
		SupportsRetry:     true,
		SupportsDeferred:  true,
		SupportsFunctions: true,
		MaxRetryAttempts:  3,       // Default retry attempts
		CurrentRetryDelay: "100ms", // Default retry delay
	}
}
