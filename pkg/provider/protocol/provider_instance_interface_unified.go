package protocol

// ProviderInstanceInterface defines the common interface for provider instances
type ProviderInstanceInterface interface {
	// Basic provider information
	Name() string
	Version() string
	Path() string

	// Lifecycle management
	Stop() error
	IsHealthy() bool

	// Protocol version for unified client
	GetProtocolVersion() int

	// V5 client access
	GetGRPCClientV5() interface{}

	// V6 client access
	GetGRPCClientV6() interface{}
}
