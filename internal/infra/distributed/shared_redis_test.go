package distributed

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/infra/distributed/testutil"
)

// TestSharedRedisSetup verifies that shared Redis with database isolation works
func TestSharedRedisSetup(t *testing.T) {
	t.Parallel()
	// Get first Redis setup with database isolation
	setup1 := testutil.SetupSharedRedis(t)
	require.NotNil(t, setup1)
	require.NotEmpty(t, setup1.URL)

	// Get second Redis setup - should reuse container but different DB
	setup2 := testutil.SetupSharedRedis(t)
	require.NotNil(t, setup2)
	require.NotEmpty(t, setup2.URL)

	// Database numbers should be different
	t.Logf("Redis 1: URL=%s, DB=%d", setup1.URL, setup1.DB)
	t.Logf("Redis 2: URL=%s, DB=%d", setup2.URL, setup2.DB)

	// Both should have valid URLs
	assert.Contains(t, setup1.URL, "redis://")
	assert.Contains(t, setup2.URL, "redis://")

	// Database numbers should be different (isolation)
	assert.NotEqual(t, setup1.DB, setup2.DB, "Should have different database numbers for isolation")
}

// TestSharedRedisIsolation verifies database isolation between sub-tests
func TestSharedRedisIsolation(t *testing.T) {
	t.Parallel()
	// Each sub-test gets its own database
	t.Run("Database1", func(t *testing.T) {
		t.Parallel()
		setup := testutil.SetupSharedRedis(t)
		require.NotNil(t, setup)
		t.Logf("Database 1: URL=%s, DB=%d", setup.URL, setup.DB)

		// Verify we got a valid database number
		assert.GreaterOrEqual(t, setup.DB, 0)
		assert.LessOrEqual(t, setup.DB, 15)
	})

	t.Run("Database2", func(t *testing.T) {
		t.Parallel()
		setup := testutil.SetupSharedRedis(t)
		require.NotNil(t, setup)
		t.Logf("Database 2: URL=%s, DB=%d", setup.URL, setup.DB)

		// Verify we got a valid database number
		assert.GreaterOrEqual(t, setup.DB, 0)
		assert.LessOrEqual(t, setup.DB, 15)
	})
}
