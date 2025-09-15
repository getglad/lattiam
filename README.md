# Lattiam - Infrastructure Deployment Service

Run Terraform providers as a service. Lattiam manages provider processes, queues deployments, and handles state by processing Terraform JSON submitted to a web endpoint.

```
+---------------------------------+
|    Platform Team's Tooling     |
+---------------------------------+
               |
               | (Terraform JSON via HTTP)
               v
+---------------------------------+
|         Lattiam Service         |
+---------------------------------+
               |
               | (gRPC)
               v
+---------------------------------+
|      Terraform Providers        |
|      (AWS, Random, etc.)        |
+---------------------------------+
               |
               | (Native Cloud APIs)
               v
+---------------------------------+
|         Cloud Resources         |
+---------------------------------+
```

Built for teams who need scaled out infrastructure deployments.

## When to Use Lattiam

**✅ Perfect if you:**

- Build internal platforms for infrastructure
- Need programmatic infrastructure deployment
- Want centralized credential management
- Deploy the same resources to many environments
- Integrate infrastructure into CI/CD pipelines or backend processes

**🤔 Consider alternatives if you:**

- Are happy with your existing Terraform CLI workflows
- Need Terraform workspaces for environment management
- Require HCL modules from Terraform Registry
- Use `terraform import` to adopt existing resources

## Quick Start

```bash
# 1. Build and start the server
git clone https://github.com/lattiam/lattiam.git
cd lattiam
make build
./build/lattiam server start

# API running on http://localhost:8084

# 2. Deploy infrastructure
curl -X POST http://localhost:8084/api/v1/deployments \
  -H "Content-Type: application/json" \
  -d '{
    "name": "demo",
    "terraform_json": {
      "resource": {
        "random_string": {
          "demo": {"length": 8}
        }
      }
    }
  }'

# 3. Check status (use ID from step 2)
curl http://localhost:8084/api/v1/deployments/{id}

# 4. Clean up
curl -X DELETE http://localhost:8084/api/v1/deployments/{id}
```

## Server Management

```bash
# Start the server
lattiam server start              # Foreground (see logs)
lattiam server start --daemon     # Background mode

# Stop the server
lattiam server stop

# Check status
lattiam server status
```

For detailed configuration options, see [Configuration Guide](./docs/CONFIGURATION.md).

## API Request Format

Lattiam accepts Terraform JSON configurations wrapped in a deployment request:

```json
{
  "name": "my-infrastructure",
  "terraform_json": {
    "terraform": {
      "required_providers": {
        "random": {
          "source": "hashicorp/random",
          "version": "3.6.0"
        },
        "aws": {
          "source": "hashicorp/aws",
          "version": "~> 5.0"
        }
      }
    },
    "provider": {
      "aws": {
        "profile": "developer",
        "region": "us-east-1"
      }
    },
    "resource": {
      "random_string": {
        "bucket_suffix": {
          "length": 8,
          "special": false,
          "upper": false
        }
      },
      "aws_s3_bucket": {
        "example": {
          "bucket": "my-app-${random_string.bucket_suffix.result}"
        }
      }
    }
  }
}
```

The `terraform_json` field contains standard Terraform JSON syntax.

## How It Works

When you submit a deployment, Lattiam:

1. Downloads required provider binaries (if not cached)
2. Starts isolated provider processes for this deployment
3. Executes the Terraform plan
4. Manages state transitions
5. Cleans up provider processes
6. Returns results via API

## State Management

Lattiam stores deployment state locally by default (`~/.lattiam/state/`). For production use, configure the AWS backend (S3 + DynamoDB) - see [Configuration Guide](./docs/CONFIGURATION.md) for setup.

## Documentation

- [API Reference](./docs/API.md) - REST endpoints and examples
- [Configuration](./docs/CONFIGURATION.md) - Environment variables and settings

For contributors: See [Contributing Documentation](./docs/contributing/)
