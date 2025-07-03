package protocol

import (
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// GetCachedSchema returns a cached full provider schema if available
func (pm *ProviderManager) GetCachedSchema(providerKey string) (*tfprotov6.GetProviderSchemaResponse, bool) {
	pm.schemaMu.RLock()
	defer pm.schemaMu.RUnlock()
	schema, exists := pm.schemaCache[providerKey]
	return schema, exists
}

// SetCachedSchema stores a full provider schema in the cache
func (pm *ProviderManager) SetCachedSchema(providerKey string, schema *tfprotov6.GetProviderSchemaResponse) {
	pm.schemaMu.Lock()
	defer pm.schemaMu.Unlock()
	pm.schemaCache[providerKey] = schema
}

// ClearSchemaCache clears all cached schemas
func (pm *ProviderManager) ClearSchemaCache() {
	pm.schemaMu.Lock()
	defer pm.schemaMu.Unlock()
	pm.schemaCache = make(map[string]*tfprotov6.GetProviderSchemaResponse)
}
