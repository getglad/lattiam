package system

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lattiam/lattiam/internal/config"
	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/provider"
	"github.com/lattiam/lattiam/pkg/logging"
	"github.com/lattiam/lattiam/pkg/provider/protocol"
)

// ProviderManagerAdapter adapts provider.Manager to interfaces.ProviderManager
type ProviderManagerAdapter struct {
	wrapped     provider.Manager
	mu          sync.RWMutex
	metrics     interfaces.ProviderManagerMetrics
	initialized bool
	logger      *logging.Logger
}

// NewProviderManagerAdapter creates a new adapter wrapping the real provider manager
func NewProviderManagerAdapter(providerDir string) (*ProviderManagerAdapter, error) {
	// Use default config source to avoid auto-detection
	return NewProviderManagerAdapterWithOptions(providerDir, protocol.WithConfigSource(config.NewDefaultConfigSource()))
}

// NewProviderManagerAdapterWithOptions creates a new adapter with configurable options
func NewProviderManagerAdapterWithOptions(providerDir string, opts ...protocol.ProviderManagerOption) (*ProviderManagerAdapter, error) {
	// Prepare options with base directory
	allOpts := []protocol.ProviderManagerOption{
		protocol.WithBaseDir(providerDir),
	}
	allOpts = append(allOpts, opts...)

	// Create the real protocol.ProviderManager with options
	manager, err := protocol.NewProviderManagerWithOptions(allOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider manager: %w", err)
	}

	return &ProviderManagerAdapter{
		wrapped: manager,
		metrics: interfaces.ProviderManagerMetrics{
			ActiveProviders: 0,
			TotalProviders:  0,
			SchemasCached:   0,
			ProviderErrors:  0,
			AverageInitTime: 0,
		},
		logger: logging.NewLogger("provider-adapter"),
	}, nil
}

// GetProvider retrieves or creates a provider instance and returns ConcreteProvider
func (a *ProviderManagerAdapter) GetProvider(ctx context.Context, deploymentID, name string, version string, cfg interfaces.ProviderConfig, resourceType string) (interfaces.UnifiedProvider, error) {
	// Get provider from wrapped manager (which handles its own caching)
	start := time.Now()
	wrappedProvider, err := a.wrapped.GetProvider(ctx, deploymentID, name, version, cfg, resourceType)
	if err != nil {
		a.mu.Lock()
		a.metrics.ProviderErrors++
		a.mu.Unlock()
		return nil, fmt.Errorf("failed to get provider %s: %w", name, err)
	}

	// Create ConcreteProvider instead of adapter
	concreteProvider := NewConcreteProvider(wrappedProvider, name, version)

	// Update metrics
	a.mu.Lock()
	a.metrics.ActiveProviders++
	a.metrics.TotalProviders++

	// Update average init time
	initTime := time.Since(start)
	if a.metrics.AverageInitTime == 0 {
		a.metrics.AverageInitTime = initTime
	} else {
		a.metrics.AverageInitTime = (a.metrics.AverageInitTime + initTime) / 2
	}
	a.mu.Unlock()

	return concreteProvider, nil
}

// ReleaseProvider releases a provider instance
func (a *ProviderManagerAdapter) ReleaseProvider(_ string, _ string) error {
	// Delegate lifecycle management to the wrapped manager to avoid double-cleanup.
	// We only track metrics here since the underlying manager handles actual cleanup.
	a.mu.Lock()
	a.metrics.ActiveProviders--
	a.mu.Unlock()
	return nil
}

// ShutdownDeployment shuts down all providers for a specific deployment
func (a *ProviderManagerAdapter) ShutdownDeployment(deploymentID string) error {
	// Delegate to the wrapped manager's ShutdownDeployment method
	if pm, ok := a.wrapped.(*protocol.ProviderManager); ok {
		if err := pm.ShutdownDeployment(deploymentID); err != nil {
			return fmt.Errorf("failed to shutdown deployment %s: %w", deploymentID, err)
		}

		// Update metrics - we don't have exact counts per deployment, so do best effort
		a.mu.Lock()
		if a.metrics.ActiveProviders > 0 {
			a.metrics.ActiveProviders = 0 // Reset since we don't track per-deployment
		}
		a.mu.Unlock()

		return nil
	}

	// If wrapped manager doesn't support deployment shutdown, return error
	return fmt.Errorf("deployment shutdown not supported by underlying provider manager")
}

// ListActiveProviders returns information about active providers
func (a *ProviderManagerAdapter) ListActiveProviders() []interfaces.ProviderInfo {
	// Get provider info from wrapped manager
	providerInfos := a.wrapped.ListProviders()

	infos := make([]interfaces.ProviderInfo, 0, len(providerInfos))
	for _, info := range providerInfos {
		status := interfaces.ProviderStatusActive
		if info.Status != "running" {
			status = interfaces.ProviderStatusFailed
		}

		infos = append(infos, interfaces.ProviderInfo{
			Name:          info.Name,
			Version:       info.Version,
			Status:        status,
			LastUsed:      time.Now(), // Wrapped manager doesn't track this
			ResourceCount: 0,          // Would need to track this
		})
	}

	return infos
}

// Initialize initializes the provider manager
func (a *ProviderManagerAdapter) Initialize() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.initialized {
		return nil
	}

	a.initialized = true
	return nil
}

// Shutdown shuts down all providers and the manager
func (a *ProviderManagerAdapter) Shutdown() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Close wrapped manager (this will close all providers)
	if err := a.wrapped.Close(); err != nil {
		// Handle expected gRPC shutdown errors to prevent spurious error reports.
		// During concurrent shutdowns, connections may already be closed by the time
		// we attempt cleanup, which is normal and shouldn't be treated as an error.
		errStr := err.Error()
		if strings.Contains(errStr, "grpc: the client connection is closing") ||
			strings.Contains(errStr, "rpc error: code = Canceled") {
			// This is expected during shutdown - connection already closing
			a.logger.Info("Provider manager shutdown completed (connection already closing)")
		} else {
			return fmt.Errorf("failed to close wrapped provider manager: %w", err)
		}
	}

	a.initialized = false
	return nil
}

// ClearProviderCache clears the provider cache without shutting down
// This is useful for tests when providers may have died externally
func (a *ProviderManagerAdapter) ClearProviderCache() {
	// Clear the underlying provider manager's cache
	if pm, ok := a.wrapped.(*protocol.ProviderManager); ok {
		pm.ClearProviderCache()
	}
}

// IsHealthy checks if the provider manager is healthy
func (a *ProviderManagerAdapter) IsHealthy() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if !a.initialized {
		return false
	}

	// The wrapped manager will handle provider health checks
	return true
}

// GetMetrics returns provider manager metrics
func (a *ProviderManagerAdapter) GetMetrics() interfaces.ProviderManagerMetrics {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return a.metrics
}
