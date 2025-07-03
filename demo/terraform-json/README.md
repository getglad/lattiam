# Demo Terraform JSON Files

These are the essential demo files for presenting Lattiam. Each file is a valid Terraform JSON configuration.

## Core Demo Files

### 1. `01-s3-bucket-simple.json`

- Creates an S3 bucket with a random suffix
- Shows multi-provider usage (AWS + Random)
- Good starting point for any demo

### 2. `02-multi-resource-dependencies.json` ⭐ THE KEY DEMO

- 4 resources with complex interdependencies
- Random strings → S3 buckets → IAM role → IAM policy → Role attachment
- Shows sophisticated dependency resolution with DAG algorithms
- This is your "wow factor" demo

### 3. `03-s3-bucket-update-tags.json`

- Updates the S3 bucket from demo 1 by adding tags
- Shows in-place update (no replacement)
- Use with PUT request to same deployment ID

### 4. `04-s3-bucket-rename.json`

- Changes the S3 bucket name from demo 1
- Forces replacement (destroy + recreate)
- Shows Lattiam's smart update vs replace detection

### 5. `05-function-showcase.json`

- Demonstrates Terraform function support
- Uses: uuid(), timestamp(), upper(), format(), base64encode(), max(), min()
- Shows that all 80+ Terraform functions work

### 6. `06-data-source-demo.json` (Optional)

- Shows data source support
- Reads AWS region and caller identity
- Resources reference data source values

## Usage

All files follow the same structure:

```json
{
  "name": "deployment-name",
  "terraform_json": {
    // Standard Terraform JSON configuration
  }
}
```

Create a deployment:

```bash
curl -X POST http://localhost:8084/api/v1/deployments \
  -H "Content-Type: application/json" \
  -d @01-s3-bucket-simple.json
```

## Important Notes

- Use exact provider versions (e.g., "3.6.0"), not constraints (e.g., "~> 3.6")
- All interpolations use standard Terraform syntax: `${resource.type.name.attribute}`
- These files work with real AWS or LocalStack
- The demo flow is: 1 → 2 → 5 → 3 → 4 (simple → complex → functions → update → replace)
