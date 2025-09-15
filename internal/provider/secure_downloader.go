// Package provider provides secure downloading capabilities for Terraform provider binaries
package provider

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/lattiam/lattiam/pkg/logging"
)

// Security-related errors
var (
	ErrInsecureProtocol     = errors.New("insecure protocol: HTTPS required")
	ErrCertificateInvalid   = errors.New("certificate validation failed")
	ErrDownloadSizeExceeded = errors.New("download size exceeded maximum allowed")
	ErrChecksumMismatch     = errors.New("checksum verification failed")
	ErrInvalidURL           = errors.New("invalid download URL")
)

// SecureDownloaderConfig holds security configuration for downloads
type SecureDownloaderConfig struct {
	// MaxDownloadSize is the maximum allowed size for a provider binary (default: 500MB)
	MaxDownloadSize int64

	// DownloadTimeout is the maximum time allowed for a download (default: 10 minutes)
	DownloadTimeout time.Duration

	// RequireHTTPS enforces HTTPS for all downloads (default: true)
	RequireHTTPS bool

	// TrustedHosts is a list of allowed hostnames for downloads
	TrustedHosts []string

	// CertificatePins are SHA256 hashes of trusted certificate public keys (optional)
	CertificatePins []string

	// EnableProgressTracking enables download progress logging
	EnableProgressTracking bool
}

// DefaultSecureDownloaderConfig returns secure defaults
func DefaultSecureDownloaderConfig() *SecureDownloaderConfig {
	return &SecureDownloaderConfig{
		MaxDownloadSize: 500 * 1024 * 1024, // 500MB
		DownloadTimeout: 10 * time.Minute,
		RequireHTTPS:    true,
		TrustedHosts: []string{
			"releases.hashicorp.com",
			"registry.terraform.io",
			"github.com",
		},
		EnableProgressTracking: true,
	}
}

// SecureProviderDownloader implements secure provider binary downloading
type SecureProviderDownloader struct {
	config     *SecureDownloaderConfig
	httpClient *http.Client
	logger     *logging.Logger
}

// NewSecureProviderDownloader creates a new secure downloader
func NewSecureProviderDownloader(config *SecureDownloaderConfig) (*SecureProviderDownloader, error) {
	if config == nil {
		config = DefaultSecureDownloaderConfig()
	}

	// Create custom TLS configuration
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		},
	}

	// Set up certificate verification if pins are provided
	if len(config.CertificatePins) > 0 {
		tlsConfig.VerifyPeerCertificate = createCertificatePinVerifier(config.CertificatePins)
	}

	// Create HTTP client with security settings
	httpClient := &http.Client{
		Timeout: config.DownloadTimeout,
		Transport: &http.Transport{
			TLSClientConfig:     tlsConfig,
			DisableCompression:  false,
			MaxIdleConns:        10,
			IdleConnTimeout:     30 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
			ForceAttemptHTTP2:   true,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Limit redirects and ensure they stay on trusted hosts
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if err := validateURL(req.URL, config); err != nil {
				return fmt.Errorf("redirect validation failed: %w", err)
			}
			return nil
		},
	}

	return &SecureProviderDownloader{
		config:     config,
		httpClient: httpClient,
		logger:     logging.NewLogger("secure-downloader"),
	}, nil
}

// DownloadProvider downloads a provider binary with security checks
func (d *SecureProviderDownloader) DownloadProvider(
	ctx context.Context,
	downloadURL string,
	expectedChecksum string,
	destinationPath string,
) error {
	// Parse and validate URL
	parsedURL, err := url.Parse(downloadURL)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidURL, err)
	}

	// Validate URL security requirements
	if err := validateURL(parsedURL, d.config); err != nil {
		return err
	}

	// Create download request with context
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Add security headers
	req.Header.Set("User-Agent", "Lattiam-Provider-Downloader/1.0")
	req.Header.Set("Accept", "application/octet-stream")

	// Perform download
	resp, err := d.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	// Check content length against maximum
	if resp.ContentLength > 0 && resp.ContentLength > d.config.MaxDownloadSize {
		return fmt.Errorf("%w: %d bytes exceeds maximum of %d bytes",
			ErrDownloadSizeExceeded, resp.ContentLength, d.config.MaxDownloadSize)
	}

	// Create limited reader to prevent excessive downloads
	limitedReader := &limitedReader{
		reader:  resp.Body,
		limit:   d.config.MaxDownloadSize,
		current: 0,
	}

	// Download with checksum verification
	checksum, err := d.downloadWithVerification(limitedReader, destinationPath)
	if err != nil {
		return err
	}

	// Verify checksum if provided
	if expectedChecksum != "" && checksum != expectedChecksum {
		return fmt.Errorf("%w: expected %s, got %s", ErrChecksumMismatch, expectedChecksum, checksum)
	}

	d.logger.Info("Successfully downloaded provider binary: url=%s checksum=%s size=%d",
		downloadURL, checksum, limitedReader.current)

	return nil
}

// downloadWithVerification downloads and calculates checksum simultaneously
func (d *SecureProviderDownloader) downloadWithVerification(
	reader io.Reader,
	destinationPath string,
) (string, error) {
	// Create temporary file
	tempFile, err := createSecureTempFile(destinationPath)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() { _ = os.Remove(tempFile.Name()) }()
	defer func() { _ = tempFile.Close() }()

	// Create hash calculator
	hasher := sha256.New()

	// Create multi-writer for file and hash
	writer := io.MultiWriter(tempFile, hasher)

	// Copy with progress tracking if enabled
	var written int64
	if d.config.EnableProgressTracking {
		written, err = copyWithProgress(writer, reader, d.logger)
	} else {
		written, err = io.Copy(writer, reader)
	}

	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}

	// Calculate final checksum
	checksum := hex.EncodeToString(hasher.Sum(nil))

	// Close temp file before moving
	if err := tempFile.Close(); err != nil {
		return "", fmt.Errorf("failed to close temp file: %w", err)
	}

	// Move to final destination
	if err := os.Rename(tempFile.Name(), destinationPath); err != nil {
		return "", fmt.Errorf("failed to move downloaded file: %w", err)
	}

	// Set secure permissions (read-only, executable)
	if err := os.Chmod(destinationPath, 0o555); err != nil { //nolint:gosec // Provider binaries must be executable
		return "", fmt.Errorf("failed to set file permissions: %w", err)
	}

	d.logger.Debug("Download complete: bytes=%d checksum=%s", written, checksum)
	return checksum, nil
}

// validateURL ensures the URL meets security requirements
func validateURL(u *url.URL, config *SecureDownloaderConfig) error {
	// Check protocol
	if config.RequireHTTPS && u.Scheme != "https" {
		return fmt.Errorf("%w: %s", ErrInsecureProtocol, u.Scheme)
	}

	// Check trusted hosts
	if len(config.TrustedHosts) > 0 {
		trusted := false
		for _, host := range config.TrustedHosts {
			if u.Host == host || u.Hostname() == host {
				trusted = true
				break
			}
		}
		if !trusted {
			return fmt.Errorf("untrusted host: %s", u.Host)
		}
	}

	return nil
}

// createCertificatePinVerifier creates a certificate verification function
func createCertificatePinVerifier(pins []string) func([][]byte, [][]*x509.Certificate) error {
	pinSet := make(map[string]bool)
	for _, pin := range pins {
		pinSet[pin] = true
	}

	return func(_ [][]byte, verifiedChains [][]*x509.Certificate) error {
		for _, chain := range verifiedChains {
			for _, cert := range chain {
				// Calculate certificate public key hash
				hash := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
				pin := hex.EncodeToString(hash[:])

				if pinSet[pin] {
					return nil // Found a pinned certificate
				}
			}
		}
		return ErrCertificateInvalid
	}
}

// limitedReader prevents reading more than the specified limit
type limitedReader struct {
	reader  io.Reader
	limit   int64
	current int64
}

func (l *limitedReader) Read(p []byte) (n int, err error) {
	if l.current >= l.limit {
		return 0, ErrDownloadSizeExceeded
	}

	// Limit read size to remaining allowance
	remaining := l.limit - l.current
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}

	n, err = l.reader.Read(p)
	l.current += int64(n)

	if l.current >= l.limit && err == nil {
		err = ErrDownloadSizeExceeded
	}

	return n, err
}

// Helper functions

func createSecureTempFile(destinationPath string) (*os.File, error) {
	dir := filepath.Dir(destinationPath)
	file, err := os.CreateTemp(dir, ".download-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary file: %w", err)
	}
	return file, nil
}

func copyWithProgress(dst io.Writer, src io.Reader, logger *logging.Logger) (int64, error) {
	buf := make([]byte, 32*1024) // 32KB buffer
	var written int64
	var lastLog time.Time

	for {
		nr, er := src.Read(buf)
		if nr > 0 {
			nw, ew := dst.Write(buf[0:nr])
			if nw > 0 {
				written += int64(nw)
			}
			if ew != nil {
				return written, fmt.Errorf("failed to write data: %w", ew)
			}
			if nr != nw {
				return written, io.ErrShortWrite
			}

			// Log progress every 5 seconds
			if time.Since(lastLog) > 5*time.Second {
				logger.Debug("Download progress: bytes=%d", written)
				lastLog = time.Now()
			}
		}
		if er != nil {
			if er != io.EOF {
				return written, fmt.Errorf("failed to read data: %w", er)
			}
			break
		}
	}
	return written, nil
}
