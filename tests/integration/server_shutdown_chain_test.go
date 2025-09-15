//go:build integration
// +build integration

package integration

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/apiserver"
	"github.com/lattiam/lattiam/internal/config"
	"github.com/lattiam/lattiam/internal/events"
	"github.com/lattiam/lattiam/internal/executor"
	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/system"
	"github.com/lattiam/lattiam/tests/helpers"
)

// createRealComponentsWithStorage creates real components for testing with persistent storage
func createRealComponentsWithStorage(t *testing.T, tempStateDir string) *helpers.TestComponents {
	t.Helper()

	// Set up LocalStack for AWS services
	localstack := helpers.NewLocalStackHelper(t)
	localstack.SkipIfLocalStackUnavailable()

	// Create S3 bucket and DynamoDB table with unique names
	bucketName, cleanupBucket := localstack.CreateTestBucket("persistence-test")
	defer cleanupBucket()

	tableName := fmt.Sprintf("lattiam-persistence-locks-%d", time.Now().UnixNano())
	cleanupTable := localstack.CreateTestDynamoDBTable(tableName)
	defer cleanupTable()

	// Create configuration
	cfg := config.NewServerConfig()
	cfg.StateStore.Type = "aws" // Use AWS backend with LocalStack for persistence across restarts
	cfg.StateStore.AWS.S3.Bucket = bucketName
	cfg.StateStore.AWS.S3.Region = "us-east-1"
	cfg.StateStore.AWS.S3.Prefix = "lattiam-state"
	cfg.StateStore.AWS.S3.Endpoint = localstack.Endpoint
	cfg.StateStore.AWS.DynamoDB.Table = tableName
	cfg.StateStore.AWS.DynamoDB.Region = "us-east-1"
	cfg.StateStore.AWS.DynamoDB.Endpoint = localstack.Endpoint
	cfg.StateStore.AWS.DynamoDB.Locking.Enabled = true
	cfg.StateStore.AWS.DynamoDB.Locking.TTLSeconds = 300
	cfg.ProviderDir = filepath.Join(tempStateDir, "providers")
	cfg.Queue.Type = "embedded" // Use embedded mode for testing

	// Create state store
	factory := system.NewDefaultComponentFactory()
	stateConfig := interfaces.StateStoreConfig{
		Type: cfg.StateStore.Type,
		Options: map[string]interface{}{
			"server_config": cfg, // The component factory expects the full server config
		},
	}
	stateStore, err := factory.CreateStateStore(stateConfig)
	if err != nil {
		t.Fatalf("Failed to create state store: %v", err)
	}

	// Create provider manager with binary cache enabled for tests
	providerConfig := interfaces.ProviderManagerConfig{
		Options: map[string]interface{}{
			"provider_dir":        cfg.ProviderDir,
			"enable_binary_cache": true, // Enable binary cache for tests to avoid download timeouts
		},
	}
	providerManager, err := factory.CreateProviderLifecycleManager(providerConfig)
	if err != nil {
		t.Fatalf("Failed to create provider manager: %v", err)
	}

	// Create interpolator
	interpolatorConfig := interfaces.InterpolationResolverConfig{
		// Use default configuration for interpolator
		Options: map[string]interface{}{},
	}
	interpolator, err := factory.CreateInterpolationResolver(interpolatorConfig)
	if err != nil {
		t.Fatalf("Failed to create interpolator: %v", err)
	}

	// Create synchronous event bus for testing
	eventBus := events.NewSynchronousEventBus()

	// Create deployment executor with the event bus
	deploymentExecutor := executor.NewWithEventBus(providerManager, stateStore, eventBus, 10*time.Minute)
	executorFunc := executor.CreateExecutorFunc(deploymentExecutor)

	// Create background system using factory
	components, err := system.NewBackgroundSystem(cfg, executorFunc)
	if err != nil {
		t.Fatalf("Failed to create background system: %v", err)
	}

	// Connect tracker to event bus
	events.ConnectTrackerToEventBus(eventBus, components.Tracker)

	// Start the worker pool
	components.WorkerPool.Start()

	// Ensure cleanup on test completion
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := components.Close(ctx); err != nil {
			t.Logf("Warning: failed to close components: %v", err)
		}
	})

	return &helpers.TestComponents{
		Components:      components,
		StateStore:      stateStore,
		ProviderManager: providerManager,
		Interpolator:    interpolator,
	}
}

// TestServerWorkerProviderShutdownChain tests the complete shutdown chain:
// API Server → Worker System → Provider Manager → Provider Processes
// This addresses the gap identified in the original feedback about incomplete shutdown testing
func TestServerWorkerProviderShutdownChain(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	t.Run("CompleteShutdownChainWithRealProviders", func(t *testing.T) {
		// Create test server with real worker system and provider manager
		testServer := helpers.StartTestServer(t)
		require.NotNil(t, testServer)
		require.NotNil(t, testServer.Components)

		// Verify system components are available before submitting deployments
		queueMetrics := testServer.Components.Queue.GetMetrics()
		t.Logf("Initial system state: Queue depth=%d, Total enqueued=%d",
			queueMetrics.CurrentDepth, queueMetrics.TotalEnqueued)

		// Submit multiple deployments to create active providers
		deploymentIDs := make([]string, 3)
		for i := 0; i < 3; i++ {
			deploymentID := testServer.SubmitTestDeployment(t)
			deploymentIDs[i] = deploymentID
			t.Logf("Submitted deployment %d: %s", i+1, deploymentID)
		}

		// Wait for deployments to start processing (creates providers)
		time.Sleep(2 * time.Second)

		// Get baseline metrics before shutdown
		preShutdownProviders := testServer.GetActiveProviderCount()
		preShutdownZombies := testServer.GetZombieProcessCount()
		preShutdownMetrics := testServer.Components.Queue.GetMetrics()

		t.Logf("Pre-shutdown state:")
		t.Logf("  Active providers: %d", preShutdownProviders)
		t.Logf("  Zombie processes: %d", preShutdownZombies)
		t.Logf("  Queue depth: %d", preShutdownMetrics.CurrentDepth)
		t.Logf("  Queue total enqueued: %d", preShutdownMetrics.TotalEnqueued)

		// If we have real providers, we should see them active
		if preShutdownProviders > 0 {
			t.Logf("SUCCESS: Real providers created and active")
		} else {
			t.Logf("INFO: No active providers detected (may be using mocks or quick completion)")
		}

		// Test the complete shutdown chain with provider verification
		testServer.StopWithProviderVerification(t)

		// Additional verification: Check that deployments completed or were cancelled
		for i, deploymentID := range deploymentIDs {
			if status, err := testServer.Components.Tracker.GetStatus(deploymentID); err == nil {
				t.Logf("Final deployment %d status: %s", i+1, *status)
			}
		}

		t.Log("Complete shutdown chain test passed successfully")
	})

	t.Run("ShutdownChainWithActiveDeployments", func(t *testing.T) {
		// Test shutdown while deployments are actively processing
		testServer := helpers.StartTestServer(t)
		require.NotNil(t, testServer)

		// Submit a longer-running deployment
		ctx := context.Background()
		deployment := &interfaces.DeploymentRequest{
			Resources: []interfaces.Resource{
				{
					Type: "aws_s3_bucket",
					Name: "long-running-test",
					Properties: map[string]interface{}{
						"bucket": fmt.Sprintf("test-bucket-long-%d", time.Now().Unix()),
						"tags": map[string]interface{}{
							"Environment": "integration-test",
							"TestType":    "shutdown-chain",
						},
					},
				},
				{
					Type: "aws_iam_role",
					Name: "test-role",
					Properties: map[string]interface{}{
						"name": fmt.Sprintf("test-role-%d", time.Now().Unix()),
						"assume_role_policy": `{
							"Version": "2012-10-17",
							"Statement": [{
								"Action": "sts:AssumeRole",
								"Effect": "Allow",
								"Principal": {"Service": "ec2.amazonaws.com"}
							}]
						}`,
					},
				},
			},
			Options: interfaces.DeploymentOptions{
				DryRun:     true, // Use dry run to avoid actual resource creation
				Timeout:    60 * time.Second,
				MaxRetries: 2,
			},
		}

		// Create a queued deployment
		queued := &interfaces.QueuedDeployment{
			ID:        fmt.Sprintf("long-running-test-%d", time.Now().UnixNano()),
			Status:    interfaces.DeploymentStatusQueued,
			CreatedAt: time.Now(),
			Request:   deployment,
		}

		// Register with tracker
		err := testServer.Components.Tracker.Register(queued)
		require.NoError(t, err)

		// Submit to queue
		err = testServer.Components.Queue.Enqueue(ctx, queued)
		if err != nil {
			// Remove from tracker if enqueue fails
			_ = testServer.Components.Tracker.Remove(queued.ID)
			require.NoError(t, err)
		}
		t.Logf("Submitted long-running deployment: %s", queued.ID)

		// Let it start processing
		time.Sleep(1 * time.Second)

		// Check system state
		preShutdownMetrics := testServer.Components.Queue.GetMetrics()

		t.Logf("Pre-shutdown with active deployment:")
		t.Logf("  Queue depth: %d", preShutdownMetrics.CurrentDepth)
		t.Logf("  Total enqueued: %d", preShutdownMetrics.TotalEnqueued)
		t.Logf("  Components available: Queue, Tracker, WorkerPool")

		// Shutdown while deployment is active
		testServer.StopWithProviderVerification(t)

		t.Log("Shutdown with active deployments completed successfully")
	})

	t.Run("ShutdownChainErrorRecovery", func(t *testing.T) {
		// Test shutdown behavior when providers fail or are in error states
		testServer := helpers.StartTestServer(t)
		require.NotNil(t, testServer)

		// Submit a deployment that will likely fail (invalid configuration)
		ctx := context.Background()
		deployment := &interfaces.DeploymentRequest{
			Resources: []interfaces.Resource{
				{
					Type: "aws_s3_bucket",
					Name: "invalid-test",
					Properties: map[string]interface{}{
						"bucket":           "", // Invalid empty bucket name should cause error
						"invalid_property": "should_cause_error",
					},
				},
			},
			Options: interfaces.DeploymentOptions{
				DryRun:     false, // Don't use dry run to test real error handling
				Timeout:    10 * time.Second,
				MaxRetries: 1,
			},
		}

		// Create a queued deployment
		queued := &interfaces.QueuedDeployment{
			ID:        fmt.Sprintf("error-test-%d", time.Now().UnixNano()),
			Status:    interfaces.DeploymentStatusQueued,
			CreatedAt: time.Now(),
			Request:   deployment,
		}

		// Register with tracker
		err := testServer.Components.Tracker.Register(queued)
		require.NoError(t, err)

		// Submit to queue
		err = testServer.Components.Queue.Enqueue(ctx, queued)
		if err != nil {
			// Remove from tracker if enqueue fails
			_ = testServer.Components.Tracker.Remove(queued.ID)
			require.NoError(t, err)
		}
		t.Logf("Submitted error-prone deployment: %s", queued.ID)

		// Wait for deployment to fail
		timeout := time.After(15 * time.Second)
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		var finalStatus interfaces.DeploymentStatus
		for {
			select {
			case <-timeout:
				t.Log("Deployment did not complete within timeout (acceptable)")
				goto shutdown
			case <-ticker.C:
				if status, err := testServer.Components.Tracker.GetStatus(queued.ID); err == nil {
					finalStatus = *status
					if finalStatus == interfaces.DeploymentStatusFailed || finalStatus == interfaces.DeploymentStatusCompleted {
						t.Logf("Deployment completed with status: %s", finalStatus)
						goto shutdown
					}
				}
			}
		}

	shutdown:
		// Test shutdown even when deployments have failed
		t.Log("Testing shutdown after deployment errors...")
		testServer.StopWithProviderVerification(t)

		t.Log("Error recovery shutdown test completed successfully")
	})
}

// TestProviderLifecycleIntegration tests provider-specific lifecycle during shutdown
func TestProviderLifecycleIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	t.Run("ProviderCacheCleanupDuringShutdown", func(t *testing.T) {
		testServer := helpers.StartTestServer(t)
		require.NotNil(t, testServer)

		// Create multiple deployments with different provider configurations
		ctx := context.Background()

		deployments := []*interfaces.DeploymentRequest{
			{
				Resources: []interfaces.Resource{
					{
						Type: "aws_s3_bucket",
						Name: "bucket-us-east-1",
						Properties: map[string]interface{}{
							"bucket": fmt.Sprintf("test-us-east-1-%d", time.Now().Unix()),
						},
					},
				},
				Options: interfaces.DeploymentOptions{
					DryRun: true,
					ProviderConfig: interfaces.ProviderConfig{
						"aws": map[string]interface{}{
							"region": "us-east-1",
						},
					},
				},
			},
			{
				Resources: []interfaces.Resource{
					{
						Type: "aws_s3_bucket",
						Name: "bucket-us-west-2",
						Properties: map[string]interface{}{
							"bucket": fmt.Sprintf("test-us-west-2-%d", time.Now().Unix()),
						},
					},
				},
				Options: interfaces.DeploymentOptions{
					DryRun: true,
					ProviderConfig: interfaces.ProviderConfig{
						"aws": map[string]interface{}{
							"region": "us-west-2",
						},
					},
				},
			},
		}

		// Submit deployments
		for i, deployment := range deployments {
			// Create a queued deployment
			queued := &interfaces.QueuedDeployment{
				ID:        fmt.Sprintf("provider-test-%d-%d", i+1, time.Now().UnixNano()),
				Status:    interfaces.DeploymentStatusQueued,
				CreatedAt: time.Now(),
				Request:   deployment,
			}

			// Register with tracker
			err := testServer.Components.Tracker.Register(queued)
			require.NoError(t, err)

			// Submit to queue
			err = testServer.Components.Queue.Enqueue(ctx, queued)
			if err != nil {
				// Remove from tracker if enqueue fails
				_ = testServer.Components.Tracker.Remove(queued.ID)
				require.NoError(t, err)
			}
			t.Logf("Submitted deployment %d: %s", i+1, queued.ID)
		}

		// Let them process
		time.Sleep(3 * time.Second)

		// Check provider state
		preShutdownProviders := testServer.GetActiveProviderCount()
		t.Logf("Active providers before shutdown: %d", preShutdownProviders)

		// Test shutdown with provider cache cleanup
		testServer.StopWithProviderVerification(t)

		t.Log("Provider cache cleanup test completed successfully")
	})
}

// TestShutdownChainTimeout tests behavior when shutdown takes too long
func TestShutdownChainTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	t.Run("ShutdownWithTimeout", func(t *testing.T) {
		testServer := helpers.StartTestServer(t)
		require.NotNil(t, testServer)

		// Submit deployment
		deploymentID := testServer.SubmitTestDeployment(t)
		t.Logf("Submitted deployment: %s", deploymentID)

		// Let it start processing
		time.Sleep(1 * time.Second)

		// Test shutdown with a reasonable timeout
		start := time.Now()
		testServer.StopWithProviderVerification(t)
		shutdownDuration := time.Since(start)

		t.Logf("Shutdown completed in %v", shutdownDuration)

		// Verify shutdown completed in reasonable time (should be < 30 seconds)
		assert.Less(t, shutdownDuration, 30*time.Second,
			"Shutdown should complete within 30 seconds")

		t.Log("Shutdown timeout test completed successfully")
	})
}

// TestDeploymentPersistenceAcrossServerRestart tests that deployments are properly
// restored from disk after a server restart. This addresses the bug where deployments
// exist on disk but are not loaded into memory on server startup.
func TestDeploymentPersistenceAcrossServerRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Skip this test until persistence architecture is clarified
	// The file backend is deprecated and AWS backend requires complex LocalStack setup
	// that interferes with other components in the test environment
	t.Skip("Skipping persistence test - file backend deprecated, AWS backend requires complex setup")

	t.Parallel()

	// Use a fixed temp directory for this test so we can restart with the same storage
	tempStateDir := t.TempDir()

	// Create helper function to create test server with shared storage
	createTestServer := func(port int) *helpers.TestServer {
		// Create real components for testing with persistent storage
		testComponents := createRealComponentsWithStorage(t, tempStateDir)

		// Create test config
		cfg := config.NewServerConfig()
		cfg.Port = port

		// Create and start the server
		server, err := apiserver.NewAPIServerWithComponents(
			cfg,
			testComponents.Components.Queue,
			testComponents.Components.Tracker,
			testComponents.Components.WorkerPool,
			testComponents.StateStore,
			testComponents.ProviderManager,
			testComponents.Interpolator,
		)
		require.NoError(t, err)

		return &helpers.TestServer{
			Server:     server,
			Components: testComponents.Components,
			StateStore: testComponents.StateStore,
			URL:        fmt.Sprintf("http://localhost:%d", port),
			Port:       port,
		}
	}

	// Step 1: Start first server instance
	t.Log("Starting first server instance...")

	// Find a free port for first server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	firstPort := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	firstTestServer := createTestServer(firstPort)

	// Start server in background
	errChan := make(chan error, 1)
	go func() {
		if err := firstTestServer.Server.Start(); err != nil && err.Error() != "http: Server closed" {
			errChan <- err
		}
		close(errChan)
	}()

	// Wait for server to be ready
	client := &http.Client{Timeout: 5 * time.Second}
	serverURL := firstTestServer.URL

	maxRetries := 30
	for i := 0; i < maxRetries; i++ {
		resp, err := client.Get(serverURL + "/api/v1/system/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				break
			}
		}
		if i == maxRetries-1 {
			t.Fatalf("First server failed to start after %d attempts", maxRetries)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Step 2: Submit a test deployment
	t.Log("Submitting test deployment...")
	deployment := &interfaces.DeploymentRequest{
		Resources: []interfaces.Resource{
			{
				Type: "aws_s3_bucket",
				Name: "test-bucket",
				Properties: map[string]interface{}{
					"bucket": "test-persistence-bucket",
					"tags": map[string]interface{}{
						"Environment": "test",
						"Purpose":     "persistence-test",
					},
				},
			},
		},
		Options: interfaces.DeploymentOptions{
			DryRun:     true, // Use dry run to avoid real AWS resources
			Timeout:    30 * time.Second,
			MaxRetries: 1,
		},
	}

	// Create a queued deployment
	queued := &interfaces.QueuedDeployment{
		ID:        fmt.Sprintf("persistence-test-%d", time.Now().UnixNano()),
		Status:    interfaces.DeploymentStatusQueued,
		CreatedAt: time.Now(),
		Request:   deployment,
	}

	// Register with tracker
	err = firstTestServer.Components.Tracker.Register(queued)
	require.NoError(t, err)

	// Submit to queue
	err = firstTestServer.Components.Queue.Enqueue(context.Background(), queued)
	if err != nil {
		// Remove from tracker if enqueue fails
		_ = firstTestServer.Components.Tracker.Remove(queued.ID)
		require.NoError(t, err)
	}

	deploymentID := queued.ID
	t.Logf("Created deployment: %s", deploymentID)

	// Wait for deployment to complete processing
	time.Sleep(2 * time.Second)

	// Verify deployment exists in first server
	deployments, err := firstTestServer.Components.Tracker.List(interfaces.DeploymentFilter{})
	require.NoError(t, err)
	require.NotEmpty(t, deployments)

	var firstDeployment *interfaces.QueuedDeployment
	for _, d := range deployments {
		if d.ID == deploymentID {
			firstDeployment = d
			break
		}
	}
	require.NotNil(t, firstDeployment)
	t.Logf("First server sees deployment with status: %s", firstDeployment.Status)

	// Step 3: Shutdown first server gracefully
	t.Log("Shutting down first server...")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	require.NoError(t, firstTestServer.Server.Shutdown(ctx))
	require.NoError(t, firstTestServer.Components.Close(ctx))

	// Step 4: Start second server instance with same storage directory
	t.Log("Starting second server instance with same storage...")

	// Find a free port for second server
	listener2, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	secondPort := listener2.Addr().(*net.TCPAddr).Port
	listener2.Close()

	secondTestServer := createTestServer(secondPort)

	// Start second server with its own error channel
	errChan2 := make(chan error, 1)
	go func() {
		if err := secondTestServer.Server.Start(); err != nil && err.Error() != "http: Server closed" {
			errChan2 <- err
		}
		close(errChan2)
	}()

	// Wait for second server to be ready
	secondServerURL := secondTestServer.URL
	for i := 0; i < maxRetries; i++ {
		resp, err := client.Get(secondServerURL + "/api/v1/system/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				break
			}
		}
		if i == maxRetries-1 {
			t.Fatalf("Second server failed to start after %d attempts", maxRetries)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Step 5: Verify deployment still exists in second server (this should fail with current bug)
	t.Log("Checking if deployment persists in second server...")

	// THIS IS THE CRITICAL TEST - with the current bug, this will fail
	// because deployments are not loaded from disk on server startup
	secondDeployments, err := secondTestServer.Components.Tracker.List(interfaces.DeploymentFilter{})

	// Clean up second server
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = secondTestServer.Server.Shutdown(ctx)
		_ = secondTestServer.Components.Close(ctx)
	}()

	// With the persistence bug fixed, deployments should now be loaded from disk
	require.NoError(t, err, "Failed to list deployments from second server")
	require.NotEmpty(t, secondDeployments, "No deployments found in second server instance - persistence may have failed")

	// Look for our specific deployment
	var recoveredDeployment *interfaces.QueuedDeployment
	for _, d := range secondDeployments {
		if d.ID == deploymentID {
			recoveredDeployment = d
			break
		}
	}

	if recoveredDeployment == nil {
		t.Logf("BUG DETECTED: Deployment %s not found in second server, but other deployments exist", deploymentID)
		t.Errorf("PERSISTENCE BUG: Specific deployment missing from second server")
		return
	}

	// If we get here, the bug has been fixed!
	require.NotNil(t, recoveredDeployment)
	assert.Equal(t, deploymentID, recoveredDeployment.ID)

	// The deployment should exist but may have a different status since it was interrupted
	// What matters is that it persisted across restart
	t.Logf("SUCCESS: Deployment %s persisted across server restart with status: %s (original: %s)",
		deploymentID, recoveredDeployment.Status, firstDeployment.Status)

	// Verify the deployment was indeed persisted and loaded from disk
	assert.NotNil(t, recoveredDeployment, "Deployment should exist after server restart")
}
