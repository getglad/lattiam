package testutil

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	// Database counter for isolation (Redis supports DB 0-15)
	dbCounter int32 = -1

	// Mutex for database cleanup operations
	dbCleanupMutex sync.Mutex

	// Shared container instance and mutex for thread safety
	sharedContainer      *RedisContainer
	sharedContainerMutex sync.RWMutex
)

// RedisSetup contains the connection details for a test's Redis instance
type RedisSetup struct {
	URL       string // Full Redis URL including database number
	DB        int    // Database number (0-15)
	Host      string
	Port      string
	container *RedisContainer // Reference to shared container
}

// getOrCreateSharedRedis gets or creates a shared Redis container with proper synchronization
func getOrCreateSharedRedis(ctx context.Context, t *testing.T) (*RedisContainer, error) {
	t.Helper()
	// Use read lock first for fast path when container exists
	sharedContainerMutex.RLock()
	if sharedContainer != nil {
		// Verify container is still running
		if isContainerHealthy(ctx, sharedContainer.Container) {
			defer sharedContainerMutex.RUnlock()
			return sharedContainer, nil
		}
		// Container is unhealthy, need to recreate
		sharedContainer = nil
	}
	sharedContainerMutex.RUnlock()

	// Use write lock to create new container
	sharedContainerMutex.Lock()
	defer sharedContainerMutex.Unlock()

	// Double-check pattern - another goroutine might have created it
	if sharedContainer != nil && isContainerHealthy(ctx, sharedContainer.Container) {
		return sharedContainer, nil
	}

	// Create new container with optimized settings for Docker proxy environment
	container, err := createOptimizedRedisContainer(ctx, t)
	if err != nil {
		return nil, fmt.Errorf("failed to create shared Redis container: %w", err)
	}

	sharedContainer = container
	return sharedContainer, nil
}

// createOptimizedRedisContainer creates a Redis container optimized for the Docker proxy environment
func createOptimizedRedisContainer(ctx context.Context, _ *testing.T) (*RedisContainer, error) {
	// Use shorter timeouts to work around Docker proxy latency issues
	req := testcontainers.ContainerRequest{
		Image:        "redis:7-alpine",
		ExposedPorts: []string{"6379/tcp"},
		// Simplified wait strategy - just wait for log message
		// Avoid port checking which requires additional Docker API calls
		WaitingFor: wait.ForLog("Ready to accept connections").WithStartupTimeout(15 * time.Second),
		// Add resource limits to prevent resource exhaustion (using environment variables)
		Env: map[string]string{
			"REDIS_MAXMEMORY": "100mb",
		},
		Cmd: []string{"redis-server", "--loglevel", "notice", "--maxmemory", "100mb", "--maxmemory-policy", "allkeys-lru"},
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Redis container: %w", err)
	}

	// Get connection details with retry logic
	host, port, err := getContainerConnection(ctx, container)
	if err != nil {
		return nil, fmt.Errorf("failed to get Redis connection details: %w", err)
	}

	// Build connection URL
	endpoint := fmt.Sprintf("redis://%s:%s", host, port)

	return &RedisContainer{
		Container: container,
		URL:       endpoint,
	}, nil
}

// isContainerHealthy checks if a container is running without timing out
func isContainerHealthy(ctx context.Context, container testcontainers.Container) bool {
	// Use short timeout to avoid blocking
	healthCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	state, err := container.State(healthCtx)
	if err != nil {
		return false
	}
	return state.Running
}

// SetupSharedRedis provides a Redis connection with database isolation for tests
// Each test gets its own database (0-15) which is automatically cleaned before and after
func SetupSharedRedis(t *testing.T) *RedisSetup {
	t.Helper()

	// Use a timeout context for container setup
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	container, err := getOrCreateSharedRedis(ctx, t)
	if err != nil {
		// Log the specific error and fallback to individual container
		t.Logf("Shared Redis setup failed, using individual container (error: %v)", err)
		return setupIndividualRedis(t)
	}

	// Assign database number (cycle through 0-15)
	dbNum := int(atomic.AddInt32(&dbCounter, 1) % 16)

	// Parse connection details
	host, port, err := parseRedisURL(container.URL)
	if err != nil {
		t.Fatalf("Failed to parse Redis URL: %v", err)
	}

	// Create URL with database number
	urlWithDB := fmt.Sprintf("redis://%s:%s/%d", host, port, dbNum)

	// Clean the database before test with timeout
	cleanDatabase(t, host, port, dbNum)

	// Register cleanup after test
	t.Cleanup(func() {
		cleanDatabase(t, host, port, dbNum)
	})

	return &RedisSetup{
		URL:       urlWithDB,
		DB:        dbNum,
		Host:      host,
		Port:      port,
		container: container,
	}
}

// setupIndividualRedis creates a dedicated Redis container as fallback
func setupIndividualRedis(t *testing.T) *RedisSetup {
	t.Helper()

	// Use optimized container creation instead of the legacy SetupRedisWithConfig
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	container, err := createOptimizedRedisContainer(ctx, t)
	if err != nil {
		t.Fatalf("Failed to create individual Redis container: %v", err)
	}

	// Register cleanup
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := container.Container.Terminate(cleanupCtx); err != nil {
			t.Logf("Failed to terminate individual Redis container: %v", err)
		}
	})

	host, port, err := parseRedisURL(container.URL)
	if err != nil {
		t.Fatalf("Failed to parse Redis URL: %v", err)
	}

	return &RedisSetup{
		URL:       container.URL + "/0", // Use DB 0 for individual containers
		DB:        0,
		Host:      host,
		Port:      port,
		container: container,
	}
}

// cleanDatabase flushes a specific Redis database using Asynq with improved error handling
func cleanDatabase(t *testing.T, host, port string, dbNum int) {
	t.Helper()

	// Use mutex to prevent concurrent FLUSHDB operations
	dbCleanupMutex.Lock()
	defer dbCleanupMutex.Unlock()

	// Create Asynq Redis client option for the specific database
	opt := asynq.RedisClientOpt{
		Addr:         fmt.Sprintf("%s:%s", host, port),
		DB:           dbNum,
		DialTimeout:  3 * time.Second, // Short timeout for Docker proxy
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
	}

	// Create inspector to manage Redis directly
	inspector := asynq.NewInspector(opt)
	defer func() {
		if err := inspector.Close(); err != nil {
			t.Logf("Warning: Failed to close inspector: %v", err)
		}
	}()

	// Delete all queues with improved retry logic
	maxRetries := 3 // Reduced from 5 for faster feedback
	for i := 0; i < maxRetries; i++ {
		queues, err := inspector.Queues()
		if err == nil {
			// Successfully got queues, delete them
			for _, queue := range queues {
				if err := inspector.DeleteQueue(queue, true); err != nil {
					t.Logf("Warning: Failed to delete queue %s in Redis DB %d: %v", queue, dbNum, err)
				}
			}
			return
		}

		// Check for retryable errors
		if i < maxRetries-1 && isRetryableRedisError(err) {
			backoff := time.Duration(i+1) * 150 * time.Millisecond // Slightly longer backoff
			t.Logf("Redis cleanup attempt %d/%d failed, retrying in %v: %v", i+1, maxRetries, backoff, err)
			time.Sleep(backoff)
			continue
		}

		// Log non-retryable or final error
		t.Logf("Warning: Failed to clean Redis DB %d after %d attempts: %v", dbNum, i+1, err)
		return
	}
}

// isRetryableRedisError determines if a Redis error is worth retrying
func isRetryableRedisError(err error) bool {
	errStr := err.Error()
	return strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "connect: connection refused") ||
		strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "deadline exceeded")
}

// parseRedisURL extracts host and port from Redis URL
func parseRedisURL(url string) (host, port string, err error) {
	// URL format: redis://host:port or redis://host:port/
	// Remove redis:// prefix
	if len(url) < 8 {
		return "", "", fmt.Errorf("invalid Redis URL: %s", url)
	}

	hostPort := url[8:] // Skip "redis://"

	// Remove trailing slash if present
	if hostPort != "" && hostPort[len(hostPort)-1] == '/' {
		hostPort = hostPort[:len(hostPort)-1]
	}

	// Split host and port
	for i := len(hostPort) - 1; i >= 0; i-- {
		if hostPort[i] == ':' {
			return hostPort[:i], hostPort[i+1:], nil
		}
	}

	return "", "", fmt.Errorf("invalid Redis URL format: %s", url)
}

// Note: We no longer need TerminateSharedRedis as testcontainers manages
// the lifecycle of reused containers automatically with Ryuk

// ResetDatabaseCounter resets the database counter (useful for testing)
func ResetDatabaseCounter() {
	atomic.StoreInt32(&dbCounter, -1)
}
