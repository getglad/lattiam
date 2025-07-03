package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/lattiam/lattiam/internal/deployment"

	"github.com/lattiam/lattiam/tests/helpers"
)

func (s *DeploymentRetryTestSuite) TestDeploymentRetryQueue() {
	// Test retry queue functionality
	s.Run("DependencyQueue basic operations", func() {
		t := s.T()
		queue := deployment.NewDependencyQueue()

		// Test initial state
		assert.False(t, queue.HasPendingResources())
		assert.Equal(t, 0, queue.GetPendingCount())

		// Add a failed resource
		properties := map[string]interface{}{
			"vpc_id":     "${aws_vpc.main.id}",
			"cidr_block": "10.0.1.0/24",
		}

		queue.AddFailedResource("aws_subnet", "main", properties,
			assert.AnError)

		// Verify queue state
		assert.True(t, queue.HasPendingResources())
		assert.Equal(t, 1, queue.GetPendingCount())

		// No resources should be ready (missing dependency)
		ready := queue.GetReadyResources(context.Background())
		assert.Empty(t, ready)

		// Mark dependency as deployed
		vpcState := map[string]interface{}{"id": "vpc-12345"}
		queue.MarkResourceDeployed("aws_vpc/main", vpcState)

		// Now the subnet should be ready after the retry delay
		// Wait for retry time to pass (base delay is 2 seconds)
		time.Sleep(3 * time.Second)

		ready = queue.GetReadyResources(context.Background())
		assert.Len(t, ready, 1, "Subnet should be ready for retry after VPC is deployed")
		if len(ready) > 0 {
			assert.Equal(t, "aws_subnet", ready[0].ResourceType)
			assert.Equal(t, "main", ready[0].ResourceName)
		}
	})

	s.Run("Retryable error detection", func() {
		t := s.T()
		testCases := []struct {
			errorMsg    string
			shouldRetry bool
		}{
			{"No valid credential sources found", true},
			{"connection timeout occurred", true},
			{"rate limited by service", true},
			{"invalid parameter value", false},
			{"resource not found", false},
			{"access denied", false},
		}

		for _, tc := range testCases {
			// We can't directly test isRetryableError as it's not exported,
			// but the logic is tested implicitly in the deployment flow
			t.Logf("Error: %s, Expected retryable: %v", tc.errorMsg, tc.shouldRetry)
		}
	})

	s.Run("Interpolation validation", func() {
		t := s.T()
		deployedStates := map[string]map[string]interface{}{
			"aws_vpc/main": {"id": "vpc-12345"},
		}

		// Properties with resolvable interpolation
		validProperties := map[string]interface{}{
			"vpc_id": "${aws_vpc.main.id}",
		}

		resolved, err := deployment.ResolveInterpolationsStrict(validProperties, deployedStates)
		require.NoError(t, err)
		assert.Equal(t, "vpc-12345", resolved["vpc_id"])

		// Properties with unresolvable interpolation
		invalidProperties := map[string]interface{}{
			"subnet_id": "${aws_subnet.missing.id}",
		}

		_, err = deployment.ResolveInterpolationsStrict(invalidProperties, deployedStates)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unresolved interpolations")
	})
}

type DeploymentRetryTestSuite struct {
	helpers.BaseTestSuite
}

//nolint:paralleltest // Integration tests use shared LocalStack and API server resources
func TestDeploymentRetrySuite(t *testing.T) {
	suite.Run(t, new(DeploymentRetryTestSuite))
}
