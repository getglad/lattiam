package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/lattiam/lattiam/internal/interfaces"
)

// DynamoDBLockConfig holds configuration for DynamoDB-based locking
type DynamoDBLockConfig struct {
	TableName        string        `json:"table_name"`
	Region           string        `json:"region"`
	Endpoint         string        `json:"endpoint,omitempty"` // For LocalStack or custom endpoints
	TTL              time.Duration `json:"ttl"`                // Lock TTL duration
	RefreshInterval  time.Duration `json:"refresh_interval"`   // How often to refresh the lock
	TimeoutOnAcquire time.Duration `json:"timeout_on_acquire"` // How long to wait when acquiring lock
	// AWS credentials should be provided via IAM roles, instance profiles, or environment variables
}

// DynamoDBLockProvider implements distributed locking using DynamoDB
type DynamoDBLockProvider struct {
	client       *dynamodb.Client
	config       DynamoDBLockConfig
	ownerID      string // Unique identifier for this lock holder
	refreshStops map[string]chan struct{}
}

// DynamoDBLock represents a lock held in DynamoDB
type DynamoDBLock struct {
	id           string
	lockID       string
	deploymentID string
	createdAt    time.Time
	operation    string
	provider     *DynamoDBLockProvider
	refreshStop  chan struct{}
}

// LockInfo represents the metadata stored in DynamoDB for a lock
type LockInfo struct {
	Operation string            `json:"operation"`
	Created   int64             `json:"created"`
	Info      map[string]string `json:"info"`
}

// NewDynamoDBLockProvider creates a new DynamoDB-based lock provider
func NewDynamoDBLockProvider(cfg DynamoDBLockConfig) (*DynamoDBLockProvider, error) {
	if cfg.TableName == "" {
		return nil, fmt.Errorf("table name is required")
	}
	if cfg.Region == "" {
		return nil, fmt.Errorf("region is required")
	}

	// Set defaults
	if cfg.TTL == 0 {
		cfg.TTL = 15 * time.Minute
	}
	if cfg.RefreshInterval == 0 {
		cfg.RefreshInterval = 3 * time.Minute // Refresh at 1/5 of TTL
	}
	if cfg.TimeoutOnAcquire == 0 {
		cfg.TimeoutOnAcquire = 10 * time.Second
	}

	// Load AWS config using shared function for consistent credential handling
	awsCfg, err := loadAWSConfigForEndpoint(cfg.Region, cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create DynamoDB client - endpoint handling is already done in loadAWSConfigForEndpoint
	// but we may still need client-level endpoint override for non-LocalStack custom endpoints
	var ddbClient *dynamodb.Client
	if cfg.Endpoint != "" && !isLocalStackOrTestEnv(cfg.Endpoint) {
		ddbClient = dynamodb.NewFromConfig(awsCfg, func(o *dynamodb.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		})
	} else {
		ddbClient = dynamodb.NewFromConfig(awsCfg)
	}

	// Generate unique owner ID (hostname + PID)
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}
	ownerID := fmt.Sprintf("%s-%d-%d", hostname, os.Getpid(), time.Now().Unix())

	provider := &DynamoDBLockProvider{
		client:       ddbClient,
		config:       cfg,
		ownerID:      ownerID,
		refreshStops: make(map[string]chan struct{}),
	}

	// Ensure table exists
	if err := provider.ensureTable(); err != nil {
		return nil, fmt.Errorf("failed to ensure DynamoDB table exists: %w", err)
	}

	return provider, nil
}

// ensureTable ensures the DynamoDB table exists with proper schema
func (p *DynamoDBLockProvider) ensureTable() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Check if table exists
	_, err := p.client.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(p.config.TableName),
	})
	if err == nil {
		// Table exists
		return nil
	}

	// Try to create table if it doesn't exist
	var notFound *types.ResourceNotFoundException
	if !errors.As(err, &notFound) {
		return fmt.Errorf("failed to describe table: %w", err)
	}

	// Create table with TTL
	_, err = p.client.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName: aws.String(p.config.TableName),
		KeySchema: []types.KeySchemaElement{
			{
				AttributeName: aws.String("LockID"),
				KeyType:       types.KeyTypeHash,
			},
		},
		AttributeDefinitions: []types.AttributeDefinition{
			{
				AttributeName: aws.String("LockID"),
				AttributeType: types.ScalarAttributeTypeS,
			},
		},
		BillingMode: types.BillingModePayPerRequest,
	})
	if err != nil {
		return fmt.Errorf("failed to create DynamoDB table: %w", err)
	}

	// Wait for table to be active
	waiter := dynamodb.NewTableExistsWaiter(p.client)
	if err := waiter.Wait(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(p.config.TableName),
	}, 5*time.Minute); err != nil {
		return fmt.Errorf("timeout waiting for table to become active: %w", err)
	}

	// Enable TTL on the TTL attribute
	_, err = p.client.UpdateTimeToLive(ctx, &dynamodb.UpdateTimeToLiveInput{
		TableName: aws.String(p.config.TableName),
		TimeToLiveSpecification: &types.TimeToLiveSpecification{
			AttributeName: aws.String("TTL"),
			Enabled:       aws.Bool(true),
		},
	})
	_ = err // TTL might already be enabled, or this might be LocalStack - log but don't fail

	return nil
}

// AcquireLock attempts to acquire a distributed lock with retry logic and exponential backoff
//
//nolint:funlen // Lock acquisition requires retry logic, validation, and error handling
func (p *DynamoDBLockProvider) AcquireLock(deploymentID, operation string) (interfaces.StateLock, error) {
	lockID := fmt.Sprintf("deployment/%s", deploymentID)

	ctx, cancel := context.WithTimeout(context.Background(), p.config.TimeoutOnAcquire)
	defer cancel()

	// Maximum number of retries with exponential backoff
	const maxRetries = 3
	const baseDelayMs = 100
	const maxDelayMs = 5000

	// Prepare lock info
	lockInfo := LockInfo{
		Operation: operation,
		Created:   time.Now().UTC().Unix(),
		Info: map[string]string{
			"deployment_id": deploymentID,
			"hostname":      p.ownerID,
		},
	}

	lockInfoJSON, err := json.Marshal(lockInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal lock info: %w", err)
	}

	// Retry loop with exponential backoff
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Check if context is canceled
		if ctx.Err() != nil {
			return nil, fmt.Errorf("lock acquisition canceled: %w", ctx.Err())
		}

		// Calculate expiration time for this attempt
		now := time.Now().UTC()
		ttlExpiry := now.Add(p.config.TTL).Unix()

		// Attempt to acquire lock using conditional write
		item := map[string]types.AttributeValue{
			"LockID":    &types.AttributeValueMemberS{Value: lockID},
			"Owner":     &types.AttributeValueMemberS{Value: p.ownerID},
			"Operation": &types.AttributeValueMemberS{Value: operation},
			"Created":   &types.AttributeValueMemberN{Value: strconv.FormatInt(now.Unix(), 10)},
			"TTL":       &types.AttributeValueMemberN{Value: strconv.FormatInt(ttlExpiry, 10)},
			"Info":      &types.AttributeValueMemberS{Value: string(lockInfoJSON)},
		}

		_, err = p.client.PutItem(ctx, &dynamodb.PutItemInput{
			TableName:           aws.String(p.config.TableName),
			Item:                item,
			ConditionExpression: aws.String("attribute_not_exists(LockID)"),
		})

		if err == nil {
			// Successfully acquired lock
			lock := &DynamoDBLock{
				id:           fmt.Sprintf("dynamodb-lock-%d", now.UnixNano()),
				lockID:       lockID,
				deploymentID: deploymentID,
				createdAt:    now,
				operation:    operation,
				provider:     p,
				refreshStop:  make(chan struct{}),
			}

			// Start refresh goroutine
			go p.refreshLock(lock)

			// Store refresh stop channel
			p.refreshStops[lock.id] = lock.refreshStop

			return lock, nil
		}

		// Check if error is retryable
		var conditionalCheckFailed *types.ConditionalCheckFailedException
		if errors.As(err, &conditionalCheckFailed) {
			// Lock is held by another process - don't retry
			return nil, fmt.Errorf("lock is already held by another process")
		}

		// Store error for final return
		lastErr = err

		// Don't sleep on the last attempt
		if attempt < maxRetries {
			// Calculate exponential backoff with jitter
			delayMs := int(math.Min(float64(baseDelayMs)*math.Pow(2, float64(attempt)), float64(maxDelayMs)))
			jitter := time.Duration(delayMs/2) * time.Millisecond
			delay := time.Duration(delayMs)*time.Millisecond + jitter

			select {
			case <-time.After(delay):
				// Continue to next retry
			case <-ctx.Done():
				return nil, fmt.Errorf("lock acquisition canceled during retry: %w", ctx.Err())
			}
		}
	}

	// All retries exhausted
	if lastErr != nil {
		return nil, fmt.Errorf("failed to acquire lock after %d retries: %w", maxRetries, lastErr)
	}
	return nil, fmt.Errorf("failed to acquire lock after %d retries", maxRetries)
}

// refreshLock periodically refreshes the lock TTL
func (p *DynamoDBLockProvider) refreshLock(lock *DynamoDBLock) {
	ticker := time.NewTicker(p.config.RefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-lock.refreshStop:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			ttlExpiry := time.Now().UTC().Add(p.config.TTL).Unix()

			_, err := p.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
				TableName: aws.String(p.config.TableName),
				Key: map[string]types.AttributeValue{
					"LockID": &types.AttributeValueMemberS{Value: lock.lockID},
				},
				UpdateExpression: aws.String("SET #ttl = :ttl"),
				ExpressionAttributeNames: map[string]string{
					"#ttl": "TTL",
				},
				ExpressionAttributeValues: map[string]types.AttributeValue{
					":ttl":   &types.AttributeValueMemberN{Value: strconv.FormatInt(ttlExpiry, 10)},
					":owner": &types.AttributeValueMemberS{Value: p.ownerID},
				},
				ConditionExpression: aws.String("#owner = :owner"),
			})

			cancel()

			if err != nil {
				// Lock might have been lost or stolen
				// Stop refreshing and let the caller handle it
				return
			}
		}
	}
}

// ForceUnlock forcibly removes a lock (use with caution)
func (p *DynamoDBLockProvider) ForceUnlock(lockID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := p.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(p.config.TableName),
		Key: map[string]types.AttributeValue{
			"LockID": &types.AttributeValueMemberS{Value: lockID},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to force unlock: %w", err)
	}

	return nil
}

// ListLocks returns all active locks (for debugging/monitoring)
//
//nolint:gocognit,gocyclo // Complex lock listing with attribute conversion
func (p *DynamoDBLockProvider) ListLocks() ([]map[string]interface{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := p.client.Scan(ctx, &dynamodb.ScanInput{
		TableName: aws.String(p.config.TableName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list locks: %w", err)
	}

	locks := make([]map[string]interface{}, 0, len(result.Items))
	for _, item := range result.Items {
		lock := make(map[string]interface{})

		if v, ok := item["LockID"]; ok {
			if s, ok := v.(*types.AttributeValueMemberS); ok {
				lock["lock_id"] = s.Value
			}
		}
		if v, ok := item["Owner"]; ok {
			if s, ok := v.(*types.AttributeValueMemberS); ok {
				lock["owner"] = s.Value
			}
		}
		if v, ok := item["Operation"]; ok {
			if s, ok := v.(*types.AttributeValueMemberS); ok {
				lock["operation"] = s.Value
			}
		}
		if v, ok := item["Created"]; ok {
			if n, ok := v.(*types.AttributeValueMemberN); ok {
				if created, err := strconv.ParseInt(n.Value, 10, 64); err == nil {
					lock["created"] = time.Unix(created, 0)
				}
			}
		}
		if v, ok := item["TTL"]; ok {
			if n, ok := v.(*types.AttributeValueMemberN); ok {
				if ttl, err := strconv.ParseInt(n.Value, 10, 64); err == nil {
					lock["expires_at"] = time.Unix(ttl, 0)
				}
			}
		}
		if v, ok := item["Info"]; ok {
			if s, ok := v.(*types.AttributeValueMemberS); ok {
				var info LockInfo
				if err := json.Unmarshal([]byte(s.Value), &info); err == nil {
					lock["info"] = info.Info
				}
			}
		}

		locks = append(locks, lock)
	}

	return locks, nil
}

// Shutdown cleanly shuts down the lock provider
func (p *DynamoDBLockProvider) Shutdown() {
	// Stop all refresh goroutines
	for _, stop := range p.refreshStops {
		close(stop)
	}
	p.refreshStops = make(map[string]chan struct{})
}

// DynamoDBLock methods implementing interfaces.StateLock

// ID returns the lock identifier
func (l *DynamoDBLock) ID() string {
	return l.id
}

// DeploymentID returns the deployment identifier
func (l *DynamoDBLock) DeploymentID() string {
	return l.deploymentID
}

// CreatedAt returns when the lock was created
func (l *DynamoDBLock) CreatedAt() time.Time {
	return l.createdAt
}

// Release releases the DynamoDB lock
func (l *DynamoDBLock) Release() error {
	// Stop refresh goroutine
	close(l.refreshStop)

	// Remove from refresh stops map
	delete(l.provider.refreshStops, l.id)

	// Delete lock from DynamoDB
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := l.provider.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(l.provider.config.TableName),
		Key: map[string]types.AttributeValue{
			"LockID": &types.AttributeValueMemberS{Value: l.lockID},
		},
		ConditionExpression: aws.String("#owner = :owner"),
		ExpressionAttributeNames: map[string]string{
			"#owner": "Owner",
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":owner": &types.AttributeValueMemberS{Value: l.provider.ownerID},
		},
	})
	if err != nil {
		var conditionalCheckFailed *types.ConditionalCheckFailedException
		if errors.As(err, &conditionalCheckFailed) {
			return fmt.Errorf("lock is not owned by this process")
		}
		return fmt.Errorf("failed to release lock: %w", err)
	}

	return nil
}

// Ensure DynamoDBLock implements interfaces.StateLock
var _ interfaces.StateLock = (*DynamoDBLock)(nil)
