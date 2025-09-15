package interfaces

// ResourceExecutionState represents the execution state of a resource
type ResourceExecutionState string

// ResourceExecutionState constants represent the various states of resource execution
const (
	ResourceStatePending   ResourceExecutionState = "pending"
	ResourceStateExecuting ResourceExecutionState = "executing"
	ResourceStateCompleted ResourceExecutionState = "completed"
	ResourceStateFailed    ResourceExecutionState = "failed"
	ResourceStateSkipped   ResourceExecutionState = "skipped"
)
