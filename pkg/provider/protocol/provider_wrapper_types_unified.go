package protocol

import (
	"sync"

	"github.com/lattiam/lattiam/internal/interfaces"
)

// providerWrapper implements provider.Provider interface using UnifiedProviderClient
// and manages provider instances with thread-safe operations and diagnostic tracking
type providerWrapper struct {
	provInstance ProviderInstanceInterface
	mu           sync.RWMutex

	// Advanced capabilities
	lastDiagnostics []interfaces.Diagnostic
	diagnosticsMu   sync.RWMutex

	// UnifiedProviderClient - the main client for all operations
	unifiedClient interface{} // Actually *UnifiedProviderClient but using interface{} for build isolation
}

// newProviderWrapper creates a new provider wrapper (unified version)
// Note: client parameter is ignored in unified mode
func newProviderWrapper(_ interface{}, instance ProviderInstanceInterface) *providerWrapper {
	return &providerWrapper{
		provInstance:    instance,
		lastDiagnostics: []interfaces.Diagnostic{},
	}
}
