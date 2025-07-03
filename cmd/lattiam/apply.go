package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lattiam/lattiam/internal/deployment"
)

// Static errors for err113 compliance
var (
	ErrSomeResourcesFailedToDeploy = errors.New("some resources failed to deploy")
	ErrGetCommandNotImplemented    = errors.New("get command not yet implemented")
	ErrDeleteCommandNotImplemented = errors.New("delete command not yet implemented")
	ErrListCommandNotImplemented   = errors.New("list command not yet implemented")
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

	// Set up the deployment environment
	deployer, logger, err := setupDeploymentEnvironment()
	if err != nil {
		return err
	}
	defer func() {
		if err := deployer.Close(); err != nil {
			logger.Errorf("Failed to close deployer: %v", err)
		}
	}()

	// Log the start of deployment
	fmt.Printf("Applying infrastructure from %s\n", filename)

	// Execute the deployment
	return executeDeployment(deployer, terraformJSON, dryRun, providerVersion, logger)
}

// loadTerraformJSON reads and parses the terraform JSON from a file
func loadTerraformJSON(filename string) (map[string]interface{}, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var wrapperJSON struct {
		Name          string                 `json:"name"`
		TerraformJSON map[string]interface{} `json:"terraform_json"`
	}
	if err := json.Unmarshal(data, &wrapperJSON); err != nil {
		return nil, fmt.Errorf("failed to parse JSON structure: %w", err)
	}

	if wrapperJSON.TerraformJSON == nil {
		return nil, errors.New("missing terraform_json field in input file")
	}

	return wrapperJSON.TerraformJSON, nil
}

// validateTerraformJSON validates the terraform JSON structure
func validateTerraformJSON(terraformJSON map[string]interface{}) error {
	terraformData, err := json.Marshal(terraformJSON)
	if err != nil {
		return fmt.Errorf("failed to marshal terraform JSON: %w", err)
	}
	_, err = deployment.ParseTerraformJSONComplete(terraformData)
	if err != nil {
		return fmt.Errorf("failed to parse JSON: %w", err)
	}
	return nil
}

// setupDeploymentEnvironment creates the provider directory and deployer
func setupDeploymentEnvironment() (*deployment.Deployer, deployment.Logger, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get home directory: %w", err)
	}
	providerDir := filepath.Join(homeDir, ".lattiam", "providers")
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("failed to create provider directory: %w", err)
	}

	logger := newCLILogger()
	deployer, err := deployment.NewDeployer(providerDir, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create deployer: %w", err)
	}

	return deployer, logger, nil
}

// executeDeployment performs the actual deployment and checks results
func executeDeployment(deployer *deployment.Deployer, terraformJSON map[string]interface{}, dryRun bool, providerVersion string, logger deployment.Logger) error {
	opts := deployment.Options{
		DryRun:          dryRun,
		ProviderVersion: providerVersion,
		Logger:          logger,
		TerraformJSON:   terraformJSON,
	}

	ctx := context.Background()
	stateFile := "" // CLI doesn't use state files yet
	results, err := deployer.DeployResourcesWithPlan(ctx, "cli-deployment", terraformJSON, stateFile, opts)
	if err != nil {
		return fmt.Errorf("failed to deploy: %w", err)
	}

	// Check if any resources failed
	failureCount := 0
	for _, result := range results {
		if result.Error != nil {
			failureCount++
		} else if !dryRun && result.State != nil {
			// Show resource state for successful deployments
			// Resource State:
			_ = result.State // Resource state available
		}
	}

	if failureCount > 0 {
		return ErrSomeResourcesFailedToDeploy
	}

	return nil
}

func runGet(resourceType, name string) error {
	_ = resourceType // Resource type specified
	_ = name         // Resource name specified
	// FUTURE: Implement state reading and resource retrieval
	return ErrGetCommandNotImplemented
}

func runDelete(resourceType, name string) error {
	_ = resourceType // Resource type specified
	_ = name         // Resource name specified
	// FUTURE: Implement resource deletion
	return ErrDeleteCommandNotImplemented
}

func runList(resourceType string) error {
	_ = resourceType // Resource type specified (or empty for all)
	// FUTURE: Implement state listing
	return ErrListCommandNotImplemented
}
