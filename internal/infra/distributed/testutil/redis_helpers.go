package testutil

import (
	"strconv"
	"strings"
	"testing"

	"github.com/hibiken/asynq"
)

// ParseRedisOpt parses Redis URL into Asynq connection options
func ParseRedisOpt(t *testing.T, redisURL string) asynq.RedisConnOpt {
	t.Helper()
	// Parse URL manually to ensure we get RedisClientOpt
	// Format: redis://host:port/db

	// Remove redis:// prefix
	url := strings.TrimPrefix(redisURL, "redis://")

	// Split by / to get host:port and database
	parts := strings.Split(url, "/")
	addr := parts[0]

	db := 0
	if len(parts) > 1 && parts[1] != "" {
		var err error
		db, err = strconv.Atoi(parts[1])
		if err != nil {
			t.Fatalf("Invalid database number in Redis URL %s: %v", redisURL, err)
		}
	}

	return &asynq.RedisClientOpt{
		Addr: addr,
		DB:   db,
	}
}
