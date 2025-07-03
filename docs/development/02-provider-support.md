# Supported Terraform Providers

## Overview

Lattiam implements a provider gating mechanism to ensure only tested and verified Terraform providers can be automatically downloaded and used. This prevents unexpected behavior from untested providers while maintaining security and reliability.

## How Provider Gating Works

> **Note for POC Phase**: The provider support mechanism is currently statically defined in the codebase to simplify the proof-of-concept implementation. This approach allows for rapid development and testing with a known set of providers. In future releases, this will be refactored to support dynamic provider discovery and registration, enabling users to add custom providers without modifying the core codebase.

### Download Control

The provider download mechanism is controlled by a whitelist in `/pkg/provider/protocol/download_provider.go`:

```go
func (pm *ProviderManager) getProviderDownloadInfo(name, version string) *DownloadInfo {
    switch name {
    case "null":
        return GetNullProviderURL()
    case "aws":
        return GetAWSProviderURL(version)
    case "azurerm":
        return GetAzureProviderURL(version)
    case "google":
        return GetGCPProviderURL(version)
    case "kubernetes":
        return GetKubernetesProviderURL(version)
    case "random":
        return GetRandomProviderURL(version)
    default:
        return nil  // Provider not supported for automatic download
    }
}
```

When a provider is not in this list:

1. Automatic download is blocked
2. Error message: `provider not yet supported for download: <provider-name>`
3. User must manually install the provider

### Version Management

Default versions for automatically downloaded providers are defined in `/internal/deployment/provider_version.go`.

```go
func getDefaultProviderVersion(providerName string) string {
    switch providerName {
    case "aws":
        return "6.0.0"
    case "azurerm":
        return "3.85.0"
    case "google":
        return "5.10.0"
    case "null":
        return "3.2.2"
    case "random":
        return "3.6.0"
    default:
        return "latest"
    }
}
```

## Adding Support for New Providers

To add support for a new provider, follow these steps:

### 1. Add Download Function

In `/pkg/provider/protocol/download_provider.go`, add a new download function:

```go
// GetMyProviderURL returns the download URL for terraform-provider-myprovider
func GetMyProviderURL(version string) *DownloadInfo {
    goos := runtime.GOOS
    goarch := runtime.GOARCH

    filename := fmt.Sprintf("terraform-provider-myprovider_%s_%s_%s.zip", version, goos, goarch)
    url := fmt.Sprintf("https://releases.hashicorp.com/terraform-provider-myprovider/%s/%s", version, filename)

    // TODO: Add real checksums from provider's SHA256SUMS file
    checksums := map[string]string{
        "terraform-provider-myprovider_1.0.0_linux_amd64.zip":  "actual-checksum-here",
        "terraform-provider-myprovider_1.0.0_darwin_amd64.zip": "actual-checksum-here",
        "terraform-provider-myprovider_1.0.0_darwin_arm64.zip": "actual-checksum-here",
    }

    return &DownloadInfo{
        URL:      url,
        Filename: filename,
        SHA256:   checksums[filename],
    }
}
```

### 2. Register in Download Switch

Add the provider to the switch statement in `getProviderDownloadInfo`:

```go
case "myprovider":
    return GetMyProviderURL(version)
```

### 3. Add Default Version

In `/internal/deployment/provider_version.go`, add a default version:

```go
case "myprovider":
    return "1.0.0"  // Use a stable, tested version
```

### 4. Test the Provider

Before adding a provider:

1. Verify protocol compatibility (v5 or v6)
2. Test basic resource operations
3. Ensure provider binary is available from releases.hashicorp.com
4. Obtain SHA256 checksums for security

### 5. Update Documentation

Add the provider to this documentation with:

- Provider name and registry source
- Default version
- Example resources
- Any special configuration requirements

## Currently Supported Providers

Lattiam has been fully tested and is officially supported for AWS. Other providers are available for use but should be considered experimental.

| Provider     | Registry Name | Default Version | Download Support | Support Level  |
| ------------ | ------------- | --------------- | ---------------- | -------------- |
| AWS          | `aws`         | 6.0.0           | ✅ Automatic     | **Official**   |
| Azure        | `azurerm`     | 3.85.0          | ✅ Automatic     | _Experimental_ |
| Google Cloud | `google`      | 5.10.0          | ✅ Automatic     | _Experimental_ |
| Kubernetes   | `kubernetes`  | 2.25.2          | ✅ Automatic     | _Experimental_ |
| Random       | `random`      | 3.6.0           | ✅ Automatic     | _Experimental_ |
| Null         | `null`        | 3.2.2           | ✅ Automatic     | _Experimental_ |

## Using Unsupported Providers

For providers not yet in the supported list:

### Option 1: Manual Installation

1. Download the provider binary manually
2. Place it in `~/.lattiam/providers/<name>/<version>/<platform>/`
3. Ensure it's executable: `chmod +x terraform-provider-<name>`
4. Lattiam will use the manually installed provider

### Option 2: Request Support

1. Open an issue on GitHub
2. Provide:
   - Provider name and source
   - Version you need
   - Use case description
3. We'll add support after testing

### Option 3: Fork and Add Support

1. Fork the repository
2. Follow the "Adding Support" steps above
3. Submit a pull request
4. Include test results in the PR

## Security Considerations

### Why Provider Gating?

1. **Security**: Only download from verified sources with checksums
2. **Compatibility**: Ensure providers work with our gRPC implementation
3. **Stability**: Test providers before allowing automatic use
4. **Support**: Provide known-good versions for common use cases

### Checksum Verification

All provider downloads are verified against SHA256 checksums to ensure:

- Provider binaries haven't been tampered with
- Downloads aren't corrupted
- You're getting the exact version tested

## Provider Protocol Compatibility

Lattiam supports:

- **Protocol v5**: Most current providers
- **Protocol v6**: Newer providers and features

Both protocols are automatically detected and used appropriately.

## Environment Variable Overrides

You can override the hardcoded default provider versions using environment variables without rebuilding Lattiam:

```bash
# Override AWS provider version
export AWS_PROVIDER_VERSION=5.50.0

# Override any provider
export <PROVIDER_NAME_UPPERCASE>_PROVIDER_VERSION=x.y.z
```

## Troubleshooting Provider Issues

### "Provider not supported for download"

- Provider isn't in the supported list
- Use manual installation or request support

### "Failed to download provider"

- Check network connectivity
- Verify the version exists
- Check disk space

### "Checksum mismatch"

- Provider binary may be corrupted
- Try deleting and re-downloading
- Verify you're using a supported version
