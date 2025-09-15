package state

import (
	"testing"
	"time"

	"github.com/lattiam/lattiam/internal/interfaces"
)

// TestAWSStateStore_Interface verifies that AWSStateStore implements the StateStore interface
func TestAWSStateStore_Interface(t *testing.T) {
	t.Parallel()
	var _ interfaces.StateStore = (*AWSStateStore)(nil)
}

// TestAWSStateStoreConfig_Validation tests configuration validation
//
//nolint:funlen // Comprehensive AWS configuration validation test
func TestAWSStateStoreConfig_Validation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		config  AWSStateStoreConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: AWSStateStoreConfig{
				DynamoDBTable:  "test-table",
				DynamoDBRegion: "us-east-1",
				S3Bucket:       "test-bucket",
				S3Region:       "us-east-1",
			},
			wantErr: false,
		},
		{
			name: "missing DynamoDB table",
			config: AWSStateStoreConfig{
				DynamoDBRegion: "us-east-1",
				S3Bucket:       "test-bucket",
				S3Region:       "us-east-1",
			},
			wantErr: true,
		},
		{
			name: "missing DynamoDB region",
			config: AWSStateStoreConfig{
				DynamoDBTable: "test-table",
				S3Bucket:      "test-bucket",
				S3Region:      "us-east-1",
			},
			wantErr: true,
		},
		{
			name: "missing S3 bucket",
			config: AWSStateStoreConfig{
				DynamoDBTable:  "test-table",
				DynamoDBRegion: "us-east-1",
				S3Region:       "us-east-1",
			},
			wantErr: true,
		},
		{
			name: "missing S3 region",
			config: AWSStateStoreConfig{
				DynamoDBTable:  "test-table",
				DynamoDBRegion: "us-east-1",
				S3Bucket:       "test-bucket",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateAWSConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateAWSConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestAWSDeploymentMetadata_MarshalUnmarshal tests metadata serialization
func TestAWSDeploymentMetadata_MarshalUnmarshal(t *testing.T) {
	t.Parallel()
	store := &AWSStateStore{} // Don't need actual AWS clients for this test

	original := AWSDeploymentMetadata{
		DeploymentID:  "test-deployment-123",
		Name:          "Test Deployment",
		Status:        string(interfaces.DeploymentStatusProcessing),
		CreatedAt:     time.Now().UTC().Truncate(time.Second), // Truncate to avoid nanosecond precision issues
		UpdatedAt:     time.Now().UTC().Truncate(time.Second),
		StateS3Key:    "test-deployment-123.tfstate",
		ResourceCount: 5,
		Serial:        123,
		Lineage:       "test-lineage-456",
		Version:       1,
	}

	// Marshal to DynamoDB format
	item, err := store.marshalDeploymentMetadata(original)
	if err != nil {
		t.Fatalf("Failed to marshal metadata: %v", err)
	}

	// Unmarshal back to struct
	unmarshaled, err := store.unmarshalDeploymentMetadata(item)
	if err != nil {
		t.Fatalf("Failed to unmarshal metadata: %v", err)
	}

	// Compare all fields
	if original.DeploymentID != unmarshaled.DeploymentID {
		t.Errorf("DeploymentID mismatch: got %s, want %s", unmarshaled.DeploymentID, original.DeploymentID)
	}
	if original.Name != unmarshaled.Name {
		t.Errorf("Name mismatch: got %s, want %s", unmarshaled.Name, original.Name)
	}
	if original.Status != unmarshaled.Status {
		t.Errorf("Status mismatch: got %s, want %s", unmarshaled.Status, original.Status)
	}
	if !original.CreatedAt.Equal(unmarshaled.CreatedAt) {
		t.Errorf("CreatedAt mismatch: got %v, want %v", unmarshaled.CreatedAt, original.CreatedAt)
	}
	if !original.UpdatedAt.Equal(unmarshaled.UpdatedAt) {
		t.Errorf("UpdatedAt mismatch: got %v, want %v", unmarshaled.UpdatedAt, original.UpdatedAt)
	}
	if original.StateS3Key != unmarshaled.StateS3Key {
		t.Errorf("StateS3Key mismatch: got %s, want %s", unmarshaled.StateS3Key, original.StateS3Key)
	}
	if original.ResourceCount != unmarshaled.ResourceCount {
		t.Errorf("ResourceCount mismatch: got %d, want %d", unmarshaled.ResourceCount, original.ResourceCount)
	}
	if original.Serial != unmarshaled.Serial {
		t.Errorf("Serial mismatch: got %d, want %d", unmarshaled.Serial, original.Serial)
	}
	if original.Lineage != unmarshaled.Lineage {
		t.Errorf("Lineage mismatch: got %s, want %s", unmarshaled.Lineage, original.Lineage)
	}
	if original.Version != unmarshaled.Version {
		t.Errorf("Version mismatch: got %d, want %d", unmarshaled.Version, original.Version)
	}
}

// TestStateS3Key tests S3 key generation
func TestStateS3Key(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		prefix       string
		deploymentID string
		expected     string
	}{
		{
			name:         "no prefix",
			prefix:       "",
			deploymentID: "test-deployment",
			expected:     "test-deployment.tfstate",
		},
		{
			name:         "with prefix",
			prefix:       "lattiam/states",
			deploymentID: "test-deployment",
			expected:     "lattiam/states/test-deployment.tfstate",
		},
		{
			name:         "prefix with trailing slash",
			prefix:       "lattiam/states/",
			deploymentID: "test-deployment",
			expected:     "lattiam/states/test-deployment.tfstate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := &AWSStateStore{
				config: AWSStateStoreConfig{
					S3Prefix: tt.prefix,
				},
			}

			result := store.stateS3Key(tt.deploymentID)
			if result != tt.expected {
				t.Errorf("stateS3Key() = %s, want %s", result, tt.expected)
			}
		})
	}
}

// TestGenerateLineage tests lineage generation
func TestGenerateLineage(t *testing.T) {
	t.Parallel()
	store := &AWSStateStore{}

	lineage1 := store.generateLineage()
	lineage2 := store.generateLineage()

	if lineage1 == "" {
		t.Error("generateLineage() returned empty string")
	}

	if lineage1 == lineage2 {
		t.Error("generateLineage() returned same value twice (should be unique)")
	}

	if len(lineage1) < 10 {
		t.Error("generateLineage() returned suspiciously short value")
	}
}
