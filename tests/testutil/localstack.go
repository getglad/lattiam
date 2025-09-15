// Package testutil provides testing utilities for Lattiam tests
package testutil

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/localstack"
	"github.com/testcontainers/testcontainers-go/wait"
)

// LocalStackContainer holds the test LocalStack container and connection details
type LocalStackContainer struct {
	Container testcontainers.Container
	Endpoint  string
}

// SetupLocalStack creates a LocalStack container for testing
// Creates individual containers for each test for proper isolation and parallelization
func SetupLocalStack(t *testing.T) *LocalStackContainer {
	t.Helper()
	return SetupLocalStackWithServices(t, "s3,dynamodb,sts,iam")
}

// SetupLocalStackWithServices creates an individual LocalStack container with specific services
//
//nolint:funlen // Complex LocalStack setup with multiple service configuration
func SetupLocalStackWithServices(t *testing.T, services string) *LocalStackContainer {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Check if we're in a container environment with a shared network
	// We need both the network name AND to actually be in a container
	networkName := os.Getenv("DOCKER_NETWORK_NAME")

	// Check if we're inside a container - look for .dockerenv or cgroup evidence
	_, err := os.Stat("/.dockerenv")
	inContainer := err == nil
	if !inContainer {
		// Also check for act environment variable
		inContainer = os.Getenv("ACT") == "true"
	}

	if networkName != "" && inContainer {
		// We're in a container environment (CI/act), use the shared network
		containerName := fmt.Sprintf("localstack-test-%d-%d", os.Getpid(), time.Now().UnixNano())

		req := testcontainers.ContainerRequest{
			Name:  containerName,
			Image: "localstack/localstack:3.8.1",
			Env: map[string]string{
				"SERVICES": services,
				"DEBUG":    "0",
			},
			WaitingFor: wait.ForLog("Ready").
				WithStartupTimeout(60 * time.Second),
			Networks: []string{networkName},
			NetworkAliases: map[string][]string{
				networkName: {containerName},
			},
		}

		container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: req,
			Started:          true,
		})
		if err != nil {
			t.Fatalf("Failed to start LocalStack container: %v", err)
		}

		// Use container name for connection in shared network
		endpoint := fmt.Sprintf("http://%s:4566", containerName)

		// Ensure cleanup with timeout
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cleanupCancel()
			if err := container.Terminate(cleanupCtx); err != nil {
				t.Logf("Failed to terminate LocalStack container: %v", err)
			}
		})

		return &LocalStackContainer{
			Container: container,
			Endpoint:  endpoint,
		}
	}

	// Not in container environment, use optimized approach with testcontainers LocalStack module
	container, err := localstack.Run(ctx,
		"localstack/localstack:3.8.1",
		testcontainers.WithEnv(map[string]string{
			"SERVICES": services,
			"DEBUG":    "0",
		}),
	)
	if err != nil {
		t.Fatalf("Failed to start LocalStack container: %v", err)
	}

	// Get the mapped port
	mappedPort, err := container.MappedPort(ctx, "4566/tcp")
	if err != nil {
		t.Fatalf("Failed to get LocalStack port: %v", err)
	}

	// Get the host
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("Failed to get LocalStack host: %v", err)
	}

	endpoint := fmt.Sprintf("http://%s:%s", host, mappedPort.Port())

	// Ensure cleanup with timeout
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if err := container.Terminate(cleanupCtx); err != nil {
			t.Logf("Failed to terminate LocalStack container: %v", err)
		}
	})

	return &LocalStackContainer{
		Container: container,
		Endpoint:  endpoint,
	}
}

// GetEndpoint returns the LocalStack endpoint for this container
func (l *LocalStackContainer) GetEndpoint() string {
	return l.Endpoint
}

// Stop stops the LocalStack container (useful for resilience testing)
func (l *LocalStackContainer) Stop(ctx context.Context) error {
	timeout := 10 * time.Second
	err := l.Container.Stop(ctx, &timeout)
	if err != nil {
		return fmt.Errorf("failed to stop container: %w", err)
	}
	return nil
}

// Start restarts a stopped LocalStack container
func (l *LocalStackContainer) Start(ctx context.Context) error {
	err := l.Container.Start(ctx)
	if err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}
	return nil
}
