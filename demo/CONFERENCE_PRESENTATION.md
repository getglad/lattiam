# Lattiam Conference Presentation Guide

## The 30-Second Elevator Pitch

**"Lattiam is a REST API for Terraform. It allows you to programmatically manage your cloud infrastructure using standard Terraform JSON, without needing to write HCL or use the Terraform CLI. Think of it as 'Terraform as a Service' for your platform."**

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

## Setup (Before You Start)

```bash
# Terminal 1: Start the API server
# Build first (if not already done):
make build

# Configure Lattiam server with AWS credentials (e.g., via environment variables or AWS config file)
# Example: export AWS_PROFILE=developer
./build/lattiam server start

# Terminal 2: Verify it's running
curl http://localhost:8084/api/v1/health | jq

# Terminal 3: Keep a terminal ready for AWS validation commands
# Ensure this terminal also has AWS credentials configured if different from Lattiam server's
export AWS_PROFILE=developer
```

**IMPORTANT**: Keep Terminal 1 visible to show server logs during demos - the dependency analysis logs are impressive!

## The Main Demo Flow (20-25 minutes)

**Terminal Setup for Maximum Impact:**

- **Terminal 1**: Server logs (keep visible - shows impressive dependency analysis)
- **Terminal 2**: API commands (where you run curl commands)
- **Terminal 3**: AWS validation (proves we're creating REAL resources)
- **Optional Terminal 4**: `watch 'aws s3 ls | grep lattiam'` (shows resources appearing/disappearing in real-time)

### 1. Start with the Problem (2 minutes)

**"Who here has struggled with programmatic infrastructure management?"**

Traditional approaches often involve complex orchestration, managing credentials in CI/CD, and dealing with the Terraform CLI as a subprocess.

```bash
# Traditional approach - complex orchestration
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

# Wait for completion (should be quick)
sleep 5

# Check the deployment status
curl http://localhost:8084/api/v1/deployments/$DEPLOYMENT_ID | jq '.status'
# Should show "completed"

# See what was actually created
curl http://localhost:8084/api/v1/deployments/$DEPLOYMENT_ID | jq '.resources[] | select(.type == "aws_s3_bucket") | .state.bucket'
# Shows the generated bucket name

# VALIDATE: Use AWS CLI to prove the bucket exists!
BUCKET_NAME=$(curl -s http://localhost:8084/api/v1/deployments/$DEPLOYMENT_ID | jq -r '.resources[] | select(.type == "aws_s3_bucket") | .state.bucket')
echo "Created bucket name: $BUCKET_NAME"

# Switch to Terminal 3 to show it's REAL
aws s3 ls | grep $BUCKET_NAME
# ✓ Should show: 2025-06-28 19:02:20 lattiam-simple-bucket-xxxxxxxx
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

# Wait and check status
sleep 10
curl http://localhost:8084/api/v1/deployments/$MULTI_ID | jq '.status'

# Show all 4 resources were created successfully
curl http://localhost:8084/api/v1/deployments/$MULTI_ID | jq '.summary'
# Should show: total_resources: 4, successful_resources: 4

# VALIDATE: Check what was actually created in AWS
# Switch to Terminal 3 for AWS validation

# 1. Show the S3 buckets exist
aws s3 ls | grep lattiam-app
# ✓ Should show:
# 2025-06-28 19:03:24 lattiam-app-data-xxxxxxxx
# 2025-06-28 19:03:26 lattiam-app-logs-xxxxxxxx

# 2. Show the IAM role exists
ROLE_NAME=$(curl -s http://localhost:8084/api/v1/deployments/$MULTI_ID | jq -r '.resources[] | select(.type == "aws_iam_role") | .state.name')
aws iam get-role --role-name $ROLE_NAME --query 'Role.RoleName' --output text
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
cat demo/terraform-json/05-function-showcase.json | jq '.terraform_json.resource.random_string.uuid_example.keepers'

# Deploy it and save ID automatically
FUNC_ID=$(curl -s -X POST http://localhost:8084/api/v1/deployments \
  -H "Content-Type: application/json" \
  -d @demo/terraform-json/05-function-showcase.json | jq -r '.id')
echo "Created function showcase deployment: $FUNC_ID"

# Wait for completion
sleep 5

# Show the evaluated functions - UUID example
curl http://localhost:8084/api/v1/deployments/$FUNC_ID | \
  jq '.resources[] | select(.name == "uuid_example") | .state.keepers'

# Show timestamp and formatting functions
curl http://localhost:8084/api/v1/deployments/$FUNC_ID | \
  jq '.resources[] | select(.name == "timestamp_example") | .state.keepers'

# Show string manipulation functions
curl http://localhost:8084/api/v1/deployments/$FUNC_ID | \
  jq '.resources[] | select(.name == "string_functions") | .state.keepers'
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
cat demo/terraform-json/06-data-source-demo.json | jq '.terraform_json.data'

# Deploy with data sources
DATA_ID=$(curl -s -X POST http://localhost:8084/api/v1/deployments \
  -H "Content-Type: application/json" \
  -d @demo/terraform-json/06-data-source-demo.json | jq -r '.id')
echo "Created data source deployment: $DATA_ID"

# Wait for completion
sleep 8

# Show how data sources were queried and used
curl http://localhost:8084/api/v1/deployments/$DATA_ID | \
  jq '.resources[] | select(.type == "aws_s3_bucket") | .state.tags'

# Show the resolved data from AWS
curl http://localhost:8084/api/v1/deployments/$DATA_ID | \
  jq '.resources[] | select(.type == "aws_s3_bucket") | .state.bucket'
```

**What you'll see**:

- Data sources queried real AWS account info (`aws_caller_identity`, `aws_region`)
- Results interpolated into resource configuration
- S3 bucket created with region and account ID in tags
- Bucket name includes the actual AWS region

**Key Point**: "Data sources execute first, then resources use their results - full dependency resolution"

### 7. Lifecycle Management - Update vs Replace (3 minutes)

**"Let's update our S3 bucket - add some tags"**

```bash
# In-place update (just adding tags)
curl -X PUT http://localhost:8084/api/v1/deployments/$DEPLOYMENT_ID \
  -H "Content-Type: application/json" \
  -d @demo/terraform-json/03-s3-bucket-update-tags.json | jq

# Wait for update to complete
sleep 5

# Check the tags were added
curl http://localhost:8084/api/v1/deployments/$DEPLOYMENT_ID | \
  jq '.resources[] | select(.type == "aws_s3_bucket") | .state.tags'

# VALIDATE: Use AWS CLI to confirm tags
# Switch to Terminal 3
aws s3api get-bucket-tagging --bucket $BUCKET_NAME | jq
# ✓ Should show: {"TagSet": [{"Key": "Environment", "Value": "demo"}, ...]}
```

**"Now let's trigger a replacement by changing the bucket name"**

```bash
# Save the old bucket name
OLD_BUCKET=$BUCKET_NAME

# This will destroy and recreate
curl -X PUT http://localhost:8084/api/v1/deployments/$DEPLOYMENT_ID \
  -H "Content-Type: application/json" \
  -d @demo/terraform-json/04-s3-bucket-rename.json | jq

# Wait for replacement
sleep 10

# Show it was replaced with new name
NEW_BUCKET=$(curl -s http://localhost:8084/api/v1/deployments/$DEPLOYMENT_ID | \
  jq -r '.resources[] | select(.type == "aws_s3_bucket") | .state.bucket')
echo "Old bucket: $OLD_BUCKET"
echo "New bucket: $NEW_BUCKET"

# VALIDATE: Old bucket should be gone, new bucket should exist
# Switch to Terminal 3
aws s3 ls | grep lattiam-simple
# ✓ Should only show the NEW bucket name, old one is gone
```

**Key Point**: "Lattiam uses Terraform's planning to detect what needs replacing - no manual tracking needed"

### 6. Clean Up (1 minute)

**"And of course, full lifecycle includes deletion"**

```bash
# Delete in reverse dependency order
curl -X DELETE http://localhost:8084/api/v1/deployments/$DATA_ID
curl -X DELETE http://localhost:8084/api/v1/deployments/$MULTI_ID
curl -X DELETE http://localhost:8084/api/v1/deployments/$FUNC_ID
curl -X DELETE http://localhost:8084/api/v1/deployments/$DEPLOYMENT_ID

# VALIDATE: Confirm resources are gone
# Switch to Terminal 3
aws s3 ls | grep lattiam
# ✓ Should return nothing - all buckets deleted

aws iam list-roles | jq '.Roles[].RoleName' | grep lattiam
# ✓ Should return nothing - all roles deleted
```

### 7. The Technical Deep Dive (2 minutes)

**Show the architecture slide or draw it:**

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

**Key Technical Points**:

- NO external Terraform CLI dependency
- NO temporary HCL files written to disk
- YES direct use of Terraform's Go libraries
- YES full provider ecosystem compatibility

## Q&A Talking Points

**"Why not just use Terraform Cloud API?"**

- Different problem space: TC manages workspaces/runs, Lattiam provides direct resource API
- Lattiam is about using Terraform as a library, not as a service (like TC)
- Can actually use both together!

**"What about Pulumi/CDK?"**

- They generate Terraform JSON too, but then shell out to CLI
- Lattiam skips the CLI entirely - direct library integration

**"Is this production ready?"**

- Show state management: `ls ~/.lattiam/state/deployments/`
- Show error handling: try to create a resource with bad config
- Mention: atomic state updates, proper locking, graceful shutdown

**"What providers work?"**

- All of them! Lattiam uses the standard Terraform provider protocol
- Show provider downloads: `ls ~/.lattiam/providers/`

**"How does Lattiam handle credentials?"**

- Lattiam acts as a secure execution boundary.
- Client applications never handle cloud credentials; they only talk to the Lattiam API.
- Lattiam itself is configured with the necessary cloud provider credentials (e.g., via environment variables, IAM roles).
- This centralizes credential management and reduces the attack surface.

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

## Common Gotchas to Avoid

1. **Don't dive too deep into implementation** - focus on the value prop
2. **Don't compare to Terraform directly** - we USE Terraform, not replace it
3. **Have deployment IDs ready** - copy them immediately after creation
4. **Test ALL demos before presentation** - especially functions and EC2
5. **Show AWS validation frequently** - proves it's not just mock data
6. **Keep server logs visible** - the dependency analysis output is impressive
7. **If functions demo fails, acknowledge it as "coming soon" and move on**

That's it. Focus on the working demos, test the complex ones beforehand.
