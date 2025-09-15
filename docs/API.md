# API Reference

## OpenAPI Specification

The Lattiam API is documented using OpenAPI 3.0. Access the API documentation at:

- **Interactive documentation**: `http://localhost:8084/swagger/` (when server is running)
- **OpenAPI spec**: `/docs/swagger.yaml`

## Base URL

```
http://localhost:8084/api/v1
```

## Quick Examples

```bash
# Create deployment
curl -X POST http://localhost:8084/api/v1/deployments \
  -H "Content-Type: application/json" \
  -d @deployment.json

# Check status
curl http://localhost:8084/api/v1/deployments/{id}

# Destroy resources
curl -X DELETE http://localhost:8084/api/v1/deployments/{id}
```

For complete endpoint documentation, error codes, and schemas, see the interactive Swagger documentation.
