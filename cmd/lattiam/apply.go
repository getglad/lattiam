//nolint:forbidigo // CLI command needs fmt.Print* for user output
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lattiam/lattiam/internal/apiserver/types"
	"github.com/lattiam/lattiam/internal/config"
	"github.com/lattiam/lattiam/internal/events"
	"github.com/lattiam/lattiam/internal/executor"
	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/system"
)

// Static errors for err113 compliance
var (
	ErrSomeResourcesFailedToDeploy = errors.New("some resources failed to deploy")
)

func runApply(filename string, dryRun bool, providerVersion string) error {
	// Load and parse the terraform JSON
	terraformJSON, err := loadTerraformJSON(filename)
	if err != nil {
		return err
	}

	// Validate the terraform JSON
	if err := validateTerraformJSON(terraformJSON); err != nil {
		return err
	}

	// Set up the worker system (same as API server)
	components, err := setupWorkerSystem()
	if err != nil {
		return err
	}
	defer func() {
		ctx := context.Background()
		if err := components.Components.Close(ctx); err != nil {
			fmt.Printf("Warning: failed to shutdown components: %v\n", err)
		}
	}()

	// Log the start of deployment
	fmt.Printf("Applying infrastructure from %s\n", filename)

	// Execute the deployment using the components
	return executeDeploymentWithComponents(components, terraformJSON, dryRun, providerVersion)
}

// loadTerraformJSON loads and parses a JSON file
func loadTerraformJSON(filename string) (map[string]interface{}, error) {
	data, err := os.ReadFile(filename) // #nosec G304 - filename is validated CLI argument
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return result, nil
}

// validateTerraformJSON validates the structure of terraform JSON
func validateTerraformJSON(terraformJSON map[string]interface{}) error {
	// Use the RequestConverter to parse and validate
	converter := types.NewRequestConverterWithDefaults()
	_, _, err := converter.ParseTerraformJSON(terraformJSON)
	if err != nil {
		return fmt.Errorf("failed to parse terraform JSON: %w", err)
	}
	return nil
}

// WorkerSystemComponents holds the components needed for deployment
type WorkerSystemComponents struct {
	Queue           interfaces.DeploymentQueue
	Tracker         interfaces.DeploymentTracker
	WorkerPool      interfaces.WorkerPool
	StateStore      interfaces.StateStore
	ProviderManager interfaces.ProviderLifecycleManager
	Components      *system.BackgroundSystemComponents
}

// setupWorkerSystem creates the worker system components with CLI-appropriate configuration
func setupWorkerSystem() (*WorkerSystemComponents, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	stateDir := filepath.Join(homeDir, ".lattiam", "state")
	providerDir := filepath.Join(homeDir, ".lattiam", "providers")

	// Ensure directories exist
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create state directory: %w", err)
	}
	if err := os.MkdirAll(providerDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create provider directory: %w", err)
	}

	// Create configuration for CLI
	cfg := config.NewServerConfig()
	cfg.StateStore.Type = "file"
	cfg.StateStore.File.Path = filepath.Join(stateDir, "state.db")
	cfg.ProviderDir = providerDir
	cfg.Queue.Type = "embedded" // Use embedded mode for CLI

	// Create state store
	stateStore, err := createStateStore(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create state store: %w", err)
	}

	// Create provider manager
	providerManager, err := createProviderManager(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider manager: %w", err)
	}

	// Create event bus
	eventBus := events.NewEventBus()

	// Create deployment executor with the event bus
	deploymentExecutor := executor.NewWithEventBus(providerManager, stateStore, eventBus, 10*time.Minute)
	executorFunc := executor.CreateExecutorFunc(deploymentExecutor)

	// Create background system using factory
	components, err := system.NewBackgroundSystem(cfg, executorFunc)
	if err != nil {
		return nil, fmt.Errorf("failed to create background system: %w", err)
	}

	// Connect tracker to event bus
	events.ConnectTrackerToEventBus(eventBus, components.Tracker)

	// Start the worker pool
	components.WorkerPool.Start()

	return &WorkerSystemComponents{
		Queue:           components.Queue,
		Tracker:         components.Tracker,
		WorkerPool:      components.WorkerPool,
		StateStore:      stateStore,
		ProviderManager: providerManager,
		Components:      components,
	}, nil
}

// executeDeploymentWithComponents executes deployment using the components directly
func executeDeploymentWithComponents(components *WorkerSystemComponents, terraformJSON map[string]interface{}, dryRun bool, providerVersion string) error {
	// Parse terraform JSON to resources and data sources
	converter := types.NewRequestConverterWithDefaults()
	if dryRun {
		defaults := converter.GetDefaults()
		defaults.DryRun = true
		converter.UpdateDefaults(defaults)
	}

	resources, dataSources, err := converter.ParseTerraformJSON(terraformJSON)
	if err != nil {
		return fmt.Errorf("failed to parse terraform JSON: %w", err)
	}

	// Create deployment request
	deploymentReq := &interfaces.DeploymentRequest{
		Resources:   resources,
		DataSources: dataSources,
		Options: interfaces.DeploymentOptions{
			DryRun:     dryRun,
			Timeout:    30 * time.Minute,
			MaxRetries: 3,
		},
		Metadata: map[string]interface{}{
			"source":         "cli",
			"terraform_json": terraformJSON,
		},
	}

	// Apply provider version override if specified
	if providerVersion != "" {
		deploymentReq.Metadata[interfaces.MetadataKeyProviderVersion] = providerVersion
	}

	// Create a queued deployment
	queuedDeployment := &interfaces.QueuedDeployment{
		ID:        fmt.Sprintf("deployment-%d", time.Now().UnixNano()),
		Status:    interfaces.DeploymentStatusQueued,
		CreatedAt: time.Now(),
		Request:   deploymentReq,
	}

	// Register with tracker
	if err := components.Tracker.Register(queuedDeployment); err != nil {
		return fmt.Errorf("failed to register deployment: %w", err)
	}

	// Submit deployment to queue
	ctx := context.Background()
	if err := components.Queue.Enqueue(ctx, queuedDeployment); err != nil {
		// Remove from tracker if enqueue fails
		_ = components.Tracker.Remove(queuedDeployment.ID)
		return fmt.Errorf("failed to enqueue deployment: %w", err)
	}

	fmt.Printf("Deployment submitted: %s\n", queuedDeployment.ID)

	// Wait for deployment to complete
	return waitForDeploymentCompletion(components, queuedDeployment.ID)
}

// waitForDeploymentCompletion monitors deployment progress and reports results
//
//nolint:gocognit,funlen,gocyclo // Complex deployment monitoring logic
func waitForDeploymentCompletion(components *WorkerSystemComponents, deploymentID string) error {
	checkInterval := 1 * time.Second
	maxWaitTime := 30 * time.Minute
	startTime := time.Now()

	for {
		if time.Since(startTime) > maxWaitTime {
			return fmt.Errorf("deployment timed out after %v", maxWaitTime)
		}

		// Get deployment from tracker
		filter := interfaces.DeploymentFilter{}
		deployments, err := components.Tracker.List(filter)
		if err != nil {
			return fmt.Errorf("failed to list deployments: %w", err)
		}

		// Find our deployment
		var queuedDeployment *interfaces.QueuedDeployment
		for _, qd := range deployments {
			if qd.ID == deploymentID {
				queuedDeployment = qd
				break
			}
		}

		if queuedDeployment == nil {
			return fmt.Errorf("deployment %s not found", deploymentID)
		}

		// Print status update
		fmt.Printf("Status: %s\n", queuedDeployment.Status)

		switch queuedDeployment.Status {
		case interfaces.DeploymentStatusCompleted:
			fmt.Println("✓ Deployment completed successfully")

			// Get and display deployment metadata
			if components.StateStore != nil {
				ctx := context.Background()
				deploymentMetadata, err := components.StateStore.GetDeployment(ctx, deploymentID)
				if err == nil && deploymentMetadata != nil {
					printDeploymentMetadata(deploymentMetadata)
				}
			}
			return nil

		case interfaces.DeploymentStatusFailed:
			fmt.Println("✗ Deployment failed")

			// Get detailed error information
			if queuedDeployment.LastError != nil {
				fmt.Printf("Error: %v\n", queuedDeployment.LastError)
			}

			// Get deployment metadata to show status
			if components.StateStore != nil {
				ctx := context.Background()
				deploymentMetadata, err := components.StateStore.GetDeployment(ctx, deploymentID)
				if err == nil && deploymentMetadata != nil {
					printDeploymentMetadata(deploymentMetadata)
				}
			}

			return ErrSomeResourcesFailedToDeploy

		case interfaces.DeploymentStatusProcessing:
			// Show progress if we have metadata
			if queuedDeployment.Request != nil {
				totalResources := len(queuedDeployment.Request.Resources) + len(queuedDeployment.Request.DataSources)
				fmt.Printf("Processing %d resources/data sources...\n", totalResources)
			}

		case interfaces.DeploymentStatusQueued:
			// Deployment is still in queue
			fmt.Println("Deployment is queued...")

		case interfaces.DeploymentStatusCanceled:
			fmt.Println("✗ Deployment was canceled")
			return fmt.Errorf("deployment canceled")

		case interfaces.DeploymentStatusCanceling:
			fmt.Println("Deployment is being canceled...")

		case interfaces.DeploymentStatusDestroying:
			fmt.Println("Resources are being destroyed...")

		case interfaces.DeploymentStatusDestroyed:
			fmt.Println("✓ Resources destroyed successfully")
			return nil

		case interfaces.DeploymentStatusDeleted:
			fmt.Println("✗ Deployment was deleted")
			return fmt.Errorf("deployment deleted")
		}

		time.Sleep(checkInterval)
		// Exponential backoff for check interval
		if checkInterval < 5*time.Second {
			checkInterval *= 2
		}
	}
}

// printDeploymentMetadata displays the deployment metadata
func printDeploymentMetadata(metadata *interfaces.DeploymentMetadata) {
	if metadata == nil {
		return
	}

	fmt.Printf("\nDeployment: %s\n", metadata.DeploymentID)
	fmt.Printf("Status: %s\n", metadata.Status)
	fmt.Printf("Resource Count: %d\n", metadata.ResourceCount)
	fmt.Printf("Created: %s\n", metadata.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("Updated: %s\n", metadata.UpdatedAt.Format("2006-01-02 15:04:05"))

	if metadata.LastAppliedAt != nil {
		fmt.Printf("Last Applied: %s\n", metadata.LastAppliedAt.Format("2006-01-02 15:04:05"))
	}

	if metadata.ErrorMessage != "" {
		fmt.Printf("Error: %s\n", metadata.ErrorMessage)
	}
}
