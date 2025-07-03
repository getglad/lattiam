# Data Sources

Lattiam supports Terraform data sources, allowing you to read information from providers before creating resources.

## Overview

Data sources let you fetch information from cloud providers that can be used in resource configuration. For example, you can look up the latest AMI ID, get availability zones, or fetch existing resource details.

## Usage

### Basic Data Source Example

```json
{
  "data": {
    "aws_region": {
      "current": {}
    },
    "aws_caller_identity": {
      "current": {}
    }
  },
  "resource": {
    "aws_s3_bucket": {
      "example": {
        "bucket": "my-bucket-${data.aws_region.current.name}-${data.aws_caller_identity.current.account_id}"
      }
    }
  }
}
```

### Data Source with Configuration

```json
{
  "data": {
    "aws_ami": {
      "ubuntu": {
        "most_recent": true,
        "filter": [
          {
            "name": "name",
            "values": [
              "ubuntu/images/hvm-ssd/ubuntu-focal-20.04-amd64-server-*"
            ]
          },
          {
            "name": "virtualization-type",
            "values": ["hvm"]
          }
        ],
        "owners": ["099720109477"]
      }
    }
  },
  "resource": {
    "aws_instance": {
      "web": {
        "ami": "${data.aws_ami.ubuntu.id}",
        "instance_type": "t3.micro"
      }
    }
  }
}
```

## Execution Order

Data sources are automatically executed before resources to ensure their values are available for interpolation. The execution flow is:

1. Parse Terraform JSON
2. Execute all data sources
3. Store data source results
4. Execute resources with interpolated values

## Interpolation

Data source values can be referenced using the pattern `${data.TYPE.NAME.ATTRIBUTE}`:

- `${data.aws_region.current.name}` - Current AWS region name
- `${data.aws_availability_zones.available.names[0]}` - First availability zone
- `${data.aws_ami.ubuntu.id}` - AMI ID from data source lookup

## Supported Providers

Data sources work with any Terraform provider that implements the `ReadDataSource` RPC. The `demo/` directory offers a walkthrough for evaluating data sources against AWS. Data sources for other providers (e.g., Azure, Google Cloud) may function but should be considered experimental.

## Error Handling

If a data source fails:

- The error is logged
- Interpolations referencing that data source remain unresolved
- The deployment can continue if the unresolved values aren't critical

## LocalStack Limitations

When testing with LocalStack:

- EC2-based data sources (like `aws_availability_zones`) require EC2 service to be enabled
- Use data sources that don't require EC2: `aws_region`, `aws_caller_identity`
- All data sources work correctly with real AWS credentials

## Examples

### Get Current AWS Account Info

```json
{
  "data": {
    "aws_caller_identity": {
      "current": {}
    }
  }
}
```

Response includes:

- `account_id` - AWS account ID
- `arn` - ARN of the caller
- `user_id` - Unique identifier of the caller

### List Availability Zones

```json
{
  "data": {
    "aws_availability_zones": {
      "available": {
        "state": "available"
      }
    }
  }
}
```

Response includes:

- `names` - List of availability zone names
- `zone_ids` - List of availability zone IDs

### Complex Example with Multiple Data Sources

```json
{
  "data": {
    "aws_region": {
      "current": {}
    },
    "aws_availability_zones": {
      "available": {
        "state": "available"
      }
    },
    "aws_ami": {
      "amazon_linux": {
        "most_recent": true,
        "owners": ["amazon"],
        "filter": [
          {
            "name": "name",
            "values": ["amzn2-ami-hvm-*-x86_64-gp2"]
          }
        ]
      }
    }
  },
  "resource": {
    "aws_instance": {
      "example": {
        "ami": "${data.aws_ami.amazon_linux.id}",
        "instance_type": "t3.micro",
        "availability_zone": "${data.aws_availability_zones.available.names[0]}",
        "tags": {
          "Name": "Example-${data.aws_region.current.name}"
        }
      }
    }
  }
}
```
