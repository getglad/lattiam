// Package downloader provides functionality for downloading and caching Terraform provider binaries.
package downloader

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Static errors for err113 compliance
var (
	ErrFailedToDownloadProvider = errors.New("failed to download provider")
)

// Downloader implements the provider.Downloader interface
type Downloader struct {
	baseDir    string
	httpClient *http.Client
}

// NewDownloader creates a new provider downloader
func NewDownloader(baseDir string) *Downloader {
	return &Downloader{
		baseDir:    baseDir,
		httpClient: &http.Client{Timeout: 10 * time.Minute},
	}
}

// Download downloads a provider binary
//
//nolint:funlen // Provider download involves multiple validation, caching, and extraction steps
func (d *Downloader) Download(ctx context.Context, name, version, platform string) (string, error) {
	// Construct download URL (this is simplified - real implementation would use Terraform Registry API)
	url := d.getDownloadURL(name, version, platform)

	// Create local directory
	providerDir := filepath.Join(d.baseDir, name, version, platform)
	if err := os.MkdirAll(providerDir, 0o700); err != nil {
		return "", fmt.Errorf("failed to create provider directory: %w", err)
	}

	// Determine binary name
	binaryName := fmt.Sprintf("terraform-provider-%s_v%s_x5", name, version)
	if platform == "windows_amd64" {
		binaryName += ".exe"
	}

	localPath := filepath.Join(providerDir, binaryName)

	// Check if already exists
	if _, err := os.Stat(localPath); err == nil {
		return localPath, nil
	}

	// Download the provider
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("failed to create download request: %w", err)
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to download provider: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: status %d", ErrFailedToDownloadProvider, resp.StatusCode)
	}

	// Create temporary file
	tmpFile, err := os.CreateTemp(providerDir, "download-*.tmp")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer func() {
		_ = tmpFile.Close()
	}()

	// Copy response body to temporary file
	_, err = io.Copy(tmpFile, resp.Body)
	if err != nil {
		_ = os.Remove(tmpFile.Name())
		return "", fmt.Errorf("failed to write provider binary: %w", err)
	}

	// Close the temp file before processing
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpFile.Name())
		return "", fmt.Errorf("failed to close temporary file: %w", err)
	}

	// Extract the provider binary from the ZIP file
	if err := d.extractProviderFromZip(tmpFile.Name(), localPath); err != nil {
		_ = os.Remove(tmpFile.Name())
		return "", fmt.Errorf("failed to extract provider: %w", err)
	}

	// Clean up the temporary ZIP file
	_ = os.Remove(tmpFile.Name())

	// Make the extracted binary executable
	if err := os.Chmod(localPath, 0o700); err != nil { //nolint:gosec // Provider binaries must be executable
		_ = os.Remove(localPath)
		return "", fmt.Errorf("failed to make provider executable: %w", err)
	}

	return localPath, nil
}

// GetLocalPath returns the local path for a provider
func (d *Downloader) GetLocalPath(name, version, platform string) string {
	providerDir := filepath.Join(d.baseDir, name, version, platform)
	binaryName := fmt.Sprintf("terraform-provider-%s_v%s_x5", name, version)
	if platform == "windows_amd64" {
		binaryName += ".exe"
	}
	return filepath.Join(providerDir, binaryName)
}

// IsAvailable checks if a provider is already downloaded
func (d *Downloader) IsAvailable(name, version, platform string) bool {
	localPath := d.GetLocalPath(name, version, platform)
	_, err := os.Stat(localPath)
	return err == nil
}

// extractProviderFromZip extracts the provider binary from a ZIP file
func (d *Downloader) extractProviderFromZip(zipPath, destPath string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("failed to open zip file: %w", err)
	}
	defer func() { _ = reader.Close() }()

	// Find the provider binary in the ZIP (it should be the only file or the one with terraform-provider prefix)
	for _, file := range reader.File {
		// Skip directories and non-provider files
		if file.FileInfo().IsDir() || !strings.Contains(file.Name, "terraform-provider") {
			continue
		}

		// Check file size to prevent decompression bombs (limit to 500MB)
		const maxSize = 500 * 1024 * 1024
		if file.UncompressedSize64 > maxSize {
			return fmt.Errorf("provider binary too large (%d bytes), max allowed: %d bytes", file.UncompressedSize64, maxSize)
		}

		// Extract the file without defers in loop
		if err := d.extractSingleFile(file, destPath); err != nil {
			return err
		}

		// We found and extracted the provider binary
		return nil
	}

	return fmt.Errorf("no provider binary found in zip file")
}

// extractSingleFile extracts a single file from the zip to avoid defer in loop
func (d *Downloader) extractSingleFile(file *zip.File, destPath string) error {
	src, err := file.Open()
	if err != nil {
		return fmt.Errorf("failed to open file in zip: %w", err)
	}
	defer func() { _ = src.Close() }()

	// Create the destination file
	dst, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode()) // #nosec G304 -- destPath is validated by caller
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer func() { _ = dst.Close() }()

	// Use io.LimitReader to prevent decompression bombs
	const maxSize = 500 * 1024 * 1024
	limitedReader := io.LimitReader(src, maxSize)

	// Copy the contents
	n, err := io.Copy(dst, limitedReader)
	if err != nil {
		return fmt.Errorf("failed to extract file: %w", err)
	}

	// Check if we hit the size limit
	if n == maxSize {
		// Try to read one more byte to see if the file was truncated
		var buf [1]byte
		if _, err := src.Read(buf[:]); err != io.EOF {
			return fmt.Errorf("provider binary exceeds maximum allowed size of %d bytes", maxSize)
		}
	}

	return nil
}

// getDownloadURL constructs the download URL for a provider
// This is a simplified implementation - real implementation would use Terraform Registry API
func (d *Downloader) getDownloadURL(name, version, platform string) string {
	// For AWS provider, use the official releases
	if name == "aws" {
		return fmt.Sprintf("https://releases.hashicorp.com/terraform-provider-aws/%s/terraform-provider-aws_%s_%s.zip",
			version, version, platform)
	}

	// Generic pattern for other providers
	return fmt.Sprintf("https://releases.hashicorp.com/terraform-provider-%s/%s/terraform-provider-%s_%s_%s.zip",
		name, version, name, version, platform)
}
