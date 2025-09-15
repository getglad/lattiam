package helpers

import (
	"encoding/json"
	"fmt"
)

// DeploymentRequest represents a deployment creation request
type DeploymentRequest struct {
	Name              string                 `json:"name"`
	Provider          string                 `json:"provider"`
	ProviderVersion   string                 `json:"provider_version"`
	TerraformJSON     map[string]interface{} `json:"terraform_json"`
	EnvironmentConfig map[string]interface{} `json:"environment_config,omitempty"`
}

// NewDeploymentRequest creates a new deployment request with default values
//
//nolint:gocognit // Complex test data builder with resource configuration
func NewDeploymentRequest(name string, resources ...map[string]interface{}) DeploymentRequest {
	terraformJSON := map[string]interface{}{
		"resource": map[string]interface{}{},
	}

	// Separate data sources from resources
	dataSourcesMap := make(map[string]interface{})

	// Merge all provided resources
	for _, resource := range resources {
		for k, v := range resource {
			if k == "data" {
				// This is a data source - merge into data sources map
				if dataMap, ok := v.(map[string]interface{}); ok {
					for dataType, dataInstances := range dataMap {
						if _, exists := dataSourcesMap[dataType]; !exists {
							dataSourcesMap[dataType] = make(map[string]interface{})
						}
						if typeMap, ok := dataSourcesMap[dataType].(map[string]interface{}); ok {
							if instancesMap, ok := dataInstances.(map[string]interface{}); ok {
								for instanceName, instanceConfig := range instancesMap {
									typeMap[instanceName] = instanceConfig
								}
							}
						}
					}
				}
			} else {
				// This is a regular resource - add to resource map
				if resourceMap, ok := terraformJSON["resource"].(map[string]interface{}); ok {
					resourceMap[k] = v
				}
			}
		}
	}

	// Only add data block if we have data sources
	if len(dataSourcesMap) > 0 {
		terraformJSON["data"] = dataSourcesMap
	}

	return DeploymentRequest{
		Name:            name,
		Provider:        "aws",
		ProviderVersion: "5.31.0",
		TerraformJSON:   terraformJSON,
	}
}

// WithProvider sets the provider for the deployment
func (d DeploymentRequest) WithProvider(provider, version string) DeploymentRequest {
	d.Provider = provider
	d.ProviderVersion = version
	return d
}

// WithEnvironmentConfig sets the environment configuration
func (d DeploymentRequest) WithEnvironmentConfig(config map[string]interface{}) DeploymentRequest {
	d.EnvironmentConfig = config
	return d
}

// NewIAMRoleResource creates a new IAM role resource configuration
func NewIAMRoleResource(roleName string) map[string]interface{} {
	assumeRolePolicy := map[string]interface{}{
		"Version": "2012-10-17",
		"Statement": []map[string]interface{}{
			{
				"Effect": "Allow",
				"Principal": map[string]interface{}{
					"Service": "ec2.amazonaws.com",
				},
				"Action": "sts:AssumeRole",
			},
		},
	}

	policyJSON, _ := json.Marshal(assumeRolePolicy)

	return map[string]interface{}{
		"aws_iam_role": map[string]interface{}{
			roleName: map[string]interface{}{
				"name":               UniqueName(fmt.Sprintf("test-role-%s", roleName)),
				"assume_role_policy": string(policyJSON),
			},
		},
	}
}

// NewS3BucketResource creates a new S3 bucket resource configuration
func NewS3BucketResource(bucketName string) map[string]interface{} {
	return map[string]interface{}{
		"aws_s3_bucket": map[string]interface{}{
			bucketName: map[string]interface{}{
				"bucket":   UniqueName(fmt.Sprintf("test-bucket-%s", bucketName)),
				"timeouts": map[string]interface{}{},
			},
		},
	}
}

// NewS3BucketWithVersioning creates an S3 bucket with versioning enabled
func NewS3BucketWithVersioning(bucketName string) map[string]interface{} {
	uniqueBucketName := UniqueName(fmt.Sprintf("test-bucket-%s", bucketName))
	return map[string]interface{}{
		"aws_s3_bucket": map[string]interface{}{
			bucketName: map[string]interface{}{
				"bucket":   uniqueBucketName,
				"timeouts": map[string]interface{}{},
			},
		},
		"aws_s3_bucket_versioning": map[string]interface{}{
			fmt.Sprintf("%s_versioning", bucketName): map[string]interface{}{
				"bucket": fmt.Sprintf("${aws_s3_bucket.%s.id}", bucketName),
				"versioning_configuration": map[string]interface{}{
					"status": "Enabled",
				},
			},
		},
	}
}

// NewSecurityGroupResource creates a new security group resource configuration
func NewSecurityGroupResource(sgName string) map[string]interface{} {
	return map[string]interface{}{
		"aws_security_group": map[string]interface{}{
			sgName: map[string]interface{}{
				"name":        UniqueName(fmt.Sprintf("test-sg-%s", sgName)),
				"description": "Test security group",
				"ingress": []map[string]interface{}{
					{
						"from_port":   80,
						"to_port":     80,
						"protocol":    "tcp",
						"cidr_blocks": []string{"0.0.0.0/0"},
					},
				},
				"egress": []map[string]interface{}{
					{
						"from_port":   0,
						"to_port":     0,
						"protocol":    "-1",
						"cidr_blocks": []string{"0.0.0.0/0"},
					},
				},
			},
		},
	}
}

// NewVPCResource creates a new VPC resource configuration
func NewVPCResource(vpcName string) map[string]interface{} {
	return map[string]interface{}{
		"aws_vpc": map[string]interface{}{
			vpcName: map[string]interface{}{
				"cidr_block": "10.0.0.0/16",
				"tags": map[string]interface{}{
					"Name": fmt.Sprintf("test-vpc-%s", vpcName),
				},
			},
		},
	}
}

// NewSubnetResource creates a new subnet resource configuration
func NewSubnetResource(subnetName, vpcRef string) map[string]interface{} {
	return map[string]interface{}{
		"aws_subnet": map[string]interface{}{
			subnetName: map[string]interface{}{
				"vpc_id":     fmt.Sprintf("${aws_vpc.%s.id}", vpcRef),
				"cidr_block": "10.0.1.0/24",
				"tags": map[string]interface{}{
					"Name": fmt.Sprintf("test-subnet-%s", subnetName),
				},
			},
		},
	}
}

// NewEC2InstanceResource creates a new EC2 instance resource configuration
func NewEC2InstanceResource(instanceName string) map[string]interface{} {
	return map[string]interface{}{
		"aws_instance": map[string]interface{}{
			instanceName: map[string]interface{}{
				"ami":           "ami-12345678", // This would need to be a valid AMI ID
				"instance_type": "t2.micro",
				"tags": map[string]interface{}{
					"Name": fmt.Sprintf("test-instance-%s", instanceName),
				},
			},
		},
	}
}

// NewDataSourceResource creates a new data source configuration
func NewDataSourceResource(dataType, dataName string, config map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"data": map[string]interface{}{
			dataType: map[string]interface{}{
				dataName: config,
			},
		},
	}
}

// NewAWSRegionDataSource creates an AWS region data source
func NewAWSRegionDataSource(name string) map[string]interface{} {
	return NewDataSourceResource("aws_region", name, map[string]interface{}{})
}

// NewAWSCallerIdentityDataSource creates an AWS caller identity data source
func NewAWSCallerIdentityDataSource(name string) map[string]interface{} {
	return NewDataSourceResource("aws_caller_identity", name, map[string]interface{}{})
}

// NewS3BucketDataSource creates an S3 bucket data source to query an existing bucket
func NewS3BucketDataSource(name string, bucketName string) map[string]interface{} {
	return NewDataSourceResource("aws_s3_bucket", name, map[string]interface{}{
		"bucket": bucketName,
	})
}

// MergeResources combines multiple resource maps into one
func MergeResources(resources ...map[string]interface{}) map[string]interface{} { //nolint:gocognit // Complex resource merging logic
	result := make(map[string]interface{})

	for _, resource := range resources {
		for k, v := range resource {
			if existing, ok := result[k]; ok {
				// Merge if both are maps
				if existingMap, ok := existing.(map[string]interface{}); ok {
					if vMap, ok := v.(map[string]interface{}); ok {
						for subK, subV := range vMap {
							existingMap[subK] = subV
						}
						continue
					}
				}
			}
			result[k] = v
		}
	}

	return result
}

// NewMultiResourceDeployment creates a deployment with multiple resources
func NewMultiResourceDeployment(name string, resources ...map[string]interface{}) DeploymentRequest {
	terraformJSON := map[string]interface{}{
		"resource": MergeResources(resources...),
	}

	// Check if any resources have data sources
	for _, resource := range resources {
		if data, ok := resource["data"]; ok {
			terraformJSON["data"] = data
		}
	}

	return DeploymentRequest{
		Name:            name,
		Provider:        "aws",
		ProviderVersion: "5.31.0",
		TerraformJSON:   terraformJSON,
	}
}
