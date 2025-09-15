// Package interfaces defines core interfaces and configuration types for system components
package interfaces

import (
	"time"
)

// ComponentFactory defines the interface for creating system components
type ComponentFactory interface {
	CreateStateStore(config StateStoreConfig) (StateStore, error)
	CreateDependencyResolver(config DependencyResolverConfig) (DependencyResolver, error)
	CreateInterpolationResolver(config InterpolationResolverConfig) (InterpolationResolver, error)
	CreateWorkerPool(config PoolConfig) (Pool, error)

	// Interface creation methods
	CreateProviderLifecycleManager(config ProviderManagerConfig) (ProviderLifecycleManager, error)
	CreateProviderMonitor(config ProviderManagerConfig) (ProviderMonitor, error)
}

// StateStoreConfig provides configuration for creating a StateStore
type StateStoreConfig struct {
	Type             string // "file", "memory", "redis", "postgres"
	ConnectionString string
	Options          map[string]interface{}
}

// ProviderManagerConfig provides configuration for creating a ProviderManager
type ProviderManagerConfig struct {
	ProviderDirectory string
	MaxProviders      int
	CacheSchemas      bool
	Options           map[string]interface{}
}

// DependencyResolverConfig provides configuration for creating a DependencyResolver
type DependencyResolverConfig struct {
	EnableCaching   bool
	MaxGraphSize    int
	EnableDebugMode bool
	Options         map[string]interface{}
}

// InterpolationResolverConfig provides configuration for creating an InterpolationResolver
type InterpolationResolverConfig struct {
	EnableStrictMode  bool
	CustomFunctions   map[string]InterpolationFunction
	MaxRecursionDepth int
	Options           map[string]interface{}
}

// PoolConfig provides configuration for creating a Pool
type PoolConfig struct {
	MinWorkers  int
	MaxWorkers  int
	QueueSize   int
	IdleTimeout time.Duration
	Options     map[string]interface{}
}

// RetryConfig provides retry configuration for a component
type RetryConfig struct {
	MaxRetries      int
	InitialDelay    time.Duration
	MaxDelay        time.Duration
	Multiplier      float64
	RandomizeFactor float64
}
