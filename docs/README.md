# Configuration Guide

This guide helps you configure Lattiam for different scenarios and requirements.

## Getting Started

### Basic Setup

Start with the default configuration - Lattiam works out of the box with sensible defaults:

```bash
# Start the server with defaults
lattiam server start

# Or customize the port
lattiam server start --port 9090
```

### Configuration Methods

Configure Lattiam through:

1. **Environment variables** - Set before starting the server
2. **Command-line flags** - Pass when starting the server
3. **Configuration file** - (Future feature)

### Validating Your Configuration

```bash
# Show current configuration
lattiam config show

# Validate configuration
lattiam config validate

# Show storage paths
lattiam config paths
```

## Scaling Your Deployment

When you need to handle more concurrent deployments or larger infrastructure operations.

### Queue Configuration

#### Embedded Mode (Default)

Best for single-instance deployments with moderate load:

```bash
# Default configuration (no changes needed)
# - In-memory queue using Go channels
# - Queue capacity: 100 jobs
# - Workers: 1-4 (dynamically scaled)
# - Single instance only
```

#### Distributed Mode

For high-availability and multi-instance deployments:

```bash
LATTIAM_QUEUE_TYPE=distributed
LATTIAM_REDIS_URL=redis://localhost:6379

# Features:
# - Redis-backed queue
# - 10 workers per instance
# - Multi-instance capable
```

### Resource Limits

Lattiam enforces these limits to prevent resource exhaustion:

- **Request size**: 10MB maximum
- **Queue capacity**: 100 jobs (embedded mode)
- **Concurrent workers**: 1-4 (embedded), 10 (distributed)
- **Provider processes**: One per deployment

To handle larger deployments, use distributed mode with Redis.

## Configuring Storage

Choose where Lattiam stores deployment state and how it manages persistence.

### Local File Storage (Default)

Best for development and single-instance deployments:

```bash
# Configure state directory (default: ~/.lattiam/state/)
LATTIAM_STATE_DIR=/var/lib/lattiam/state

# Configure provider cache (default: ~/.lattiam/providers/)
LATTIAM_PROVIDER_DIR=/var/lib/lattiam/providers
```

Directory structure:

```
~/.lattiam/
├── state/
│   └── deployments/
│       ├── deploy-abc123.json
│       └── deploy-xyz789.json
└── providers/
    ├── aws/5.0.0/
    └── random/3.6.0/
```

### AWS Cloud Storage

For production environments requiring durability and team access:

```bash
# Enable AWS backend
LATTIAM_STATE_STORE=aws

# S3 Configuration
LATTIAM_AWS_S3_BUCKET=my-lattiam-state
LATTIAM_AWS_S3_REGION=us-east-1
LATTIAM_AWS_S3_PREFIX=states/

# DynamoDB Configuration (for locking)
LATTIAM_AWS_DYNAMODB_TABLE=lattiam-locks
LATTIAM_AWS_DYNAMODB_REGION=us-east-1

# AWS Credentials (standard AWS SDK variables)
AWS_ACCESS_KEY_ID=your-access-key
AWS_SECRET_ACCESS_KEY=your-secret-key
AWS_PROFILE=your-profile  # Alternative to keys
```

For LocalStack testing:

```bash
# Point to LocalStack endpoints
LATTIAM_AWS_S3_ENDPOINT=http://localhost:4566
LATTIAM_AWS_DYNAMODB_ENDPOINT=http://localhost:4566
AWS_ENDPOINT_URL=http://localhost:4566
```

### Backend Safety

Lattiam prevents accidental data loss when switching backends:

```bash
# If backend configuration changes, you'll see an error
# To force reinitialization (WARNING: DATA LOSS):
export LATTIAM_FORCE_BACKEND_REINIT=true
lattiam server start
```

## Debugging and Troubleshooting

When deployments fail or behave unexpectedly, enable debugging to diagnose issues.

### Debug Mode

Enable comprehensive logging to diagnose issues:

```bash
# Enable debug mode
LATTIAM_DEBUG=1
LATTIAM_DEBUG_DIR=/var/log/lattiam/debug
lattiam server start

# Or via command line
lattiam server start --debug
```

Debug mode creates detailed logs:

```
/var/log/lattiam/debug/
├── lattiam-main-{timestamp}.log      # Main server logs
├── provider-start-{timestamp}.log    # Provider startup
├── provider-aws-{timestamp}.log      # AWS provider logs
├── grpc-messages-{timestamp}.log     # Provider communication
└── state-changes-{timestamp}.log     # State transitions
```

### Server Logs

Configure where server logs are written:

```bash
# Log to file (default: stdout)
LATTIAM_LOG_FILE=/var/log/lattiam/server.log

# Run in background with logs
lattiam server start --daemon
tail -f /tmp/lattiam-server.log
```

### Provider Debugging

Each deployment gets isolated provider processes. To debug provider issues:

```bash
# Enable debug mode to see provider communication
LATTIAM_DEBUG=1 lattiam server start

# Check provider working directories
ls /tmp/lattiam-deployments/{deployment-id}/
```

Common provider issues:

- **EOF errors**: Provider process died unexpectedly
- **Plugin incompatible**: Protocol version mismatch
- **Timeout**: Provider operation taking too long

## Production Deployment

Configure Lattiam for reliable production operation.

### Server Configuration

```bash
# Server port
LATTIAM_PORT=8084

# PID file for process management
LATTIAM_PID_FILE=/var/run/lattiam.pid

# Daemon mode for background operation
lattiam server start --daemon --port 8084
```

### Process Management

Use systemd or supervisor for production deployments:

```ini
# /etc/systemd/system/lattiam.service
[Unit]
Description=Lattiam Infrastructure API
After=network.target

[Service]
Type=simple
User=lattiam
Environment="LATTIAM_PORT=8084"
Environment="LATTIAM_STATE_STORE=aws"
ExecStart=/usr/local/bin/lattiam server start
Restart=always

[Install]
WantedBy=multi-user.target
```

### Health Monitoring

Monitor service health:

```bash
# Health check endpoint
curl http://localhost:8084/api/v1/system/health

# Queue metrics
curl http://localhost:8084/api/v1/queue/metrics
```

## CLI Commands

### Server Management

```bash
lattiam server start              # Start in foreground
lattiam server start --daemon     # Start in background
lattiam server stop               # Stop the server
lattiam server status             # Check if running
```

### Configuration Commands

```bash
lattiam config show               # Display current configuration
lattiam config validate           # Validate configuration
lattiam config paths              # Show file paths
```

## Environment Variables Reference

### Server Configuration

| Variable           | Default            | Description          |
| ------------------ | ------------------ | -------------------- |
| `LATTIAM_PORT`     | `8084`             | API server port      |
| `LATTIAM_DEBUG`    | `false`            | Enable debug logging |
| `LATTIAM_LOG_FILE` | (stdout)           | Log file path        |
| `LATTIAM_PID_FILE` | `/tmp/lattiam.pid` | PID file location    |

### Storage Configuration

| Variable               | Default                | Description                    |
| ---------------------- | ---------------------- | ------------------------------ |
| `LATTIAM_STATE_DIR`    | `~/.lattiam/state`     | State storage directory        |
| `LATTIAM_PROVIDER_DIR` | `~/.lattiam/providers` | Provider cache directory       |
| `LATTIAM_STATE_STORE`  | `file`                 | Backend type (`file` or `aws`) |

### AWS Backend Configuration

| Variable                        | Default | Description              |
| ------------------------------- | ------- | ------------------------ |
| `LATTIAM_AWS_S3_BUCKET`         | -       | S3 bucket for state      |
| `LATTIAM_AWS_S3_REGION`         | -       | S3 region                |
| `LATTIAM_AWS_S3_PREFIX`         | -       | S3 key prefix            |
| `LATTIAM_AWS_S3_ENDPOINT`       | -       | Custom S3 endpoint       |
| `LATTIAM_AWS_DYNAMODB_TABLE`    | -       | DynamoDB table for locks |
| `LATTIAM_AWS_DYNAMODB_REGION`   | -       | DynamoDB region          |
| `LATTIAM_AWS_DYNAMODB_ENDPOINT` | -       | Custom DynamoDB endpoint |

### Queue Configuration

| Variable             | Default    | Description                              |
| -------------------- | ---------- | ---------------------------------------- |
| `LATTIAM_QUEUE_TYPE` | `embedded` | Queue type (`embedded` or `distributed`) |
| `LATTIAM_REDIS_URL`  | -          | Redis URL for distributed queue          |

### Debug Configuration

| Variable            | Default              | Description         |
| ------------------- | -------------------- | ------------------- |
| `LATTIAM_DEBUG`     | `false`              | Enable debug mode   |
| `LATTIAM_DEBUG_DIR` | `/tmp/lattiam-debug` | Debug log directory |

### Safety Flags

| Variable                       | Default | Description                                |
| ------------------------------ | ------- | ------------------------------------------ |
| `LATTIAM_FORCE_BACKEND_REINIT` | `false` | Force backend reinitialization (DATA LOSS) |
