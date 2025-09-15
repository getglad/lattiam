package dependency

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInterpolationParser_ParseInterpolations(t *testing.T) { //nolint:funlen // Test function with comprehensive test cases
	t.Parallel()
	parser := NewInterpolationParser()

	tests := []struct {
		name     string
		input    string
		expected []string
		wantErr  bool
	}{
		{
			name:     "simple interpolation",
			input:    `bucket = "${aws_s3_bucket.logs.id}"`,
			expected: []string{"aws_s3_bucket.logs.id"},
		},
		{
			name:     "multiple interpolations",
			input:    `name = "${var.prefix}-${aws_s3_bucket.data.id}"`,
			expected: []string{"var.prefix", "aws_s3_bucket.data.id"},
		},
		{
			name:     "interpolation in comment should be ignored",
			input:    `// This depends on ${aws_s3_bucket.fake.id}`,
			expected: []string{},
		},
		{
			name:     "interpolation in block comment should be ignored",
			input:    `/* This depends on ${aws_s3_bucket.fake.id} */`,
			expected: []string{},
		},
		{
			name:     "interpolation in string literal should be parsed",
			input:    `description = "Bucket ${aws_s3_bucket.main.id}"`,
			expected: []string{"aws_s3_bucket.main.id"},
		},
		{
			name: "mixed valid and commented interpolations",
			input: `
				bucket = "${aws_s3_bucket.logs.id}"
				// Old: bucket = "${aws_s3_bucket.old.id}"
				/* Deprecated: "${aws_s3_bucket.deprecated.id}" */
				arn = "${aws_iam_role.lambda.arn}"
			`,
			expected: []string{"aws_s3_bucket.logs.id", "aws_iam_role.lambda.arn"},
		},
		{
			name:     "nested braces",
			input:    `value = "${jsonencode({bucket = aws_s3_bucket.data.id})}"`,
			expected: []string{"jsonencode({bucket = aws_s3_bucket.data.id})"},
		},
		{
			name:     "unclosed interpolation",
			input:    `bucket = "${aws_s3_bucket.logs.id"`,
			expected: []string{},
			wantErr:  true,
		},
		{
			name:     "empty interpolation",
			input:    `value = "${}"`,
			expected: []string{},
			wantErr:  true,
		},
		{
			name:     "hash comment",
			input:    `# This depends on ${aws_s3_bucket.fake.id}`,
			expected: []string{},
		},
		{
			name:     "escaped quote in string",
			input:    `description = "Bucket \"${aws_s3_bucket.main.id}\""`,
			expected: []string{"aws_s3_bucket.main.id"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := parser.ParseInterpolations(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestInterpolationParser_ExtractResourceReferences(t *testing.T) {
	t.Parallel()
	parser := NewInterpolationParser()

	tests := []struct {
		name           string
		interpolations []string
		expected       []string
	}{
		{
			name:           "simple resource reference",
			interpolations: []string{"aws_s3_bucket.logs.id"},
			expected:       []string{"aws_s3_bucket.logs"},
		},
		{
			name:           "multiple attributes same resource",
			interpolations: []string{"aws_s3_bucket.logs.id", "aws_s3_bucket.logs.arn"},
			expected:       []string{"aws_s3_bucket.logs"},
		},
		{
			name:           "function call should be ignored",
			interpolations: []string{"jsonencode(aws_s3_bucket.logs.id)"},
			expected:       []string{},
		},
		{
			name: "mixed references and functions",
			interpolations: []string{
				"aws_s3_bucket.logs.id",
				"file(\"data.json\")",
				"aws_iam_role.lambda.arn",
			},
			expected: []string{"aws_s3_bucket.logs", "aws_iam_role.lambda"},
		},
		{
			name:           "variable reference",
			interpolations: []string{"var.region"},
			expected:       []string{"var.region"},
		},
		{
			name:           "local reference",
			interpolations: []string{"local.bucket_name"},
			expected:       []string{"local.bucket_name"},
		},
		{
			name:           "insufficient parts",
			interpolations: []string{"justname"},
			expected:       []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := parser.ExtractResourceReferences(tt.interpolations)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestInterpolationParser_ComplexScenarios(t *testing.T) { //nolint:funlen // Test function with comprehensive test cases
	t.Parallel()
	parser := NewInterpolationParser()

	t.Run("terraform configuration with comments", func(t *testing.T) {
		t.Parallel()
		input := `
resource "aws_s3_bucket" "logs" {
  bucket = "${var.prefix}-logs-${data.aws_caller_identity.current.account_id}"
  
  // Old bucket name: "${var.old_prefix}-logs"
  // TODO: Update to use ${aws_s3_bucket.archive.id} for archival
  
  lifecycle_rule {
    /* 
     * This rule archives to ${aws_s3_bucket.archive.id}
     * after 30 days
     */
    enabled = true
    
    transition {
      days          = 30
      storage_class = "GLACIER"
    }
  }
  
  tags = {
    Name        = "${var.prefix}-logs"
    Environment = var.environment  # No interpolation here
    // Debug     = "${local.debug_mode}"
  }
}
`
		result, err := parser.ParseInterpolations(input)
		require.NoError(t, err)

		expected := []string{
			"var.prefix",
			"data.aws_caller_identity.current.account_id",
			"var.prefix",
		}
		assert.Equal(t, expected, result)
	})

	t.Run("json with interpolations", func(t *testing.T) {
		t.Parallel()
		input := `
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Principal": {
      "Service": "lambda.amazonaws.com"
    },
    "Action": "sts:AssumeRole",
    "Resource": "${aws_iam_role.lambda.arn}"
  }]
}
`
		result, err := parser.ParseInterpolations(input)
		require.NoError(t, err)
		assert.Equal(t, []string{"aws_iam_role.lambda.arn"}, result)
	})
}
