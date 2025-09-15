package interfaces

// Metadata key constants used across the deployment system
const (
	// Metadata keys for deployment operations
	MetadataKeyOperation            = "operation"
	MetadataKeyDeploymentID         = "deployment_id"
	MetadataKeyOriginalDeploymentID = "original_deployment_id"
	MetadataKeyUpdateID             = "update_id"
	MetadataKeyUpdatePlan           = "update_plan"
	MetadataKeyTerraformJSON        = "terraform_json"
	MetadataKeyIsUpdate             = "is_update"
	MetadataKeyName                 = "name"
	MetadataKeyRequestID            = "request_id"
	MetadataKeyProviderVersion      = "provider_version"

	// Operation types
	OperationUpdate  = "update"
	OperationDestroy = "destroy"
)
