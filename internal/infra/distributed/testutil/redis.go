package testutil

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func init() {
	// Set environment variables globally to allow test parallelization
	// Using t.Setenv() prevents t.Parallel() from working
	_ = os.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")
}

// RedisContainer holds the test Redis container and connection details
type RedisContainer struct {
	Container testcontainers.Container
	URL       string
}

// SetupRedis creates a Redis connection for testing
// Creates individual containers for each test for proper isolation and parallelization
func SetupRedis(t *testing.T) *RedisContainer {
	t.Helper()

	// Note: We set TESTCONTAINERS_RYUK_DISABLED globally in init() to avoid t.Setenv() which prevents parallelization
	// Simply create an individual Redis container for each test
	return SetupRedisWithConfig(t, "notice")
}

// SetupRedisWithConfig creates an individual Redis container with custom configuration
//
//nolint:funlen // Test setup function requires comprehensive container configuration
func SetupRedisWithConfig(t *testing.T, logLevel string) *RedisContainer {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Check if we're in a container environment with a shared network
	networkName := os.Getenv("DOCKER_NETWORK_NAME")

	if networkName != "" {
		// We're in a container environment, create Redis on the shared network
		// Use a predictable name so we can connect via container name
		redisContainerName := fmt.Sprintf("redis-test-%d-%d", os.Getpid(), time.Now().UnixNano())
		req := testcontainers.ContainerRequest{
			Name:  redisContainerName,
			Image: "redis:7-alpine",
			// Reduced timeout for Docker proxy environment
			WaitingFor: wait.ForLog("Ready to accept connections").
				WithStartupTimeout(15 * time.Second),
			Networks: []string{networkName},
			NetworkAliases: map[string][]string{
				networkName: {redisContainerName},
			},
			// Add resource limits using environment variables
			Env: map[string]string{
				"REDIS_MAXMEMORY": "100mb",
			},
			Cmd: []string{"redis-server", "--loglevel", logLevel, "--bind", "0.0.0.0", "--protected-mode", "no", "--maxmemory", "100mb", "--maxmemory-policy", "allkeys-lru"},
		}

		container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: req,
			Started:          true,
		})
		if err != nil {
			t.Fatalf("Failed to start Redis container: %v", err)
		}

		// Use container name for connection in shared network
		endpoint := fmt.Sprintf("redis://%s:6379", redisContainerName)

		// Ensure cleanup with timeout
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cleanupCancel()
			if err := container.Terminate(cleanupCtx); err != nil {
				t.Logf("Failed to terminate Redis container: %v", err)
			}
		})

		return &RedisContainer{
			Container: container,
			URL:       endpoint,
		}
	}

	// Not in container environment, use optimized approach
	req := testcontainers.ContainerRequest{
		Image:        "redis:7-alpine",
		ExposedPorts: []string{"6379/tcp"},
		// Reduced timeout and simplified wait strategy
		WaitingFor: wait.ForLog("Ready to accept connections").
			WithStartupTimeout(15 * time.Second),
		// Add resource limits using environment variables
		Env: map[string]string{
			"REDIS_MAXMEMORY": "100mb",
		},
		Cmd: []string{"redis-server", "--loglevel", logLevel, "--maxmemory", "100mb", "--maxmemory-policy", "allkeys-lru"},
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("Failed to start Redis container: %v", err)
	}

	// Get connection details with retry
	host, port, err := getContainerConnection(ctx, container)
	if err != nil {
		t.Fatalf("Failed to get Redis connection details: %v", err)
	}

	// Build connection URL
	endpoint := fmt.Sprintf("redis://%s:%s", host, port)

	// Ensure cleanup with timeout
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := container.Terminate(cleanupCtx); err != nil {
			t.Logf("Failed to terminate Redis container: %v", err)
		}
	})

	return &RedisContainer{
		Container: container,
		URL:       endpoint,
	}
}

// getContainerConnection retrieves host and port with retries
func getContainerConnection(ctx context.Context, container testcontainers.Container) (host string, port string, err error) {
	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		attemptCtx, cancel := context.WithTimeout(ctx, 5*time.Second)

		host, err := container.Host(attemptCtx)
		if err != nil {
			cancel()
			if i == maxRetries-1 {
				return "", "", fmt.Errorf("failed to get Redis host after %d retries: %w", maxRetries, err)
			}
			time.Sleep(time.Duration(i+1) * 200 * time.Millisecond)
			continue
		}

		// Force IPv4 to avoid IPv6 issues in CI environments
		// Replace any form of localhost with 127.0.0.1
		if host == "localhost" || host == "::1" || host == "[::1]" {
			host = "127.0.0.1"
		}

		mappedPort, err := container.MappedPort(attemptCtx, "6379")
		cancel()
		if err != nil {
			if i == maxRetries-1 {
				return "", "", fmt.Errorf("failed to get Redis port after %d retries: %w", maxRetries, err)
			}
			time.Sleep(time.Duration(i+1) * 200 * time.Millisecond)
			continue
		}

		return host, mappedPort.Port(), nil
	}
	return "", "", fmt.Errorf("unexpected retry loop exit")
}

// WaitForRedis waits for Redis to be ready for connections
func WaitForRedis(_ *testing.T, container *RedisContainer, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// The container should already be ready when returned from Run()
	// This function is kept for backward compatibility
	state, err := container.Container.State(ctx)
	if err != nil {
		return fmt.Errorf("failed to get container state: %w", err)
	}
	if !state.Running {
		return context.DeadlineExceeded
	}
	return nil
}

// GetRedisURL formats the Redis URL correctly for the client
func (r *RedisContainer) GetRedisURL() string {
	// The ConnectionString from testcontainers returns redis://<host>:<port>
	// which is what we need
	return r.URL
}

// Stop stops the Redis container (useful for resilience testing)
func (r *RedisContainer) Stop(ctx context.Context) error {
	timeout := 10 * time.Second
	err := r.Container.Stop(ctx, &timeout)
	if err != nil {
		return fmt.Errorf("failed to stop container: %w", err)
	}
	return nil
}

// Start restarts a stopped Redis container
func (r *RedisContainer) Start(ctx context.Context) error {
	err := r.Container.Start(ctx)
	if err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	// After restart, the port mapping might have changed, so update the URL
	host, port, err := getContainerConnection(ctx, r.Container)
	if err != nil {
		return fmt.Errorf("failed to get connection details after restart: %w", err)
	}

	// Update the URL with the new connection details
	r.URL = fmt.Sprintf("redis://%s:%s", host, port)

	return nil
}
