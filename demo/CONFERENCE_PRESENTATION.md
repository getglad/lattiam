# Lattiam Conference Presentation Guide

## The 30-Second Elevator Pitch

**"Lattiam turns Terraform into an API service for platform teams. It uses Terraform's Go libraries directly to provide a REST API for managing cloud infrastructure. You get all of Terraform's capabilities - providers, planning, state management - as a service your platform can call. Perfect for building internal developer platforms."**

```
  +---------------------------------+
  |    Platform Team's Tooling      |
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

## Setup (Before You Start)

```bash
# Terminal 1: Start the API server
# Build first (if not already done):
make build

# Start the server (it reads AWS profile from the Terraform JSON configs)
# No need to set AWS_PROFILE for the server itself
./build/lattiam server start

# Terminal 2: For running curl commands and demos

# Terminal 3: Keep a terminal ready for AWS validation commands
# This terminal needs AWS credentials for validation
export AWS_PROFILE=developer
```

**IMPORTANT**: Keep Terminal 1 visible to show server logs during demos - the dependency analysis logs are impressive!

## API Endpoints & AWS CLI Notes

**API Endpoint Usage:**

- List deployments: `GET /api/v1/deployments` (returns summary view)
- Get deployment details: `GET /api/v1/deployments/{id}` (returns full resource details)
- Use the specific deployment endpoint when accessing `.resources[]` data

**Helpful jq Patterns:**

```bash
# Extract deployment ID from creation response
curl -s -X POST ... | jq -r '.id'

# Get deployment status
curl -s .../deployments/$ID | jq '.status'

# Get specific resource by type
curl -s .../deployments/$ID | jq '.resources[] | select(.type == "aws_s3_bucket")'

# Get resource state attribute
curl -s .../deployments/$ID | jq -r '.resources[] | select(.type == "aws_s3_bucket") | .state.bucket'

# Count resources
curl -s .../deployments/$ID | jq '.resources | length'

# Show resource summary
curl -s .../deployments/$ID | jq '.resources | map({name, type, status})'

# Extract dangerous changes
curl -s -X PUT ... | jq '.dangerous_changes'
```

**AWS CLI Requirements:**

- S3 and IAM commands: Work without explicit region (`AWS_PROFILE=developer aws s3 ls`)
- EC2 commands: Require explicit `--region us-east-1` flag
- Always use `AWS_PROFILE=developer` for all validation commands

## The Main Demo Flow (25-30 minutes)

**Demo File Quick Reference:**
- Demo 1: `01-s3-bucket-simple.json` - Simple S3 bucket
- Demo 2: `02-multi-resource-dependencies.json` - Multiple resources with dependencies
- Demo 3: `03-function-showcase.json` - Terraform functions (6 examples)
- Demo 4: `04-data-source-demo.json` - Data sources
- Demo 5: `05a-s3-bucket-update-tags.json` (create) → `05b-s3-bucket-rename.json` (replace)
- Demo 6: `06-ec2-complex-dependencies.json` - Complex EC2 infrastructure

**Terminal Setup for Maximum Impact:**

- **Terminal 1**: Server logs (keep visible - shows impressive dependency analysis)
- **Terminal 2**: API commands (where you run curl commands)
- **Terminal 3**: AWS validation (proves we're creating REAL resources)
- **Optional Terminal 4**: `watch 'aws s3 ls | grep lattiam'` (shows resources appearing/disappearing in real-time)

### 1. Start with the Problem (2 minutes)

**"Who here has struggled with programmatic infrastructure management?"**

Common approaches often involve complex orchestration, managing credentials in CI/CD, and dealing with the Terraform CLI as a subprocess.

```bash
# Common approach - complex orchestration
output=$(terraform plan -out=plan.tfplan 2>&1)
terraform apply plan.tfplan
# Parse output, handle errors, manage files...

# Lattiam approach - clean API
curl -X POST http://localhost:8084/api/v1/deployments \
  -H "Content-Type: application/json" \
  -d @demo.json
```

**Key Point**: "Lattiam simplifies programmatic infrastructure by using Terraform's libraries directly, providing a clean API boundary."

### 2. Simple Demo - Build Confidence (3 minutes)

**"Let's start with something simple - creating an S3 bucket via API"**

```bash
# Show the JSON (it's just standard Terraform JSON format)
cat demo/terraform-json/01-s3-bucket-simple.json

# Create the deployment and save the ID automatically
DEPLOYMENT_ID=$(curl -s -X POST http://localhost:8084/api/v1/deployments \
  -H "Content-Type: application/json" \
  -d @demo/terraform-json/01-s3-bucket-simple.json | jq -r '.id')
echo "Created deployment: $DEPLOYMENT_ID"

# Check the deployment status
curl -s http://localhost:8084/api/v1/deployments/$DEPLOYMENT_ID | jq '.status'
# Should show "completed"

# See what was actually created
BUCKET_NAME=$(curl -s http://localhost:8084/api/v1/deployments/$DEPLOYMENT_ID | jq -r '.resources[] | select(.type == "aws_s3_bucket") | .state.bucket')
echo "Generated bucket name: $BUCKET_NAME"

# VALIDATE: Check server logs (Terminal 1) for the deployment flow
# Look for: "State transition: aws_s3_bucket/demo [Applying → Completed]"

# VALIDATE: Use AWS CLI to prove the bucket exists!
# Switch to Terminal 3 to show it's REAL
# Note: S3 commands work without explicit region
AWS_PROFILE=developer aws s3 ls | grep "$BUCKET_NAME"
# ✓ Should show: 2025-07-05 23:50:26 lattiam-simple-bucket-xxxxxxxx
```

**What to Emphasize**:

- Standard Terraform JSON format (no custom YAML)
- Lattiam handles provider credentials (not in the JSON)
- Real AWS resources created (show with AWS CLI)
- Returns deployment ID for lifecycle management

### 3. The Crown Jewel - Dependency Analysis (5 minutes)

**"Now let me show you the real magic - sophisticated dependency resolution"**

First, show them what we're deploying:

```bash
# Quick look at the structure
cat demo/terraform-json/02-multi-resource-dependencies.json | jq '.terraform_json.resource | keys'
```

**"This has 4 resources with complex interdependencies. Watch what happens..."**

```bash
# In Terminal 2 (keep Terminal 1 showing server logs)
# Create multi-resource deployment and save ID automatically
MULTI_ID=$(curl -s -X POST http://localhost:8084/api/v1/deployments \
  -H "Content-Type: application/json" \
  -d @demo/terraform-json/02-multi-resource-dependencies.json | jq -r '.id')
echo "Created multi-resource deployment: $MULTI_ID"

# Check deployment status
curl -s http://localhost:8084/api/v1/deployments/$MULTI_ID | jq '.status'

# Show all 4 resources were created successfully
curl -s http://localhost:8084/api/v1/deployments/$MULTI_ID | jq '.summary'
# Should show: total_resources: 4, successful_resources: 4

# VALIDATE: Check server logs (Terminal 1) for dependency resolution
# Look for patterns like:
# - "State transition: random_string/stack_suffix [Applying → Completed]"
# - "State transition: aws_s3_bucket/app_data [WaitingForDependencies → ResolvingDependencies]"
# - "Dependencies now available" messages showing the dependency chain

# VALIDATE: Check what was actually created in AWS
# Switch to Terminal 3 for AWS validation

# 1. Show the S3 buckets exist
AWS_PROFILE=developer aws s3 ls | grep lattiam-app
# ✓ Should show:
# 2025-06-28 19:03:24 lattiam-app-data-xxxxxxxx
# 2025-06-28 19:03:26 lattiam-app-logs-xxxxxxxx

# 2. Show the IAM role exists
ROLE_NAME=$(curl -s http://localhost:8084/api/v1/deployments/$MULTI_ID | jq -r '.resources[] | select(.type == "aws_iam_role") | .state.name')
AWS_PROFILE=developer aws iam get-role --role-name $ROLE_NAME --query 'Role.RoleName' --output text
# ✓ Should show: lattiam-app-role-xxxxxxxx
```

**Point to the server logs** showing:

- `Starting sophisticated JSON-based dependency analysis for 4 resources`
- `Found dependency: random_string.X -> resource.Y`
- `Successfully analyzed dependencies using sophisticated DAG`

**Key Points**:

- Zero disk I/O - pure in-memory analysis
- DAG algorithms (using dominikbraun/graph library)
- Automatic topological sorting
- Resources created in correct dependency order
- No HCL files, no terraform init, no subprocess calls

### 4. Show Terraform Functions Working (3 minutes)

**"Lattiam supports a wide range of Terraform functions"**

```bash
# Show a snippet of functions being used
cat demo/terraform-json/03-function-showcase.json | jq '.terraform_json.resource.random_string.uuid_example.keepers'

# Deploy it and save ID automatically
FUNC_ID=$(curl -s -X POST http://localhost:8084/api/v1/deployments \
  -H "Content-Type: application/json" \
  -d @demo/terraform-json/03-function-showcase.json | jq -r '.id')
echo "Created function showcase deployment: $FUNC_ID"

# Show the evaluated functions - UUID example
curl -s http://localhost:8084/api/v1/deployments/$FUNC_ID | jq '.resources[] | select(.name == "uuid_example") | .state.keepers'

# Show timestamp and formatting functions
curl -s http://localhost:8084/api/v1/deployments/$FUNC_ID | jq '.resources[] | select(.name == "timestamp_example") | .state.keepers'

# Show string manipulation functions
curl -s http://localhost:8084/api/v1/deployments/$FUNC_ID | jq '.resources[] | select(.name == "string_functions") | .state.keepers'

# VALIDATE: Check server logs (Terminal 1) for function evaluation
# Look for: "State transition: random_string/uuid_example [Applying → Completed]"
# The functions are evaluated during the Planning phase before Apply
```

**What you'll see**:

- `uuid()` generated a real UUID (e.g., "a1b2c3d4-e5f6-7890-abcd-ef1234567890")
- `timestamp()` shows current time (e.g., "2025-06-26T03:45:00Z")
- `upper("lattiam rocks")` returns "LATTIAM ROCKS"
- `format()`, `base64encode()`, etc. all work

**Key Point**: "Functions are evaluated in Lattiam before sending to providers - just like Terraform does"

### 5. Data Sources - Dynamic Infrastructure Discovery (3 minutes)

**"Lattiam also supports Terraform data sources for runtime infrastructure discovery"**

```bash
# Show the data source configuration
cat demo/terraform-json/04-data-source-demo.json | jq '.terraform_json.data'

# Deploy with data sources
DATA_ID=$(curl -s -X POST http://localhost:8084/api/v1/deployments \
  -H "Content-Type: application/json" \
  -d @demo/terraform-json/04-data-source-demo.json | jq -r '.id')
echo "Created data source deployment: $DATA_ID"

# Show how data sources were queried and used
curl -s http://localhost:8084/api/v1/deployments/$DATA_ID | jq '.resources[] | select(.type == "aws_s3_bucket") | .state.tags'
# ✓ Should show: {"Account": "123456789012", "Region": "us-east-1", "ManagedBy": "lattiam"}

# Show the resolved data from AWS
curl -s http://localhost:8084/api/v1/deployments/$DATA_ID | jq '.resources[] | select(.type == "aws_s3_bucket") | .state.bucket'
# ✓ Should show: "lattiam-ds-demo-us-east-1-xxxxxxxx"

# VALIDATE: Check the S3 bucket was created with data source values
AWS_PROFILE=developer aws s3 ls | grep lattiam-ds-demo
# ✓ Should show: 2025-06-28 19:04:15 lattiam-ds-demo-us-east-1-xxxxxxxx

# The bucket name proves data sources worked:
# - "us-east-1" came from data.aws_region.current
# - The bucket exists, proving data.aws_caller_identity.current provided valid credentials
```

**What you'll see**:

- Data sources queried real AWS account info (`aws_caller_identity`, `aws_region`)
- Results interpolated into resource configuration
- S3 bucket created with region and account ID in tags
- Bucket name includes the actual AWS region (e.g., "lattiam-ds-demo-us-east-1-52r0ootg")
- AWS CLI confirms the bucket exists with the dynamically generated name

**Key Point**: "Data sources execute first, then resources use their results - full dependency resolution"

### 6. Lifecycle Management - Update vs Replace (5 minutes)

**"Let's demonstrate Lattiam's update capabilities and safety mechanisms"**

#### Part A: In-Place Updates (Working) ✅

```bash
# Create initial deployment for lifecycle demo
LIFECYCLE_ID=$(curl -s -X POST http://localhost:8084/api/v1/deployments \
  -H "Content-Type: application/json" \
  -d @demo/terraform-json/05a-s3-bucket-update-tags.json | jq -r '.id')
echo "Created lifecycle demo deployment: $LIFECYCLE_ID"

# Wait for completion and get bucket name
sleep 3
BUCKET_NAME=$(curl -s http://localhost:8084/api/v1/deployments/$LIFECYCLE_ID | jq -r '.resources[] | select(.type == "aws_s3_bucket") | .state.bucket')
echo "Bucket created: $BUCKET_NAME"

# Check the tags in initial state
curl -s http://localhost:8084/api/v1/deployments/$LIFECYCLE_ID | jq '.resources[] | select(.type == "aws_s3_bucket") | .state.tags'
# ✓ Should show: {"Environment": "demo", "ManagedBy": "lattiam", "Updated": "true"}

# VALIDATE: Confirm tags were actually applied in AWS
# Switch to Terminal 3
AWS_PROFILE=developer aws s3api get-bucket-tagging --bucket "$BUCKET_NAME" | jq '.TagSet'
# ✓ Should show the tags are present
```

**Key Point**: "Tags can be updated in-place without destroying the resource"

#### Part B: Resource Replacement with Safety Checks

```bash
# Save the old bucket name for comparison
OLD_BUCKET=$BUCKET_NAME
echo "Current bucket name: $OLD_BUCKET"

# This will attempt replacement (renaming bucket requires destroy/create)
curl -X PUT http://localhost:8084/api/v1/deployments/$LIFECYCLE_ID \
  -H "Content-Type: application/json" \
  -d @demo/terraform-json/05b-s3-bucket-rename.json | jq '.error, .message'

# ✓ Should show:
# "dangerous_changes_detected"
# "Update contains potentially dangerous changes that could cause data loss or downtime"

# Show the detailed plan with dangerous changes
curl -X PUT http://localhost:8084/api/v1/deployments/$LIFECYCLE_ID \
  -H "Content-Type: application/json" \
  -d @demo/terraform-json/05b-s3-bucket-rename.json | jq '.dangerous_changes'

# ✓ Shows 2 resources will be replaced (random_string and aws_s3_bucket)

# Force the update with explicit approval
curl -X PUT "http://localhost:8084/api/v1/deployments/$LIFECYCLE_ID?force_update=true" \
  -H "Content-Type: application/json" \
  -d @demo/terraform-json/05b-s3-bucket-rename.json | jq '.status'

# Wait and check the new state
sleep 5
NEW_BUCKET=$(curl -s http://localhost:8084/api/v1/deployments/$LIFECYCLE_ID | jq -r '.resources[] | select(.type == "aws_s3_bucket") | .state.bucket')
echo "New bucket name: $NEW_BUCKET"

# VALIDATE: Old bucket should be gone, new bucket should exist
# Switch to Terminal 3
AWS_PROFILE=developer aws s3 ls | grep "lattiam-renamed-bucket"
# ✓ Should show the new bucket with renamed prefix

# VALIDATE: Original bucket should be deleted
AWS_PROFILE=developer aws s3 ls | grep "$OLD_BUCKET" || echo "Old bucket successfully deleted"
# ✓ Should show old bucket is gone

# VALIDATE: Show replacement in server logs (Terminal 1)
# Look for: "Destroying resource aws_s3_bucket/demo" followed by "Creating resource aws_s3_bucket/demo"
```

**Key Points**: 
- "Lattiam detects dangerous changes and requires explicit approval"
- "Uses Terraform's planning logic to identify replacements"
- "Provides detailed information about what will be destroyed and recreated"
- "Same resource lifecycle management as Terraform, with added safety checks"

#### Technical Deep Dive (Show in server logs)

Point to Terminal 1 showing:

- `"Updating existing resource aws_s3_bucket/demo"` (for tag updates)
- `"Destroying resource aws_s3_bucket/demo"` (for replacements)
- `"Creating resource aws_s3_bucket/demo"` (for replacements)

**What This Demonstrates**:

- ✅ **Update Detection**: Lattiam properly identifies existing resources
- ✅ **In-Place Updates**: Successfully applies changes that don't require replacement (tags, policies)
- ✅ **Resource Replacement**: Automatically handles destroy/create cycles when required

**Audience Takeaway**: "Lattiam provides the same resource lifecycle management as Terraform - leveraging Terraform's provider intelligence to handle both in-place updates and replacements seamlessly via API."

**Bonus - State Persistence**: "You can restart the server and all deployment states persist - try it! Stop the server (Ctrl-C), restart it, and query any deployment ID. All resource details are preserved."

### 7. Complex Infrastructure - EC2 Stack (5 minutes)

**"Now let me show you Lattiam handling truly complex infrastructure"**

```bash
# Show what we're about to deploy
cat demo/terraform-json/06-ec2-complex-dependencies.json | jq '{
  name: .name,
  resource_count: (.terraform_json.resource | length),
  resources: (.terraform_json.resource | keys)
}'

# This creates 13 resources with complex dependencies
# Deploy the EC2 stack and save ID automatically
EC2_ID=$(curl -s -X POST http://localhost:8084/api/v1/deployments \
  -H "Content-Type: application/json" \
  -d @demo/terraform-json/06-ec2-complex-dependencies.json | jq -r '.id')
echo "Created EC2 deployment: $EC2_ID"

# Watch the deployment progress (keep Terminal 1 visible for dependency analysis logs)
echo "Deploying VPC, subnets, security groups, IAM role, and EC2 instance..."
sleep 10

# Check deployment status
curl -s http://localhost:8084/api/v1/deployments/$EC2_ID | jq '.status'
# Should show: "completed" or "applying"

# Show the complexity - 13 resources created in proper order
curl -s http://localhost:8084/api/v1/deployments/$EC2_ID | jq '.summary'
# Should show: total_resources: 16 (includes 3 data sources)

# VALIDATE: Check server logs (Terminal 1) for complex dependency resolution
# Look for the cascade of state transitions showing proper ordering:
# - "State transition: aws_vpc/main [Applying → Completed]"
# - "State transition: aws_subnet/main [WaitingForDependencies → ResolvingDependencies]"
# - "State transition: aws_security_group/web_server [WaitingForDependencies → ResolvingDependencies]"
# - "State transition: aws_instance/web_server [WaitingForDependencies → ResolvingDependencies]"
# Shows how Lattiam respects the dependency graph

# VALIDATE: Check key infrastructure components in AWS
# Switch to Terminal 3

# 1. VPC was created
VPC_ID=$(curl -s http://localhost:8084/api/v1/deployments/$EC2_ID | jq -r '.resources[] | select(.type == "aws_vpc") | .state.id')
# Note: EC2 commands require explicit region specification
AWS_PROFILE=developer aws ec2 describe-vpcs --vpc-ids $VPC_ID --query 'Vpcs[0].VpcId' --output text --region us-east-1
# ✓ Should show the VPC ID

# 2. EC2 instance was created
INSTANCE_ID=$(curl -s http://localhost:8084/api/v1/deployments/$EC2_ID | jq -r '.resources[] | select(.type == "aws_instance") | .state.id')
AWS_PROFILE=developer aws ec2 describe-instances --instance-ids $INSTANCE_ID \
  --query 'Reservations[0].Instances[0].State.Name' --output text --region us-east-1
# ✓ Should show: "running" or "pending"
```

**Point to Terminal 1 logs showing**:

- "Starting sophisticated JSON-based dependency analysis for 13 resources"
- Resources created in dependency order: VPC → Subnet → Security Group → Instance
- Zero conflicts, automatic topological sorting

**Key Points**:

- Handles complex, real-world infrastructure
- 13 interdependent resources: networking, security, compute, IAM
- Proves Lattiam can manage production-grade deployments
- All through a simple REST API call

### 8. Clean Up (1 minute)

**"And of course, full lifecycle includes deletion"**

```bash
# Delete all deployments - Lattiam handles dependency order automatically
curl -X DELETE http://localhost:8084/api/v1/deployments/$EC2_ID
curl -X DELETE http://localhost:8084/api/v1/deployments/$DATA_ID
curl -X DELETE http://localhost:8084/api/v1/deployments/$FUNC_ID
curl -X DELETE http://localhost:8084/api/v1/deployments/$MULTI_ID
curl -X DELETE http://localhost:8084/api/v1/deployments/$DEPLOYMENT_ID

# VALIDATE: Confirm resources are gone
# Switch to Terminal 3
AWS_PROFILE=developer aws s3 ls | grep lattiam
# ✓ Should return nothing - all buckets deleted

AWS_PROFILE=developer aws iam list-roles | jq '.Roles[].RoleName' | grep lattiam
# ✓ Should return nothing - all roles deleted
```

### 9. The Technical Deep Dive (2 minutes)

**Show the architecture slide or draw it:**

```
  +---------------------------------+
  |    Platform Team's Tooling      |
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

**Key Technical Points**:

- Uses Terraform's Go libraries directly (no CLI subprocess)
- Same providers, same planning logic, same state format
- Standard Terraform JSON configuration format
- Full compatibility with Terraform's provider ecosystem

## Q&A Talking Points

**"Why build this instead of using existing tools?"**

- Direct API control over infrastructure deployment
- Self-hosted, runs in your infrastructure
- Simple REST API without workspace complexity

**"What about Pulumi/CDK?"**

- They generate Terraform JSON too, but custom code and then shell out to CLI
- Lattiam skips the CLI entirely and is intended to accept Terraform JSON as an API and use providers

**"How close is this to Terraform?"**

- Show state management: `ls ~/.lattiam/state/deployments/`
- Show provider downloads: `ls ~/.lattiam/providers/`
- Mention: atomic state updates, proper locking, graceful shutdown

**"How does Lattiam handle credentials?"**

- Lattiam acts as a secure execution boundary.
- Client applications never handle cloud credentials; they only talk to the Lattiam API.
- Credentials are managed according to the provider
- Creates a path to centralize credential management to the Lattiam servers and reduce the attack surface.

## If You Have Extra Time

### Show the Actual Code

```go
// This is what we're doing instead of subprocess
ctx := terraform.NewContext(...)
plan, diags := ctx.Plan()
```

## Backup Slides

Keep these ready:

1. Architecture diagram showing no external CLI dependency
2. Performance comparison (subprocess vs direct calls)
3. List of supported Terraform functions
4. State file example showing it's real Terraform state
5. Slide on Lattiam's security model (secure execution boundary, credential centralization)
