# Lattiam Demo & Presentation Materials

This directory contains everything you need to present Lattiam at a conference or demo.

## Quick Start

```bash
# Start the server
../build/lattiam server start

# Then follow the demo flow in CONFERENCE_PRESENTATION.md
```

## What's Here

### Presentation Materials

- **`CONFERENCE_PRESENTATION.md`** - Complete conference presentation guide with speaker notes and demo commands

### Demo Files (terraform-json/)

Only the essential files for a focused demo:

1. **`01-s3-bucket-simple.json`** - Simple S3 bucket creation showing multi-provider
2. **`02-multi-resource-dependencies.json`** - Dependency resolution demo (the "wow" factor)
3. **`03-function-showcase.json`** - Terraform functions demo
4. **`04-data-source-demo.json`** - Data source example
5. **`05a-s3-bucket-update-tags.json`** - In-place update example
6. **`05b-s3-bucket-rename.json`** - Replacement trigger example
7. **`06-ec2-complex-dependencies.json`** - Complex EC2 infrastructure

## Presentation Flow

The recommended demo flow:

1. **Simple Start** - S3 bucket with random suffix
2. **Dependency Magic** - 4 resources with complex dependencies
3. **Functions** - Show uuid(), timestamp(), etc.
4. **Updates** - Tags (update) vs name (replacement)
5. **Cleanup** - Proper deletion

Total time: ~15 minutes

## Tips

- Run through the demos once before your presentation to ensure everything works
- Keep the server logs visible in a separate terminal - the dependency analysis logs are impressive
- The whole story only needs 6 JSON files - we've removed all the clutter
- Follow the detailed commands in CONFERENCE_PRESENTATION.md for best results

## API & AWS CLI Notes

- Use `/api/v1/deployments/{id}` endpoint for accessing `.resources[]` data (not the list endpoint)
- AWS CLI: Use `AWS_PROFILE=developer` for all commands
- EC2 commands need explicit `--region us-east-1` flag, S3/IAM commands don't

## The Core Message

"Lattiam communicates directly with Terraform providers via gRPC, bypassing the need for the Terraform CLI. This gives you all of Terraform's power through a clean REST API."
