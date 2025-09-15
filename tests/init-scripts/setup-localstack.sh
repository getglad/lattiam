#!/bin/bash

# Setup script for LocalStack integration testing
set -e

echo "Setting up LocalStack for Lattiam integration tests..."

# Wait for LocalStack to be ready
echo "Waiting for LocalStack to be ready..."
while ! curl -f http://localhost:4566/_localstack/health > /dev/null 2>&1; do
    echo "Waiting for LocalStack..."
    sleep 2
done

echo "LocalStack is ready!"

# Set AWS CLI to use LocalStack
export AWS_ACCESS_KEY_ID=test
export AWS_SECRET_ACCESS_KEY=test
export AWS_DEFAULT_REGION=us-east-1
export AWS_ENDPOINT_URL=http://localhost:4566

echo "Creating test IAM resources..."

# Create test policies
echo "Creating test policies..."
aws iam create-policy \
    --policy-name lattiam-test-read-only \
    --path /test/ \
    --policy-document '{
        "Version": "2012-10-17",
        "Statement": [
            {
                "Effect": "Allow",
                "Action": [
                    "s3:GetObject",
                    "s3:ListBucket"
                ],
                "Resource": "*"
            }
        ]
    }' || echo "Policy already exists"

# Create test roles
echo "Creating test roles..."
aws iam create-role \
    --role-name lattiam-test-base-role \
    --path /test/ \
    --assume-role-policy-document '{
        "Version": "2012-10-17",
        "Statement": [
            {
                "Effect": "Allow",
                "Principal": {
                    "Service": "ec2.amazonaws.com"
                },
                "Action": "sts:AssumeRole"
            }
        ]
    }' \
    --description "Base test role for Lattiam integration tests" || echo "Role already exists"

# Create test users
echo "Creating test users..."
aws iam create-user \
    --user-name lattiam-test-user \
    --path /test/ || echo "User already exists"

# Create test groups
echo "Creating test groups..."
aws iam create-group \
    --group-name lattiam-test-group \
    --path /test/ || echo "Group already exists"

echo "LocalStack setup complete!"
echo "Available test resources:"
echo "- Role: lattiam-test-base-role"
echo "- User: lattiam-test-user"
echo "- Group: lattiam-test-group"
echo "- Policy: lattiam-test-read-only"