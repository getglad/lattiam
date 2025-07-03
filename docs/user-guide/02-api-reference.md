# Lattiam API Format

## OpenAPI Specification

The complete API specification is available in OpenAPI 3.0 format at `/openapi.yaml`. This specification can be used to:

- Generate client libraries in various languages
- Import into API testing tools like Postman or Insomnia
- Generate API documentation with tools like Swagger UI

## Provider Support

Lattiam manages Terraform providers automatically. See the [Provider Documentation](../development/02-provider-support.md) for details on how provider support works and how to add new providers.

## New Format (Terraform JSON)

The new API expects standard Terraform JSON format:

```json
{
  "resource": {
    "aws_iam_role": {
      "my_role": {
        "name": "my-role",
        "assume_role_policy": "{...}",
        "description": "My IAM role"
      }
    }
  }
}
```

## API Usage

### Create Deployment

Send deployments to the API:

```bash
curl -X POST http://localhost:8084/api/v1/deployments \
  -H "Content-Type: application/json" \
  -d '{
    "name": "my-deployment",
    "terraform_json": {
      "resource": {
        "aws_iam_role": {
          "example": {
            "name": "test-role",
            "assume_role_policy": "{...}"
          }
        }
      }
    }
  }'
```

### Update Deployment

Update an existing deployment (name, configuration, or resources):

```bash
# Update deployment name
curl -X PUT http://localhost:8084/api/v1/deployments/{id} \
  -H "Content-Type: application/json" \
  -d '{
    "name": "updated-deployment"
  }'

# Update deployment configuration
curl -X PUT http://localhost:8084/api/v1/deployments/{id} \
  -H "Content-Type: application/json" \
  -d '{
    "config": {
      "aws_profile": "production",
      "aws_region": "us-west-2"
    }
  }'

# Update deployment resources (triggers redeployment)
curl -X PUT http://localhost:8084/api/v1/deployments/{id} \
  -H "Content-Type: application/json" \
  -d '{
    "terraform_json": {
      "resource": {
        "aws_s3_bucket": {
          "updated_bucket": {
            "bucket": "my-updated-bucket"
          }
        }
      }
    }
  }'
```

Note: Deployments cannot be updated while they are in progress (planning/applying) or after they have been destroyed.

### Plan Deployment Changes

Generate a plan showing what changes would be made to a deployment:

```bash
# Plan with current configuration
curl -X POST http://localhost:8084/api/v1/deployments/{id}/plan

# Plan with new configuration
curl -X POST http://localhost:8084/api/v1/deployments/{id}/plan \
  -H "Content-Type: application/json" \
  -d '{
    "terraform_json": {
      "resource": {
        "aws_s3_bucket": {
          "updated_bucket": {
            "bucket": "my-new-bucket"
          }
        }
      }
    }
  }'
```

Response:

```json
{
  "deployment_id": "dep-1234567890",
  "plan": {
    "deployment_id": "dep-1234567890",
    "current_status": "completed",
    "resource_plans": [
      {
        "type": "aws_s3_bucket",
        "name": "updated_bucket",
        "action": "create",
        "proposed_state": {
          "bucket": "my-new-bucket"
        }
      }
    ],
    "summary": {
      "create": 1,
      "update": 0,
      "destroy": 0,
      "total": 1
    }
  },
  "created_at": "2024-01-15T10:30:00Z"
}
```

### List Deployments

Get all deployments:

```bash
# Simple list
curl http://localhost:8084/api/v1/deployments

# Detailed list with resource information
curl http://localhost:8084/api/v1/deployments?detailed=true
```

### Get Deployment

Get detailed information about a specific deployment:

```bash
curl http://localhost:8084/api/v1/deployments/{id}
```

### Delete Deployment

Destroy resources and remove deployment:

```bash
curl -X DELETE http://localhost:8084/api/v1/deployments/{id}
```

### Health Check

Check API server health and LocalStack connectivity:

```bash
curl http://localhost:8084/api/v1/health
```

## Multi-Resource Deployments

Lattiam's API can accept Terraform JSON configurations containing multiple resources within a single deployment request. Lattiam automatically analyzes resource dependencies and deploys them in the correct order.

For example, you can define a VPC and a subnet within the same `terraform_json` block, and Lattiam will ensure the VPC is created before the subnet.

```json
{
  "name": "my-vpc-and-subnet",
  "terraform_json": {
    "resource": {
      "aws_vpc": {
        "main": {
          "cidr_block": "10.0.0.0/16"
        }
      },
      "aws_subnet": {
        "public": {
          "vpc_id": "${aws_vpc.main.id}",
          "cidr_block": "10.0.1.0/24"
        }
      }
    }
  }
}
```

This simplifies managing related infrastructure components within a single API call.

## Benefits

1. **Less Maintenance**: No custom schemas to maintain
2. **Terraform Compatibility**: Standard Terraform resources work
3. **Existing Documentation**: Terraform docs apply directly
4. **API Interface**: REST API for automation

## API Reference

**Security Note:** Lattiam currently does not have built-in authentication or authorization for its API. For production deployments, it is critical to secure the API using external solutions such as API Gateways, reverse proxies, or network-level access controls.

### Base URL

```
http://localhost:8084/api/v1
```

### Endpoints

#### Health Check

- **GET** `/health`
- **Description**: Check API server health and LocalStack connectivity
- **Response**:
  ```json
  {
    "status": "healthy",
    "timestamp": "2024-01-15T10:30:00Z",
    "version": "0.1.0",
    "localstack": {
      "status": "healthy"
    }
  }
  ```

#### Deployments

##### Create Deployment

- **POST** `/deployments`
- **Description**: Create a new deployment
- **Request Body**:
  ```json
  {
    "name": "my-deployment",
    "terraform_json": {
      "resource": { ... }
    },
    "config": {
      "aws_profile": "default",
      "aws_region": "us-east-1"
    }
  }
  ```
- **Response**: 201 Created
  ```json
  {
    "id": "dep-1234567890",
    "name": "my-deployment",
    "status": "pending"
  }
  ```

##### List Deployments

- **GET** `/deployments`
- **Query Parameters**:
  - `detailed` (optional): Return detailed information including resources
- **Response**: 200 OK
  ```json
  [
    {
      "id": "dep-1234567890",
      "name": "my-deployment",
      "status": "completed"
    }
  ]
  ```

##### Get Deployment

- **GET** `/deployments/{id}`
- **Description**: Get detailed deployment information
- **Response**: 200 OK
  ```json
  {
    "id": "dep-1234567890",
    "name": "my-deployment",
    "status": "completed",
    "resources": [...],
    "metadata": {...},
    "created_at": "2024-01-15T10:00:00Z",
    "updated_at": "2024-01-15T10:01:00Z"
  }
  ```

##### Update Deployment

- **PUT** `/deployments/{id}`
- **Description**: Update deployment name, config, or resources
- **Request Body**: Any of:
  ```json
  {
    "name": "new-name",
    "config": { ... },
    "terraform_json": { ... }
  }
  ```
- **Response**: 200 OK

##### Delete Deployment

- **DELETE** `/deployments/{id}`
- **Description**: Destroy resources and remove deployment
- **Response**: 204 No Content

##### Plan Deployment

- **POST** `/deployments/{id}/plan`
- **Description**: Generate a plan for deployment changes
- **Request Body** (optional):
  ```json
  {
    "terraform_json": { ... }
  }
  ```
- **Response**: 200 OK
  ```json
  {
    "deployment_id": "dep-1234567890",
    "plan": {
      "resource_plans": [...],
      "summary": {
        "create": 0,
        "update": 1,
        "destroy": 0,
        "total": 1
      }
    },
    "created_at": "2024-01-15T10:30:00Z"
  }
  ```

### Status Codes

- **200 OK**: Request succeeded
- **201 Created**: Resource created successfully
- **204 No Content**: Request succeeded with no response body
- **400 Bad Request**: Invalid request format or parameters
- **404 Not Found**: Resource not found
- **409 Conflict**: Operation not allowed in current state
- **500 Internal Server Error**: Server error

### Deployment Statuses

- `pending`: Deployment created but not started
- `planning`: Planning resource changes
- `applying`: Creating/updating resources
- `completed`: All resources deployed successfully
- `failed`: Deployment failed
- `destroying`: Destroying resources
- `destroyed`: All resources destroyed

### Configuration Options

The `config` object in deployment requests supports:

- `state_backend`: Custom state backend location (e.g., `s3://bucket/path/`, `gs://bucket/path/`, `azurerm://account/container/`)

### Environment Variables

The API server respects these environment variables:

- `LATTIAM_STATE_DIR`: Directory for persistent state storage
- `LATTIAM_TERRAFORM_BACKEND`: Default state backend for all deployments (can be overridden per deployment)
- `AWS_ENDPOINT_URL`: Custom endpoint (e.g., LocalStack)
- `AWS_PROVIDER_VERSION`: Override AWS provider version
- `LATTIAM_PROVIDER_TIMEOUT`: Provider operation timeout
- `LATTIAM_PROVIDER_STARTUP_TIMEOUT`: Provider startup timeout
- `LATTIAM_DEPLOYMENT_TIMEOUT`: Overall deployment timeout
