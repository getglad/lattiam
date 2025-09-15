package protocol

// TerraformRawState stores the raw provider state data
type TerraformRawState struct {
	StateData     []byte // Raw msgpack state from provider
	PrivateData   []byte // Provider's private data for refresh/destroy
	SchemaVersion int    // Schema version of the resource
}

// ResourceState contains both the processed state and raw Terraform state data
type ResourceState struct {
	State          map[string]interface{}
	TerraformState *TerraformRawState
}
