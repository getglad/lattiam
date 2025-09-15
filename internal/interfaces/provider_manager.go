package interfaces

import (
	"time"
)

// ProviderConfig represents provider-specific configuration
type ProviderConfig map[string]map[string]interface{}

// ProviderInfo provides information about an active provider
type ProviderInfo struct {
	Name          string
	Version       string
	Status        ProviderStatus
	LastUsed      time.Time
	ResourceCount int
}

// ProviderStatus represents the status of a provider
type ProviderStatus string

// ProviderStatus constants represent the various states of a provider
const (
	ProviderStatusActive   ProviderStatus = "active"
	ProviderStatusIdle     ProviderStatus = "idle"
	ProviderStatusFailed   ProviderStatus = "failed"
	ProviderStatusShutdown ProviderStatus = "shutdown"
)

// ProviderSchema represents the schema for a provider
type ProviderSchema struct {
	Provider      *Schema
	ResourceTypes map[string]*ResourceSchema
	DataSources   map[string]*ResourceSchema
}

// ResourceSchema represents the schema for a resource or data source
type ResourceSchema struct {
	Version    int64
	Block      *Schema
	Deprecated string
}

// Schema represents a configuration block schema
type Schema struct {
	Attributes map[string]*Attribute
	BlockTypes map[string]*BlockType
}

// Attribute represents a schema attribute
type Attribute struct {
	Type        string
	Description string
	Required    bool
	Optional    bool
	Computed    bool
	Sensitive   bool
	Deprecated  string
}

// BlockType represents a nested block type in schema
type BlockType struct {
	Block       *Schema
	NestingMode string
	MinItems    int
	MaxItems    int
}

// ResourcePlan represents a planned resource change
type ResourcePlan struct {
	ResourceType    string
	Action          PlanAction
	Actions         []string // For simplified mock implementation
	CurrentState    map[string]interface{}
	ProposedState   map[string]interface{}
	PlannedState    map[string]interface{}
	BeforeState     map[string]interface{} // For mock implementation
	AfterState      map[string]interface{} // For mock implementation
	RequiresReplace []string
}

// PlanAction represents the action to be taken on a resource
type PlanAction string

// PlanAction constants represent the various actions in a plan
const (
	PlanActionCreate  PlanAction = "create"
	PlanActionUpdate  PlanAction = "update"
	PlanActionDelete  PlanAction = "delete"
	PlanActionReplace PlanAction = "replace"
	PlanActionNoop    PlanAction = "noop"
)

// ProviderManagerMetrics provides metrics about provider management
type ProviderManagerMetrics struct {
	ActiveProviders int
	TotalProviders  int
	SchemasCached   int
	ProviderErrors  int64
	AverageInitTime time.Duration
}

// ProviderManagerCall represents a call to the ProviderManager for tracking in mocks
type ProviderManagerCall struct {
	Method       string
	ProviderName string
	ResourceType string
	Timestamp    time.Time
	Error        error
}

// Diagnostic represents a diagnostic message from the provider
type Diagnostic struct {
	Severity   DiagnosticSeverity
	Summary    string
	Detail     string
	Attribute  string // Optional: specific attribute that caused the issue
	Operation  string // Optional: operation that generated this diagnostic
	ProviderID string // Provider that generated this diagnostic
}

// DiagnosticSeverity represents the severity of a diagnostic
type DiagnosticSeverity int

// DiagnosticSeverity constants represent the severity levels of diagnostics
const (
	SeverityInvalid DiagnosticSeverity = iota
	SeverityError
	SeverityWarning
)

// ProviderCapabilities describes what a provider supports
type ProviderCapabilities struct {
	ProtocolVersion   int
	SupportsRetry     bool
	SupportsDeferred  bool // v6.10+ deferred actions
	SupportsFunctions bool // v6.10+ provider functions
	MaxRetryAttempts  int
	CurrentRetryDelay string
}

// ProviderError is an error type that includes diagnostic context
type ProviderError struct {
	Operation    string
	Provider     string
	ResourceType string
	Diagnostics  []Diagnostic
	Err          error
}

func (e *ProviderError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	if len(e.Diagnostics) > 0 {
		// Return first error diagnostic
		for _, d := range e.Diagnostics {
			if d.Severity == SeverityError {
				return d.Summary + ": " + d.Detail
			}
		}
	}
	return "provider operation failed"
}

func (e *ProviderError) Unwrap() error {
	return e.Err
}

// HasErrors returns true if any diagnostics are errors
func (e *ProviderError) HasErrors() bool {
	for _, d := range e.Diagnostics {
		if d.Severity == SeverityError {
			return true
		}
	}
	return false
}

// GetWarnings returns only warning diagnostics
func (e *ProviderError) GetWarnings() []Diagnostic {
	var warnings []Diagnostic
	for _, d := range e.Diagnostics {
		if d.Severity == SeverityWarning {
			warnings = append(warnings, d)
		}
	}
	return warnings
}
