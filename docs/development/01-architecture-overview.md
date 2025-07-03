# Architecture

This document describes the system design and implementation of Lattiam.

## System Overview

Lattiam provides "Terraform as a Service" - a REST API that accepts Terraform JSON and executes deployments using Terraform providers via gRPC. This architecture enables platform teams to build infrastructure automation without managing Terraform state or credentials in their applications.

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│ Client Apps     │     │ REST API        │     │ Deployment      │     │ Terraform       │
│                 │────▶│                 │────▶│ Service         │────▶│ Providers       │
│ No credentials  │     │ Async ops       │     │ State mgmt      │     │ gRPC protocol   │
│ JSON configs    │     │ Multi-tenant    │     │ Credentials     │     │ Direct comms    │
└─────────────────┘     └─────────────────┘     └─────────────────┘     └─────────────────┘
                                                         │
                                                         ▼
                                             ┌─────────────────────┐
                                             │ Multiple Cloud      │
                                             │ Environments        │
                                             │ - AWS accounts      │
                                             │ - Azure subs        │
                                             │ - GCP projects      │
                                             └─────────────────────┘
```

### Service Architecture Benefits

1. **Credential Isolation**: Client applications never handle cloud credentials
2. **Multi-Environment**: Deploy to N accounts/regions with simple API calls
3. **State Management**: Centralized state storage with proper locking
4. **Async Operations**: Non-blocking deployments with status tracking
5. **Provider Direct**: No Terraform CLI subprocess management

## Core Design Principles

### 1. Schema-Driven Resource Management

- Provider schemas are the source of truth for resource definitions.
- Lattiam uses these schemas to understand resource attributes, types, and validation rules.
- This enables dynamic handling of any Terraform resource without hardcoding its structure.

### 2. Direct Provider Communication

- Lattiam communicates directly with Terraform providers using the gRPC protocol.
- This eliminates the need for the Terraform CLI or HCL files in the deployment process.
- Providers run as isolated subprocesses, and data is exchanged using MessagePack encoding.

### 3. Runtime Schema Discovery

- Lattiam fetches provider schemas at runtime.
- This allows for dynamic support of any Terraform provider and version without requiring code generation.
- Schemas are cached in memory to optimize performance.

## Component Architecture

### Deployment Service (`internal/apiserver/deployment_service.go`)

This is the core of Lattiam, managing the lifecycle of infrastructure deployments. It handles:

- Receiving Terraform JSON configurations via the API
- Orchestrating provider interactions (create, update, destroy)
- Managing deployment state and status
- Handling asynchronous deployment execution

### Provider Manager (`internal/provider/`)

Manages Terraform provider lifecycle:

```go
type Manager interface {
    GetProvider(ctx context.Context, name, version string) (Provider, error)
    DownloadProvider(ctx context.Context, name, version string) error
    Close() error
}
```

Key responsibilities:

- Download provider binaries from registry
- Start provider subprocess with magic cookie
- Manage provider lifecycle (start/stop)
- Handle concurrent provider access

### Protocol Layer (`internal/protocol/`)

Implements gRPC communication with providers:

```go
type Client interface {
    GetProviderSchema(ctx context.Context) (*GetProviderSchemaResponse, error)
    ConfigureProvider(ctx context.Context, config map[string]interface{}) error
    ApplyResourceChange(ctx context.Context, req *ApplyResourceChangeRequest) (*ApplyResourceChangeResponse, error)
}
```

Uses Terraform plugin protocol v5/v6 for compatibility.

## Data Flow

Lattiam processes Terraform JSON configurations through the following steps:

### 1. Terraform JSON Input

Client applications send Terraform JSON to the Lattiam API. For example:

```json
{
  "resource": {
    "aws_iam_role": {
      "app_role": {
        "name": "app-role",
        "assume_role_policy": "{\"Version\":\"2012-10-17\",\"Statement\":[{\"Effect\":\"Allow\",\"Principal\":{\"Service\":\"ec2.amazonaws.com\"},\"Action\":\"sts:AssumeRole\"}]}",
        "description": "Application role",
        "tags": {
          "Environment": "prod"
        }
      }
    }
  }
}
```

### 2. Resource Parsing and Validation

Lattiam parses the incoming Terraform JSON, extracts resource definitions, and performs initial validation based on the expected structure.

### 3. Provider Communication

Lattiam establishes a gRPC connection with the appropriate Terraform provider. It then sends the resource configuration to the provider for processing. For example, to create a resource:

```go
// Send to provider via gRPC
request := &ApplyResourceChangeRequest{
    TypeName:     "aws_iam_role",
    Config:       tfConfig, // Configuration in cty.Value format
    PlannedState: tfConfig,
}
response := client.ApplyResourceChange(request)
```

### 4. Resource Provisioning

The Terraform provider executes the actual cloud API calls to provision or manage the resource in the target cloud environment. The provider then returns the resource state and any relevant information back to Lattiam.

### 5. State Management and Tracking

Lattiam stores the resulting Terraform state and updates the deployment's status. This state is persisted and used for subsequent updates, plans, and destructions.

## Provider Protocol

### Protocol Handshake

1. Start provider with magic cookie environment variable
2. Provider outputs address: `1|5|tcp|127.0.0.1:1234|grpc`
3. Parse address and protocol version
4. Establish gRPC connection
5. Configure provider with credentials

### Message Flow

```
Client                    Provider
  │                          │
  ├──GetProviderSchema───────▶
  ◀────────SchemaResponse────┤
  │                          │
  ├──ConfigureProvider───────▶
  ◀────────ConfigResponse────┤
  │                          │
  ├──ApplyResourceChange─────▶
  ◀─────────StateResponse────┤
```

## Error Handling

### Provider Errors

- Connection failures → retry with backoff
- Schema fetch errors → fail fast
- Configuration errors → return validation details
- Resource conflicts → surface provider messages

### Transformation Errors

- Missing required fields → clear error messages
- Type mismatches → show expected vs actual
- Invalid references → suggest corrections

## Testing Strategy

### Unit Tests

- Transformation logic with mock schemas
- Type inference patterns
- Error handling paths

### Integration Tests

- Real provider communication
- End-to-end resource creation
- Schema extraction validation

### Test Infrastructure

- Can uses Localstack container for AWS testing
- Provider binaries cached locally
- Parallel test execution supported

## Performance Considerations

### Provider Management

- Reuse provider instances across operations
- Lazy provider startup
- Connection pooling for gRPC

### Schema Caching

- Cache schemas in memory during runtime
- Pre-generate common resource types
- Avoid repeated schema fetches

### Transformation Speed

- Direct map operations, no reflection
- Compiled regex patterns
- Minimal allocations

## Extensibility

### Adding Provider Support

1. Provider must support Terraform plugin protocol
2. Add provider download metadata
3. Test with sample resources

## Future Enhancements

### Drift Detection

- Implement detection of configuration drift against real-world infrastructure.

