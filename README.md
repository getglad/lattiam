# Lattiam

Lattiam is a universal infrastructure automation platform that makes it easy to manage your cloud resources programmatically. It provides a simple REST API that accepts standard Terraform JSON, allowing you to integrate infrastructure management directly into applications and workflows, gaining the consistency of the Terraform provider ecosystem and the concreteness of Terraform deployment specs and plans while avoiding writing bespoke management code and needing to use the Terraform CLI.

## How It Works

Lattiam acts as a bridge between your application and your cloud providers. It takes your Terraform JSON configuration and communicates directly with the appropriate Terraform provider via gRPC to provision and manage your resources.

```
  +---------------------------------+
  |   Your Application or Client    |
  +---------------------------------+
               |
               | (Terraform JSON via REST API)
               v
  +---------------------------------+
  |         Lattiam Server          |
  +---------------------------------+
               |
               | (gRPC)
               v
  +---------------------------------+
  |      Terraform Providers        |
  | (AWS, Azure, GCP, Kubernetes)   |
  +---------------------------------+
               |
               | (Native Cloud APIs)
               v
  +---------------------------------+
  |         Cloud Resources         |
  +---------------------------------+
```

## Why Lattiam?

Lattiam makes it simple to vend standard infrastructure blocks programmatically across multiple environments. It allows you to build a secure, centralized service for managing cloud resources, powered by the Terraform provider ecosystem.

### The Multi-Environment & Credential Problem

Managing infrastructure at scale presents two major challenges:

1.  **The Multi-Environment Problem:** Deploying the same infrastructure pattern to N environments (e.g., 100s of AWS accounts or Kubernetes clusters) with tools like Terraform often requires complex wrapper scripts, duplicated state files, and significant effort to avoid configuration drift.
2.  **The Credential Management Problem:** In a typical CI/CD workflow, your automation system needs direct, privileged access to your cloud providers, increasing your security exposure.

### Lattiam's Solution: Terraform as a Service

Lattiam addresses these challenges by providing a centralized service that you control:

- **Solve the Multi-Environment Problem:** Deploy the same infrastructure pattern to any number of environments with simple API calls. Each deployment maintains its own state, ensuring consistency and eliminating drift without complex orchestration.
- **Solve the Credential Management Problem:** Your teams and CI/CD pipelines can define and deploy infrastructure without ever handling cloud credentials. They only need access to the Lattiam API. The Lattiam service is the only component that requires privileged access, creating a secure execution boundary.

This approach allows you to build a robust, secure, and scalable internal developer platform (IDP) for infrastructure.

### Ideal Use Cases

- **Platform Teams** building self-service infrastructure capabilities.
- **Automating Infrastructure** in your applications and CI/CD pipelines.
- **Standardizing Infrastructure** patterns across multiple teams and environments.

### Not Designed For

- Teams satisfied with Terraform CLI workflows
- Complex multi-provider orchestrations
- One-off infrastructure deployments

## Key Features

- **API-Driven Workflow:** Manage infrastructure through a simple REST API, not a CLI.
- **Secure Execution:** Centralize credential management and reduce your security footprint.
- **Familiar Configuration:** Use standard Terraform JSON to define your resources, including multiple resources within a single deployment.
- **No CLI Required:** Lattiam communicates directly with Terraform providers, bypassing the CLI.
- **Broad Provider Support:** Leverage the entire ecosystem of Terraform providers.
- **Async Operations:** Track and manage your deployments with asynchronous API operations.

## Documentation

### User Guide

- [Getting Started](./docs/user-guide/01-getting-started.md)
- [API Reference](./docs/user-guide/02-api-reference.md)
- [Using Data Sources](./docs/user-guide/03-using-data-sources.md)
- [Deployment Guide](./docs/user-guide/04-deployment-guide.md)

### Development

- [Architecture Overview](./docs/development/01-architecture-overview.md)
- [Provider Support](./docs/development/02-provider-support.md)

## Getting Started

### Prerequisites

- Go 1.21+
- Make
- Provider credentials

### Installation

```bash
git clone https://github.com/lattiam/lattiam.git
cd lattiam
make build
```

### Your First Deployment

1.  **Start the API server**:

    ```bash
    ./build/lattiam server start
    # Server running on http://localhost:8084
    ```

2.  **Deploy infrastructure via API**:

    ```bash
    curl -X POST http://localhost:8084/api/v1/deployments \
      -H "Content-Type: application/json" \
      -d '{
        "name": "my-s3-bucket",
        "terraform_json": {
          "resource": {
            "aws_s3_bucket": {
              "demo": {
                "bucket": "my-unique-bucket-name-12345"
              }
            }
          }
        }
      }'

    # Response includes deployment ID for tracking
    ```

3.  **Check deployment status**:

    ```bash
    curl http://localhost:8084/api/v1/deployments/$DEPLOYMENT_ID

    # Response shows detailed status:
    # {
    #   "id": "dep-1751284071651033000-79",
    #   "name": "my-s3-bucket",
    #   "status": "completed",
    #   "resources": [{
    #     "type": "aws_s3_bucket",
    #     "name": "demo",
    #     "status": "completed",
    #     "state": {
    #       "arn": "arn:aws:s3:::my-unique-bucket-name-12345",
    #       "bucket": "my-unique-bucket-name-12345",
    #       "region": "us-east-1"
    #       // ... additional state details
    #     }
    #   }],
    #   "summary": {
    #     "total_resources": 1,
    #     "successful_resources": 1,
    #     "failed_resources": 0,
    #     "pending_resources": 0
    #   }
    # }
    ```

4.  **Update the deployment** (e.g., add tags):

    ```bash
    curl -X PUT http://localhost:8084/api/v1/deployments/$DEPLOYMENT_ID \
      -H "Content-Type: application/json" \
      -d '{
        "terraform_json": {
          "resource": {
            "aws_s3_bucket": {
              "demo": {
                "bucket": "my-unique-bucket-name-12345",
                "tags": {
                  "Environment": "production",
                  "Team": "platform"
                }
              }
            }
          }
        }
      }'
    ```

5.  **Delete the deployment** (destroy resources):

    ```bash
    curl -X DELETE http://localhost:8084/api/v1/deployments/$DEPLOYMENT_ID

    # Check status to confirm destruction
    curl http://localhost:8084/api/v1/deployments/$DEPLOYMENT_ID
    # Status will show "destroying" then "destroyed"
    ```

## Community

- **Report a Bug:** Have you found a bug? Please [open an issue](https://github.com/lattiam/lattiam/issues/new?template=bug_report.md).
- **Request a Feature:** Have an idea for a new feature? [Open a feature request](https://github.com/lattiam/lattiam/issues/new?template=feature_request.md).

## Contributing

We welcome contributions from the community! Please see our [Contributing Guide](./docs/contributing/README.md) for more information on how to get involved.

