package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	stateStoreTypeFile   = "file"
	stateStoreTypeMemory = "memory"
	stateStoreTypeAWS    = "aws"
)

// AppVersion is the application version, can be set at build time or runtime
var AppVersion = "dev"

// ServerConfig holds all configuration for the Lattiam server
type ServerConfig struct {
	// Server settings
	Port  int  `json:"port" env:"LATTIAM_PORT" flag:"port" default:"8080" desc:"Server port"`
	Debug bool `json:"debug" env:"LATTIAM_DEBUG" flag:"debug" default:"false" desc:"Enable debug mode"`

	// Storage paths
	StateDir    string `json:"state_dir" env:"LATTIAM_STATE_DIR" flag:"state-dir" default:"~/.lattiam/state" desc:"State directory path"`
	LogFile     string `json:"log_file" env:"LATTIAM_LOG_FILE" flag:"log-file" default:"" desc:"Log file path"` // empty = stdout
	ProviderDir string `json:"provider_dir" env:"LATTIAM_PROVIDER_DIR" flag:"provider-dir" default:"./providers" desc:"Provider directory path"`

	// State store configuration
	StateStore StateStoreConfig `json:"state_store"`

	// Queue configuration
	Queue QueueConfig `json:"queue"`

	// Daemon settings
	DaemonMode bool   `json:"daemon_mode" flag:"daemon" default:"false" desc:"Run in daemon mode"`
	PIDFile    string `json:"pid_file" env:"LATTIAM_PID_FILE" flag:"pid-file" default:"" desc:"PID file path"`
}

// StateStoreConfig holds state store specific configuration
type StateStoreConfig struct {
	Type string          `json:"type" env:"LATTIAM_STATE_STORE" flag:"state-store" default:"file" desc:"State store type (file, memory, aws)"`
	File FileStoreConfig `json:"file"`
	AWS  AWSStoreConfig  `json:"aws"`
}

// FileStoreConfig holds file-based state store configuration
type FileStoreConfig struct {
	Path string `json:"path" env:"LATTIAM_STATE_PATH" flag:"state-path" default:"~/.lattiam/state/deployments" desc:"State store path"`
}

// AWSStoreConfig holds AWS-based state store configuration (S3 + DynamoDB)
type AWSStoreConfig struct {
	// S3 Configuration
	S3 AWSS3Config `json:"s3"`

	// DynamoDB Configuration
	DynamoDB AWSDynamoDBConfig `json:"dynamodb"`
}

// AWSS3Config holds S3-specific configuration for AWS backend
type AWSS3Config struct {
	Bucket   string `json:"bucket" env:"LATTIAM_AWS_S3_BUCKET" desc:"S3 bucket name for state storage"`
	Region   string `json:"region" env:"LATTIAM_AWS_S3_REGION" desc:"AWS region for S3 bucket"`
	Prefix   string `json:"prefix" env:"LATTIAM_AWS_S3_PREFIX" default:"states/" desc:"S3 key prefix for state objects"`
	Endpoint string `json:"endpoint" env:"LATTIAM_AWS_S3_ENDPOINT" desc:"Custom S3 endpoint (for LocalStack)"`
}

// AWSDynamoDBConfig holds DynamoDB-specific configuration for AWS backend
type AWSDynamoDBConfig struct {
	Table    string           `json:"table" env:"LATTIAM_AWS_DYNAMODB_TABLE" desc:"DynamoDB table name for deployment metadata"`
	Region   string           `json:"region" env:"LATTIAM_AWS_DYNAMODB_REGION" desc:"AWS region for DynamoDB table"`
	Endpoint string           `json:"endpoint" env:"LATTIAM_AWS_DYNAMODB_ENDPOINT" desc:"Custom DynamoDB endpoint (for LocalStack)"`
	Locking  AWSLockingConfig `json:"locking"`
}

// AWSLockingConfig holds locking configuration for AWS backend
type AWSLockingConfig struct {
	Enabled    bool `json:"enabled" env:"LATTIAM_AWS_LOCKING_ENABLED" default:"true" desc:"Enable DynamoDB state locking"`
	TTLSeconds int  `json:"ttl_seconds" env:"LATTIAM_AWS_LOCKING_TTL_SECONDS" default:"300" desc:"Lock TTL in seconds"`
}

// QueueConfig holds queue system configuration
type QueueConfig struct {
	Type     string `json:"type" env:"LATTIAM_QUEUE_TYPE" flag:"queue-type" default:"embedded" desc:"Queue type (embedded, distributed)"`
	RedisURL string `json:"redis_url" env:"LATTIAM_REDIS_URL" flag:"redis-url" default:"" desc:"Redis URL for distributed mode"`
}

// NewServerConfig creates a new server configuration with defaults
func NewServerConfig() *ServerConfig {
	return &ServerConfig{
		Port:        8080,
		Debug:       false,
		StateDir:    "~/.lattiam/state",
		LogFile:     "",
		ProviderDir: "./providers",
		StateStore: StateStoreConfig{
			Type: "file",
			File: FileStoreConfig{
				Path: "~/.lattiam/state/deployments",
			},
			AWS: AWSStoreConfig{
				S3: AWSS3Config{
					Prefix: "states/",
				},
				DynamoDB: AWSDynamoDBConfig{
					Locking: AWSLockingConfig{
						Enabled:    true,
						TTLSeconds: 300,
					},
				},
			},
		},
		Queue: QueueConfig{
			Type:     "embedded",
			RedisURL: "",
		},
	}
}

// LoadFromEnv loads configuration from environment variables
func (c *ServerConfig) LoadFromEnv() error { //nolint:funlen,gocognit,gocyclo // Configuration loading function with many environment variables
	// Port
	if port := os.Getenv("LATTIAM_PORT"); port != "" {
		var p int
		if _, err := fmt.Sscanf(port, "%d", &p); err != nil {
			return fmt.Errorf("invalid LATTIAM_PORT value: %s", port)
		}
		c.Port = p
	}

	// Debug
	if debug := os.Getenv("LATTIAM_DEBUG"); debug != "" {
		switch strings.ToLower(debug) {
		case "true", "1", "yes", "on":
			c.Debug = true
		case "false", "0", "no", "off":
			c.Debug = false
		default:
			return fmt.Errorf("invalid LATTIAM_DEBUG value: %s", debug)
		}
	}

	// Paths
	if stateDir := os.Getenv("LATTIAM_STATE_DIR"); stateDir != "" {
		c.StateDir = stateDir
	}
	if logFile := os.Getenv("LATTIAM_LOG_FILE"); logFile != "" {
		c.LogFile = logFile
	}
	if providerDir := os.Getenv("LATTIAM_PROVIDER_DIR"); providerDir != "" {
		c.ProviderDir = providerDir
	}

	// State store
	if stateStore := os.Getenv("LATTIAM_STATE_STORE"); stateStore != "" {
		c.StateStore.Type = stateStore
	}
	if statePath := os.Getenv("LATTIAM_STATE_PATH"); statePath != "" {
		c.StateStore.File.Path = statePath
	}

	// AWS State store configuration
	if bucket := os.Getenv("LATTIAM_AWS_S3_BUCKET"); bucket != "" {
		c.StateStore.AWS.S3.Bucket = bucket
	}
	if region := os.Getenv("LATTIAM_AWS_S3_REGION"); region != "" {
		c.StateStore.AWS.S3.Region = region
	}
	if prefix := os.Getenv("LATTIAM_AWS_S3_PREFIX"); prefix != "" {
		c.StateStore.AWS.S3.Prefix = prefix
	}
	if endpoint := os.Getenv("LATTIAM_AWS_S3_ENDPOINT"); endpoint != "" {
		c.StateStore.AWS.S3.Endpoint = endpoint
	}
	if table := os.Getenv("LATTIAM_AWS_DYNAMODB_TABLE"); table != "" {
		c.StateStore.AWS.DynamoDB.Table = table
	}
	if region := os.Getenv("LATTIAM_AWS_DYNAMODB_REGION"); region != "" {
		c.StateStore.AWS.DynamoDB.Region = region
	}
	if endpoint := os.Getenv("LATTIAM_AWS_DYNAMODB_ENDPOINT"); endpoint != "" {
		c.StateStore.AWS.DynamoDB.Endpoint = endpoint
	}
	if enabled := os.Getenv("LATTIAM_AWS_LOCKING_ENABLED"); enabled != "" {
		c.StateStore.AWS.DynamoDB.Locking.Enabled = parseBool(enabled)
	}
	if ttl := os.Getenv("LATTIAM_AWS_LOCKING_TTL_SECONDS"); ttl != "" {
		if ttlInt, err := strconv.Atoi(ttl); err == nil {
			c.StateStore.AWS.DynamoDB.Locking.TTLSeconds = ttlInt
		}
	}

	// Queue
	if queueType := os.Getenv("LATTIAM_QUEUE_TYPE"); queueType != "" {
		c.Queue.Type = queueType
	}
	if redisURL := os.Getenv("LATTIAM_REDIS_URL"); redisURL != "" {
		c.Queue.RedisURL = redisURL
	}

	// PID file
	if pidFile := os.Getenv("LATTIAM_PID_FILE"); pidFile != "" {
		c.PIDFile = pidFile
	}

	return nil
}

// ExpandPaths expands all paths in the configuration (~ to home directory)
func (c *ServerConfig) ExpandPaths() error {
	var err error

	c.StateDir, err = expandPath(c.StateDir)
	if err != nil {
		return fmt.Errorf("failed to expand state_dir: %w", err)
	}

	if c.LogFile != "" {
		c.LogFile, err = expandPath(c.LogFile)
		if err != nil {
			return fmt.Errorf("failed to expand log_file: %w", err)
		}
	}

	c.ProviderDir, err = expandPath(c.ProviderDir)
	if err != nil {
		return fmt.Errorf("failed to expand provider_dir: %w", err)
	}

	c.StateStore.File.Path, err = expandPath(c.StateStore.File.Path)
	if err != nil {
		return fmt.Errorf("failed to expand state_path: %w", err)
	}

	if c.PIDFile == "" {
		// Default PID file location
		c.PIDFile = filepath.Join(os.TempDir(), "lattiam-server.pid")
	} else {
		c.PIDFile, err = expandPath(c.PIDFile)
		if err != nil {
			return fmt.Errorf("failed to expand pid_file: %w", err)
		}
	}

	return nil
}

// Validate checks if the configuration is valid
func (c *ServerConfig) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("invalid port: %d", c.Port)
	}

	// Validate required directories
	if c.StateDir == "" {
		return fmt.Errorf("state directory cannot be empty")
	}
	if c.ProviderDir == "" {
		return fmt.Errorf("provider directory cannot be empty")
	}

	// Validate state store type
	switch c.StateStore.Type {
	case stateStoreTypeFile, stateStoreTypeMemory, stateStoreTypeAWS:
		// Valid types
	default:
		return fmt.Errorf("invalid state store type: %s", c.StateStore.Type)
	}

	// Validate AWS configuration if AWS type is selected
	if c.StateStore.Type == stateStoreTypeAWS {
		if c.StateStore.AWS.S3.Bucket == "" {
			return fmt.Errorf("AWS S3 bucket is required when using AWS state store")
		}
		if c.StateStore.AWS.S3.Region == "" {
			return fmt.Errorf("AWS S3 region is required when using AWS state store")
		}
		if c.StateStore.AWS.DynamoDB.Table == "" {
			return fmt.Errorf("AWS DynamoDB table is required when using AWS state store")
		}
		if c.StateStore.AWS.DynamoDB.Region == "" {
			return fmt.Errorf("AWS DynamoDB region is required when using AWS state store")
		}
		if c.StateStore.AWS.S3.Prefix == "" {
			c.StateStore.AWS.S3.Prefix = "states/" // Set default if empty
		}
		if c.StateStore.AWS.DynamoDB.Locking.TTLSeconds <= 0 {
			return fmt.Errorf("AWS locking TTL seconds must be positive")
		}
	}

	return nil
}

// GetLogPath returns the full path for the log file, handling defaults
func (c *ServerConfig) GetLogPath() string {
	if c.LogFile != "" {
		return c.LogFile
	}
	if c.DaemonMode {
		// Default log file for daemon mode
		return filepath.Join(os.TempDir(), "lattiam-server.log")
	}
	return "" // stdout
}

// ToJSON returns the configuration as a JSON string
func (c *ServerConfig) ToJSON() string {
	data, _ := json.MarshalIndent(c, "", "  ")
	return string(data)
}

// GetSanitized returns a sanitized version of the config safe for logging
func (c *ServerConfig) GetSanitized() map[string]interface{} {
	// Only return non-sensitive configuration
	sanitized := map[string]interface{}{
		"port":        c.Port,
		"debug":       c.Debug,
		"daemon_mode": c.DaemonMode,
		"state_store": c.StateStore.Type,
	}

	// In debug mode, include paths but still sanitize them
	if c.Debug {
		sanitized["state_configured"] = c.StateDir != ""
		sanitized["log_configured"] = c.GetLogPath() != ""
		sanitized["provider_configured"] = c.ProviderDir != ""

		// Include AWS configuration status without sensitive values
		if c.StateStore.Type == stateStoreTypeAWS {
			awsConfig := map[string]interface{}{
				"s3_bucket_configured":         c.StateStore.AWS.S3.Bucket != "",
				"s3_region_configured":         c.StateStore.AWS.S3.Region != "",
				"s3_prefix":                    c.StateStore.AWS.S3.Prefix,
				"s3_endpoint_configured":       c.StateStore.AWS.S3.Endpoint != "",
				"dynamodb_table_configured":    c.StateStore.AWS.DynamoDB.Table != "",
				"dynamodb_region_configured":   c.StateStore.AWS.DynamoDB.Region != "",
				"dynamodb_endpoint_configured": c.StateStore.AWS.DynamoDB.Endpoint != "",
				"locking_enabled":              c.StateStore.AWS.DynamoDB.Locking.Enabled,
				"locking_ttl_seconds":          c.StateStore.AWS.DynamoDB.Locking.TTLSeconds,
			}
			sanitized["aws_config"] = awsConfig
		}
	}

	return sanitized
}

// expandPath expands ~ to the home directory
func expandPath(path string) (string, error) {
	if path == "" {
		return path, nil
	}

	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		path = filepath.Join(home, path[2:])
	}

	return filepath.Clean(path), nil
}

// WriteConfigInfo writes configuration info to a well-known location for debugging
func (c *ServerConfig) WriteConfigInfo() error {
	info := struct {
		StartedAt string                 `json:"started_at"`
		PID       int                    `json:"pid"`
		Version   string                 `json:"version"`
		Config    map[string]interface{} `json:"config"`
	}{
		StartedAt: time.Now().Format(time.RFC3339),
		PID:       os.Getpid(),
		Version:   AppVersion,
		Config:    c.GetSanitized(),
	}

	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config info: %w", err)
	}

	// Check for custom path from environment
	infoPath := os.Getenv("LATTIAM_INFO_FILE")
	if infoPath == "" {
		// Fall back to temp directory
		infoPath = filepath.Join(os.TempDir(), "lattiam.info")
	}

	// Expand ~ if present
	expanded, err := expandPath(infoPath)
	if err == nil {
		infoPath = expanded
	}

	if err := os.WriteFile(infoPath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write server info: %w", err)
	}
	return nil
}

// GetPIDPath returns just the PID file path from environment
// This is a lightweight alternative to loading the full config
func GetPIDPath() string {
	pidFile := os.Getenv("LATTIAM_PID_FILE")
	if pidFile != "" {
		expanded, err := expandPath(pidFile)
		if err == nil {
			return expanded
		}
		// Fall through to default on error
	}

	// Default PID file location
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/lattiam.pid"
	}
	return filepath.Join(home, ".lattiam", "lattiam.pid")
}

// GetPort returns just the port from environment
// This is a lightweight alternative to loading the full config
func GetPort() int {
	portStr := os.Getenv("LATTIAM_PORT")
	if portStr == "" {
		return 8080 // default
	}

	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return 8080 // default on error
	}

	return port
}

// parseBool parses a string to bool with more lenient handling
func parseBool(value string) bool {
	switch strings.ToLower(value) {
	case "true", "1", "yes", "on", "enabled":
		return true
	case "false", "0", "no", "off", "disabled", "":
		return false
	default:
		return false
	}
}
