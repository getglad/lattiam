// Package testutil provides test utilities and factories for distributed infrastructure testing.
package testutil

import (
	"fmt"
	"time"

	"github.com/lattiam/lattiam/internal/interfaces"
)

// CreateTestDeployment creates a test deployment with reasonable defaults
func CreateTestDeployment(id string) *interfaces.QueuedDeployment {
	return &interfaces.QueuedDeployment{
		ID:        id,
		Status:    interfaces.DeploymentStatusQueued,
		CreatedAt: time.Now(),
		Request: &interfaces.DeploymentRequest{
			Resources: []interfaces.Resource{
				{
					Type: "test_resource",
					Name: fmt.Sprintf("resource-%s", id),
					Properties: map[string]interface{}{
						"test": true,
						"id":   id,
					},
				},
			},
			DataSources: []interfaces.DataSource{},
			Options: interfaces.DeploymentOptions{
				DryRun:     false,
				Timeout:    5 * time.Minute,
				MaxRetries: 3,
			},
			Metadata: map[string]interface{}{
				"test": true,
			},
		},
	}
}

// CreateTestDeploymentWithResources creates a deployment with multiple resources
func CreateTestDeploymentWithResources(id string, resourceCount int) *interfaces.QueuedDeployment {
	deployment := CreateTestDeployment(id)
	deployment.Request.Resources = make([]interfaces.Resource, resourceCount)

	for i := 0; i < resourceCount; i++ {
		deployment.Request.Resources[i] = interfaces.Resource{
			Type: "test_resource",
			Name: fmt.Sprintf("resource-%s-%d", id, i),
			Properties: map[string]interface{}{
				"test":  true,
				"id":    id,
				"index": i,
			},
		}
	}

	return deployment
}

// CreateTestDataSource creates a test data source
func CreateTestDataSource(name string) interfaces.DataSource {
	return interfaces.DataSource{
		Type: "test_data",
		Name: name,
		Properties: map[string]interface{}{
			"test": true,
			"name": name,
		},
	}
}

// CreateLargeDeployment creates a deployment with many resources for load testing
func CreateLargeDeployment(id string) *interfaces.QueuedDeployment {
	deployment := CreateTestDeployment(id)

	// Add 10 data sources
	deployment.Request.DataSources = make([]interfaces.DataSource, 10)
	for i := 0; i < 10; i++ {
		deployment.Request.DataSources[i] = CreateTestDataSource(fmt.Sprintf("data-%s-%d", id, i))
	}

	// Add 50 resources
	deployment.Request.Resources = make([]interfaces.Resource, 50)
	for i := 0; i < 50; i++ {
		deployment.Request.Resources[i] = interfaces.Resource{
			Type: "test_resource",
			Name: fmt.Sprintf("resource-%s-%d", id, i),
			Properties: map[string]interface{}{
				"test":       true,
				"id":         id,
				"index":      i,
				"large_data": fmt.Sprintf("This is some test data for resource %d in deployment %s", i, id),
			},
		}
	}

	return deployment
}
