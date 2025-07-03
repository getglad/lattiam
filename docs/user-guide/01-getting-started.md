# Getting Started

This guide helps you deploy Lattiam as a service for programmatic infrastructure management across your organization.

## What is Lattiam?

Lattiam provides "Terraform as a Service" - a REST API that executes Terraform deployments without requiring callers to have cloud credentials or manage state files. It's designed for platform teams who need to:

- Deploy standard infrastructure patterns to multiple environments (accounts, regions, clusters)
- Enable teams to provision infrastructure without handling cloud credentials
- Maintain consistent state management across hundreds of deployments
- Build self-service infrastructure platforms

## Prerequisites for Running Lattiam

- Go 1.21 or later (for building from source)
- Terraform provider credentials (e.g., AWS profiles, environment variables) configured on the Lattiam server
- Network access from client applications to Lattiam API

**Important**: Only the Lattiam service needs cloud credentials. Client applications only need access to the Lattiam API.

## Installation

### For Platform Teams: Deploy as a Service

```bash
# Clone the repository
git clone https://github.com/lattiam/lattiam.git
cd lattiam

# Build the server binary
make build

# Start the API server
./build/lattiam server start
# Server running on http://localhost:8084
```

### For Production: Container Deployment

```dockerfile
# Dockerfile example (simplified)
FROM golang:1.21 as builder
WORKDIR /app
COPY . .
RUN make build

FROM ubuntu:22.04
COPY --from=builder /app/build/lattiam /usr/local/bin/
EXPOSE 8084
CMD ["lattiam", "server", "start"]
```

## Quick Start: Your First Deployment

This section guides you through starting the Lattiam API server and deploying your first resource using a simple `curl` command.

### Prerequisites

Before you begin, ensure you have:

- Go 1.21+ and Make (for building Lattiam)
- `curl` (for making API requests)
- AWS credentials configured in the environment where Lattiam server will run (e.g., `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION` environment variables, or a configured AWS profile).

### 1. Build and Start the Lattiam Server

First, clone the Lattiam repository and build the executable:

```bash
git clone https://github.com/lattiam/lattiam.git
cd lattiam
make build
```

Now, start the Lattiam API server. For a simple local test, you can run it in the foreground:

```bash
./build/lattiam server start
# Server running on http://localhost:8084
```

Keep this terminal window open, as the server will run here.

### 2. Deploy Your First Resource via API

Open a new terminal window. You can now send an API request to Lattiam to deploy an AWS S3 bucket.

```bash
curl -X POST http://localhost:8084/api/v1/deployments \
  -H "Content-Type: application/json" \
  -d '{
    "name": "my-first-s3-bucket",
    "terraform_json": {
      "resource": {
        "aws_s3_bucket": {
          "demo": {
            "bucket": "my-unique-bucket-name-$(date +%s)"
          }
        }
      }
    }
  }'
```

**Note:** The `$(date +%s)` command generates a unique suffix for the bucket name, as S3 bucket names must be globally unique.

The API will respond with a deployment ID, which you can use to track the status of your deployment.

### 3. Check Deployment Status

You can check the status of your deployment using the deployment ID returned from the previous step. Replace `$DEPLOYMENT_ID` with the actual ID.

```bash
curl http://localhost:8084/api/v1/deployments/$DEPLOYMENT_ID
```

The response will show the current status of your deployment, including details about the resources being provisioned.

### 4. Clean Up (Destroy Resources)

To destroy the resources created by your deployment, use the `DELETE` endpoint with the deployment ID:

```bash
curl -X DELETE http://localhost:8084/api/v1/deployments/$DEPLOYMENT_ID
```

Lattiam will begin destroying the resources. You can check the status again using the `GET` endpoint until the status changes to `destroyed`.

## Next Steps

- Read the [API Reference](./02-api-reference.md) to understand all available endpoints.
- Explore [Using Data Sources](./03-using-data-sources.md) for dynamic configurations.
- Learn about [Deployment Guide](./04-deployment-guide.md) for production setups.
- Browse additional [Example Configurations](../../examples/) for common use cases.
- If you're interested in contributing, see our [Contributing Guide](../../docs/contributing/README.md).
