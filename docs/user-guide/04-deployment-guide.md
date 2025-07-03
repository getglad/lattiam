# Deployment Guide

This guide covers deploying Lattiam as a service for your organization.

## Overview

Lattiam is designed to run as a centralized service that handles infrastructure deployments on behalf of your teams. This architecture provides:

- **Credential Isolation**: Only the Lattiam service needs cloud provider access
- **State Management**: Centralized state storage and locking
- **Multi-Environment Support**: Deploy to any number of accounts/regions from a single service
- **API Access**: Teams interact via REST API, not directly with cloud providers

## Deployment Architecture

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│   Dev Team A    │     │   Dev Team B    │     │  CI/CD Pipeline │
└────────┬────────┘     └────────┬────────┘     └────────┬────────┘
         │                       │                         │
         │         REST API      │                         │
         └───────────────────────┴─────────────────────────┘
                                 │
                      ┌──────────▼──────────┐
                      │   Lattiam Service   │
                      │   (API on :8084)    │
                      └──────────┬──────────┘
                                 │
                      ┌──────────▼──────────┐
                      │  Cloud Credentials  │
                      │  - AWS Profiles     │
                      │  - Service Accounts │
                      └──────────┬──────────┘
                                 │
         ┌───────────────────────┼───────────────────────┐
         │                       │                       │
┌────────▼────────┐   ┌──────────▼──────────┐  ┌────────▼────────┐
│  AWS Account 1  │   │   AWS Account 2     │  │   GCP Project   │
└─────────────────┘   └─────────────────────┘  └─────────────────┘
```

## Deployment Options

### 1. Development/Testing

For local development and testing:

```bash
# Build the binary
make build

# Start the API server
./build/lattiam server start --port 8084
```

#### Configuration

- `LATTIAM_PORT` - API server port (default: 8084)
- `LATTIAM_LOG_LEVEL` - Logging level: debug, info, warn, error (default: info)
- `LATTIAM_STATE_DIR` - Directory for state files (default: ~/.lattiam/state)

### 2. Production Container Deployment

Deploy Lattiam as a containerized service with proper provider authentication.

### 3. Cloud Platform Deployment

Deploy to your preferred cloud platform:

- **Kubernetes**: Use a Deployment with persistent volumes for state
- **ECS/Fargate**: Run as a service with EFS for state storage
- **Cloud Run/App Engine**: With Cloud Storage for state backend

## Cloud Provider Authentication

**Important**: The Lattiam service requires provider credentials, not the client applications. This creates a security boundary where only Lattiam needs privileged access to your cloud environments.

Lattiam uses the cloud provider credentials configured in its own environment (e.g., environment variables, IAM instance profiles, service accounts) when executing deployments. This means all deployments processed by a single Lattiam instance will use the same underlying credentials.

### Multi-Account Support

To manage resources across multiple accounts or subscriptions, you have a few options:

1.  **Multiple Lattiam Instances:** Run separate Lattiam instances, each configured with credentials for a different target account/subscription.
2.  **`assume_role` within `terraform_json`:** For AWS, you can define an `aws_iam_role` resource in your `terraform_json` that assumes a role into a target account. Subsequent resources in the same `terraform_json` can then use this assumed role.
3.  **Provider Configuration in `terraform_json`:** You can include a `provider` block directly within your `terraform_json` to specify credentials or regions for that specific deployment. This configuration will override the Lattiam server's default environment credentials for that deployment.

### AWS

The AWS provider uses standard authentication methods:

1. **IAM Roles** (Recommended for production)

   - EC2 Instance Profiles
   - ECS Task Roles
   - EKS Pod Identity/IRSA
   - Lambda Execution Roles

2. **AWS Profiles**

   ```bash
   export AWS_PROFILE=lattiam-service
   export AWS_REGION=us-east-1
   ```

3. **Shared Credentials File**
   - Uses `~/.aws/credentials` automatically
   - Supports cross-account assume role configurations

### Azure

The Azure provider uses standard authentication:

1. **Managed Identity** (Recommended)

   - Azure VM Managed Identity
   - AKS Pod Identity

2. **Service Principal**
   ```bash
   export AZURE_SUBSCRIPTION_ID=subscription-id
   export AZURE_TENANT_ID=tenant-id
   export AZURE_CLIENT_ID=client-id
   export AZURE_CLIENT_SECRET=client-secret
   ```

### Google Cloud

The GCP provider uses standard authentication:

1. **Workload Identity** (Recommended)

   - GKE Workload Identity
   - Cloud Run Service Account

2. **Service Account Key**
   ```bash
   export GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account.json
   export GOOGLE_PROJECT=project-id
   ```

## Production Considerations

### Security

1. **TLS/SSL**

   - Always use HTTPS in production
   - Configure TLS certificates for the API server

2. **Authentication**

   - Implement proper API authentication for client access
   - Use IAM roles/managed identities for provider access
   - Consider OAuth2/OIDC for user authentication

3. **Network Security**
   - Deploy behind a load balancer
   - Configure security groups/firewall rules
   - Limit API access to trusted networks

### High Availability

1. **Multiple Instances**

   - Run multiple API server instances
   - Use a load balancer for distribution

2. **State Management**

   - Configure shared state backend (S3, GCS, Azure Storage)
   - Enable state locking for concurrent operations

3. **Health Checks**
   - Monitor `/health` endpoint
   - Configure automatic restarts

### Monitoring

1. **Logging**

   - Centralize logs using CloudWatch, Stackdriver, etc.
   - Set appropriate log levels for production

2. **Metrics**

   - Monitor API response times
   - Track deployment success/failure rates
   - Monitor provider communication health

3. **Alerts**
   - Set up alerts for failures
   - Monitor resource utilization

## Configuration Reference

### Environment Variables

| Variable                    | Description                                | Default          |
| --------------------------- | ------------------------------------------ | ---------------- |
| `LATTIAM_PORT`              | Not currently used (port set via CLI flag) | 8084             |
| `LATTIAM_LOG_LEVEL`         | Not currently implemented                  | info             |
| `LATTIAM_STATE_DIR`         | State storage directory                    | ~/.lattiam/state |
| `LATTIAM_TERRAFORM_BACKEND` | Terraform state backend type               | local            |
| `LATTIAM_DEBUG_DIR`         | Directory for debug provider logs          | none             |

### Provider Configuration

These environment variables configure the Lattiam server's own cloud provider credentials. All deployments executed by this Lattiam instance will use these credentials unless overridden by a `provider` block within the `terraform_json` of a specific deployment request.

## Deployment Checklist

- [ ] Build production binary with appropriate tags
- [ ] Configure cloud provider credentials
- [ ] Set up TLS/SSL certificates
- [ ] Configure logging and monitoring
- [ ] Set up health checks and auto-scaling
- [ ] Configure state backend for production
- [ ] Test deployment in staging environment
- [ ] Document runbooks for operations team

## Troubleshooting

### Common Issues

1. **Provider Connection Failures**

   - Check provider credentials
   - Verify network connectivity
   - Check provider binary permissions

2. **State Lock Conflicts**

   - Ensure proper state backend configuration
   - Check for stuck locks

3. **Memory Issues**
   - Increase container/instance memory
   - Monitor provider subprocess memory usage

### Debug Mode

Enable debug logging for troubleshooting:

```bash
export LATTIAM_LOG_LEVEL=debug
export LATTIAM_ENABLE_DEBUG=true
```

This provides detailed logs for:

- Provider communication
- State operations
- API request/response details
