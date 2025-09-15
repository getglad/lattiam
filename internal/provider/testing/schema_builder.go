package testing

import (
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// SchemaBuilder provides a fluent API for building Terraform provider schemas
type SchemaBuilder struct {
	provider    *tfprotov6.Schema
	resources   map[string]*tfprotov6.Schema
	dataSources map[string]*tfprotov6.Schema
}

// NewSchemaBuilder creates a new schema builder
func NewSchemaBuilder() *SchemaBuilder {
	return &SchemaBuilder{
		resources:   make(map[string]*tfprotov6.Schema),
		dataSources: make(map[string]*tfprotov6.Schema),
	}
}

// WithProvider sets the provider schema
func (sb *SchemaBuilder) WithProvider() *ProviderSchemaBuilder {
	return &ProviderSchemaBuilder{
		parent: sb,
		block:  &tfprotov6.SchemaBlock{},
	}
}

// WithResource adds a resource schema
func (sb *SchemaBuilder) WithResource(name string) *ResourceSchemaBuilder {
	return &ResourceSchemaBuilder{
		parent:       sb,
		resourceName: name,
		block:        &tfprotov6.SchemaBlock{},
	}
}

// WithDataSource adds a data source schema
func (sb *SchemaBuilder) WithDataSource(name string) *DataSourceSchemaBuilder {
	return &DataSourceSchemaBuilder{
		parent:         sb,
		dataSourceName: name,
		block:          &tfprotov6.SchemaBlock{},
	}
}

// Build creates the final GetProviderSchemaResponse
func (sb *SchemaBuilder) Build() *tfprotov6.GetProviderSchemaResponse {
	return &tfprotov6.GetProviderSchemaResponse{
		Provider:          sb.provider,
		ResourceSchemas:   sb.resources,
		DataSourceSchemas: sb.dataSources,
	}
}

// ProviderSchemaBuilder builds provider configuration schema
type ProviderSchemaBuilder struct {
	parent *SchemaBuilder
	block  *tfprotov6.SchemaBlock
}

// WithStringAttribute adds a string attribute to the provider schema
func (psb *ProviderSchemaBuilder) WithStringAttribute(name string, required bool) *ProviderSchemaBuilder {
	attr := &tfprotov6.SchemaAttribute{
		Name:     name,
		Type:     tftypes.String,
		Required: required,
		Optional: !required,
	}
	psb.block.Attributes = append(psb.block.Attributes, attr)
	return psb
}

// End finalizes the provider schema and returns to the main builder
func (psb *ProviderSchemaBuilder) End() *SchemaBuilder {
	psb.parent.provider = &tfprotov6.Schema{Block: psb.block}
	return psb.parent
}

// ResourceSchemaBuilder builds resource schemas
type ResourceSchemaBuilder struct {
	parent       *SchemaBuilder
	resourceName string
	block        *tfprotov6.SchemaBlock
}

// WithStringAttribute adds a string attribute
func (rsb *ResourceSchemaBuilder) WithStringAttribute(name string, config AttributeConfig) *ResourceSchemaBuilder {
	attr := &tfprotov6.SchemaAttribute{
		Name:     name,
		Type:     tftypes.String,
		Required: config.Required,
		Optional: config.Optional,
		Computed: config.Computed,
	}
	rsb.block.Attributes = append(rsb.block.Attributes, attr)
	return rsb
}

// WithBoolAttribute adds a boolean attribute
func (rsb *ResourceSchemaBuilder) WithBoolAttribute(name string, config AttributeConfig) *ResourceSchemaBuilder {
	attr := &tfprotov6.SchemaAttribute{
		Name:     name,
		Type:     tftypes.Bool,
		Required: config.Required,
		Optional: config.Optional,
		Computed: config.Computed,
	}
	rsb.block.Attributes = append(rsb.block.Attributes, attr)
	return rsb
}

// WithListStringAttribute adds a list of strings attribute
func (rsb *ResourceSchemaBuilder) WithListStringAttribute(name string, config AttributeConfig) *ResourceSchemaBuilder {
	attr := &tfprotov6.SchemaAttribute{
		Name:     name,
		Type:     tftypes.List{ElementType: tftypes.String},
		Required: config.Required,
		Optional: config.Optional,
		Computed: config.Computed,
	}
	rsb.block.Attributes = append(rsb.block.Attributes, attr)
	return rsb
}

// End finalizes the resource schema and returns to the main builder
func (rsb *ResourceSchemaBuilder) End() *SchemaBuilder {
	rsb.parent.resources[rsb.resourceName] = &tfprotov6.Schema{Block: rsb.block}
	return rsb.parent
}

// DataSourceSchemaBuilder builds data source schemas
type DataSourceSchemaBuilder struct {
	parent         *SchemaBuilder
	dataSourceName string
	block          *tfprotov6.SchemaBlock
}

// WithStringAttribute adds a string attribute
func (dsb *DataSourceSchemaBuilder) WithStringAttribute(name string, config AttributeConfig) *DataSourceSchemaBuilder {
	attr := &tfprotov6.SchemaAttribute{
		Name:     name,
		Type:     tftypes.String,
		Required: config.Required,
		Optional: config.Optional,
		Computed: config.Computed,
	}
	dsb.block.Attributes = append(dsb.block.Attributes, attr)
	return dsb
}

// WithBoolAttribute adds a boolean attribute
func (dsb *DataSourceSchemaBuilder) WithBoolAttribute(name string, config AttributeConfig) *DataSourceSchemaBuilder {
	attr := &tfprotov6.SchemaAttribute{
		Name:     name,
		Type:     tftypes.Bool,
		Required: config.Required,
		Optional: config.Optional,
		Computed: config.Computed,
	}
	dsb.block.Attributes = append(dsb.block.Attributes, attr)
	return dsb
}

// WithListStringAttribute adds a list of strings attribute
func (dsb *DataSourceSchemaBuilder) WithListStringAttribute(name string, config AttributeConfig) *DataSourceSchemaBuilder {
	attr := &tfprotov6.SchemaAttribute{
		Name:     name,
		Type:     tftypes.List{ElementType: tftypes.String},
		Required: config.Required,
		Optional: config.Optional,
		Computed: config.Computed,
	}
	dsb.block.Attributes = append(dsb.block.Attributes, attr)
	return dsb
}

// End finalizes the data source schema and returns to the main builder
func (dsb *DataSourceSchemaBuilder) End() *SchemaBuilder {
	dsb.parent.dataSources[dsb.dataSourceName] = &tfprotov6.Schema{Block: dsb.block}
	return dsb.parent
}

// AttributeConfig configures attribute properties
type AttributeConfig struct {
	Required bool
	Optional bool
	Computed bool
}

// Required creates a required attribute config
func Required() AttributeConfig {
	return AttributeConfig{Required: true}
}

// Optional creates an optional attribute config
func Optional() AttributeConfig {
	return AttributeConfig{Optional: true}
}

// Computed creates a computed attribute config
func Computed() AttributeConfig {
	return AttributeConfig{Computed: true}
}
