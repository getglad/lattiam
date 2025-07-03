# Lattiam Demo & Presentation Materials

This directory contains everything you need to present Lattiam at a conference or demo.

## Quick Start

```bash
# 1. Start the server
../build/lattiam server start

# 2. Run the automated demo
./QUICK_DEMO.sh
```

## What's Here

### Presentation Materials

- **`CONFERENCE_PRESENTATION.md`** - Complete conference presentation guide with speaker notes
- **`QUICK_DEMO.sh`** - Automated 15-minute demo script that runs all examples
- **`SLIDE_NOTES.md`** - Slide deck outline with code examples and talking points

### Demo Files (terraform-json/)

Only the essential files for a focused demo:

1. **`01-s3-bucket-simple.json`** - Simple S3 bucket creation showing multi-provider
2. **`02-multi-resource-dependencies.json`** - Dependency resolution demo (the "wow" factor)
3. **`03-s3-bucket-update-tags.json`** - In-place update example
4. **`04-s3-bucket-rename.json`** - Replacement trigger example
5. **`05-function-showcase.json`** - Terraform functions demo
6. **`06-data-source-demo.json`** - Data source example (optional)

## Presentation Flow

The QUICK_DEMO.sh script follows this flow:

1. **Simple Start** - S3 bucket with random suffix
2. **Dependency Magic** - 4 resources with complex dependencies
3. **Functions** - Show uuid(), timestamp(), etc.
4. **Updates** - Tags (update) vs name (replacement)
5. **Cleanup** - Proper deletion

Total time: ~15 minutes

## Tips

- Run QUICK_DEMO.sh once before your presentation to ensure everything works
- Keep the server logs visible in a separate terminal - the dependency analysis logs are impressive
- The whole story only needs 6 JSON files - we've removed all the clutter
- If asked about other features, check the `/archive` folder for additional examples

## The Core Message

"Lattiam uses Terraform as a library, not as a CLI. We call `terraform.Context.Plan()` directly, giving you all of Terraform's power through a clean REST API."
